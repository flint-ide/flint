package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/accounts"
	"flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/config"
	"flint/internal/dap"
	"flint/internal/feature"
	"flint/internal/features/extensions"
	"flint/internal/fuzzy"
	"flint/internal/git"
	"flint/internal/github"
	"flint/internal/httpfile"
	"flint/internal/lsp"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
	"flint/internal/orchestrate"
	"flint/internal/remote"
	"flint/internal/restore"
	"flint/internal/runner"
	"flint/internal/scratch"
	"flint/internal/sessions"
	"flint/internal/settings"
	"flint/internal/usage"
	"flint/internal/watcher"
)

const maxRecentFolders = 10

// appState is remembered between launches.
type appState struct {
	RecentFolders []string `json:"recentFolders"`
	// Agent is the preferred coding agent before settings.json existed; read only
	// as a fallback for settings.DefaultAgent.
	Agent string `json:"agent"`
	// AgentPolicy is the approval policy for agent chat sessions (nil = default).
	AgentPolicy *agentsession.Policy `json:"agentPolicy,omitempty"`
	// TrustedFolders the user trusts to run their code (see trust.go).
	TrustedFolders []string `json:"trustedFolders,omitempty"`
	// ToolCheckShown: the tool check has opened on its own once (first launch).
	ToolCheckShown bool `json:"toolCheckShown,omitempty"`
	// AgentTasks are the agent tasks per repository (tasks_state.go).
	AgentTasks agenttask.Store `json:"agentTasks,omitempty"`
	// Accounts are the signed-in accounts (api_accounts.go): login, avatar,
	// host and scopes only. Their tokens are in the keyring.
	Accounts []accounts.Account `json:"accounts,omitempty"`

	// features are the state.json keys feature modules keep, as written
	// (features.go); replaced, never changed in place.
	features map[string]json.RawMessage
}

// App holds the services shared by all bound APIs.
type App struct {
	ctx    context.Context // ends when the application shuts down
	wails  *application.App
	window application.Window // the main window
	// pageLoaded is set once the window's page has loaded (window_darwin.go).
	pageLoaded atomic.Bool
	configDir  string
	argFolder  string

	scratch   *scratch.Store
	terminals sessions.Terminals // the router (terminals_router.go)
	// termStreams starts Docker/extension stream sessions; same router as terminals.
	termStreams streamStarter
	termRouter  *terminalsRouter
	// remote holds the connections to remote machines (api_machines.go); the
	// router sends "<machineId>:<id>" terminal ids to it.
	remote *remote.Hub
	// background runs terminals in the background host (background_terminals.go);
	// nil when sessions.background was off at start.
	background *hostTerminals
	// stopSessionsOnQuit: Quit and Stop Background Sessions was chosen.
	stopSessionsOnQuit atomic.Bool
	runs               *runner.Manager // run configurations and test runs
	runEvents          *runEvents
	debugs             *dap.Manager // debug sessions (dlv dap)
	debugEvents        *debugEvents
	profiles           *profiles // temporary profiles and recordings in progress
	lsp                *lsp.Manager
	agents             sessions.Agents
	// agentMgr is the in-process manager behind agents, for what only it can
	// do: registering custom agents (syncCustomAgents) and starting a defined
	// agent on the driver of the definitions just read (StartWith).
	agentMgr   *agentsession.Manager
	mcp        *mcpserver.Server // Flint's MCP server for its agents (api_mcp.go)
	mcpSources *mcptools.Sources // what the MCP server's tools read
	// actionGrants and configRuns serve the MCP tools that act (api_mcp_actions.go).
	actionGrants actionGrants
	configRuns   configRuns
	orchGrants   orchGrants            // what the user allowed each chat to orchestrate (orch_grant.go)
	notices      orchestrate.Notices   // what a chat's children have to tell it (chat_notify.go)
	tasks        *agenttask.Tracker    // state of agent tasks from their chats
	orch         *orchestrate.Registry // every chat's place in the tree, state and output (chat_open.go)
	sessions     *restore.Store
	usage        *usage.Store // agent usage per day (usage_state.go)
	usageChats   usageChats
	queue        taskQueue        // slots of agent tasks being started (tasks_queue.go)
	files        fuzzy.Index      // quick-open file list of the open folder
	http         *httpfile.Client // requests of .http files, sent only on demand
	features     *feature.Set     // the feature modules (features.go)

	projMu   sync.Mutex
	roots    []string                    // open project folders, the primary one first
	watchers map[string]*watcher.Watcher // one per root

	stateMu sync.Mutex
	state   appState

	settingsMu sync.Mutex
	// definedMu serializes loadDefinedAgents; taken before settingsMu.
	definedMu sync.Mutex
	settings  settings.Settings // settings.json, loaded once at startup
	// definedAgents are the drivers of the agent definitions last read
	// (agents_defined.go); guarded by settingsMu like the custom agents.
	definedAgents []*definedAgent

	accounts *accounts.Store    // tokens of signed-in accounts (keyring)
	registry *accounts.Registry // account providers (GitHub)
	github   *github.Client     // pull requests and CI (api_github.go, api_ci.go)

	closeMu      sync.Mutex
	quitting     bool
	closeAskedAt time.Time

	confirmMu sync.Mutex      // one confirmation dialog at a time
	confirmed map[string]bool // custom agent command lines the user allowed a Check of, this session
	// emitHook replaces the Wails event bus in tests.
	emitHook func(name string, data ...any)
	// ask replaces the native confirmation dialog (askNativeAnswered) in tests.
	ask func(title, message, ok string) bool
	// attn follows the foreground and Flint's open questions (attention.go);
	// alerts sends desktop notifications (AlertsAPI; a fake in tests).
	attn   *attention
	alerts notifier
	// refreshForeground asks the window whether Flint is in front and tells
	// attn (followForeground; active is the app's activation an event
	// reported, nil for none). Set once, before the app runs.
	refreshForeground func(active *bool)
	// noticeProblems is the last problem told about each chat whose notice
	// from Flint did not arrive (chat id → text), so it is told once.
	noticeProblems sync.Map
}

// notifier sends a desktop notification; clicking it shows tab ("" for none).
type notifier interface {
	Notify(title, body, tab string) error
}

// NewApp wires services from the config directory.
func NewApp() (*App, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	scratches, err := scratch.Open(dir)
	if err != nil {
		return nil, err
	}
	a := &App{
		configDir: dir,
		argFolder: folderFromArgs(os.Args[1:], ""),
		scratch:   scratches,
		sessions:  restore.New(filepath.Join(dir, "sessions")),
		http:      httpfile.NewClient(),
	}
	a.runEvents = newRunEvents(a)
	a.runs = runner.NewManager(a.runEvents)
	a.debugEvents = newDebugEvents(a)
	a.debugs = dap.NewManager(a.debugEvents)
	a.lsp = lsp.NewManager()
	a.lsp.Trusted = a.isTrusted
	a.lsp.Config = func(language string) lsp.ServerConfig { return a.currentSettings().ServerConfig(language) }
	git.Untrusted = func(dir string) bool { return !a.isTrusted(dir) }
	a.profiles = newProfiles()
	// Before anything reads settings or state: their sections and keys.
	if err := registerFeatures(); err != nil {
		return nil, err
	}
	if err := config.LoadJSON(a.statePath(), &a.state); err != nil {
		return nil, err
	}
	// A broken settings file must not keep Flint from starting: defaults are used
	// and the file is left alone until the user saves.
	a.settings, err = settings.Load(settings.Path(dir))
	if err != nil {
		log.Printf("flint: %v (using default settings)", err)
	}
	a.wireTerminals(newTerminalEvents(a), dialHost, spawnHost) // after settings: sessions.background picks
	a.openUsage()
	a.wireAccounts()
	a.mcp, a.mcpSources = newMCP(a)
	a.features = feature.New(features, featureHost{a})
	a.tasks = agenttask.NewTracker(a.tasksByFolder, a.taskChanged)
	a.orch = orchestrate.New()
	a.agentMgr = newAgentManager(a)
	// Agent chats stay in this process even with sessions.background on: the
	// host has no MCP server, approval gate, custom agents or per-agent
	// settings yet (steps 3.8, 3.9). Moving them to the host is this switch.
	a.agents = a.agentMgr
	a.attn = a.newAttention()
	return a, nil
}

// newAttention wires the question announcer to the notifier, the settings
// and the UI (attention.go).
func (a *App) newAttention() *attention {
	return newAttention(
		func(title, body string) error {
			if a.alerts == nil {
				return nil
			}
			return a.alerts.Notify(title, body, "")
		},
		func() bool {
			s := a.currentSettings()
			return s.NotifyInBackground && s.NotifyOnApproval
		},
		a.emit,
	)
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.background != nil {
		go a.background.connect() // terminal calls wait for it, startup does not
	}
	a.features.Start(ctx)
}

func (a *App) shutdown() {
	// Language servers and agents each have grace periods; stop them in parallel
	// so quitting stays fast.
	var wg sync.WaitGroup
	for _, stop := range append([]func(){
		a.closeTerminals, // detaches from background terminals instead of ending them
		a.runs.CloseAll,
		a.debugs.CloseAll,
		a.http.CancelAll,
		a.lsp.Shutdown,
		func() { _ = a.agents.Close() },
		func() { _ = a.mcp.Close() },
		a.profiles.closeAll,
		a.closeProject,
		a.closeUsage,
	}, a.features.Closers()...) {
		wg.Add(1)
		go func(stop func()) {
			defer wg.Done()
			stop()
		}(stop)
	}
	wg.Wait()
}

func (a *App) statePath() string { return filepath.Join(a.configDir, "state.json") }

// rememberFolder moves path to the front of the recent-folders list.
func (a *App) rememberFolder(path string) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	recent := slices.DeleteFunc(slices.Clone(a.state.RecentFolders), func(p string) bool { return p == path })
	recent = append([]string{path}, recent...)
	if len(recent) > maxRecentFolders {
		recent = recent[:maxRecentFolders]
	}
	next := a.state
	next.RecentFolders = recent
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	go a.refreshMenu() // outside stateMu: building the menu reads the list again
	return nil
}

// claimToolCheck is true only the first time it is asked, ever: the tool check
// opens on its own once. A failed save still counts for this run, so a broken
// config directory shows it once per launch rather than never.
func (a *App) claimToolCheck() bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.state.ToolCheckShown {
		return false
	}
	next := a.state
	next.ToolCheckShown = true
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		a.logf("save state: %v", err)
	}
	a.state = next
	return true
}

func (a *App) recentFolders() []string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return append([]string{}, a.state.RecentFolders...) // never nil: the frontend expects []
}

// folderFromArgs supports `flint <dir>`; flags (e.g. injected by macOS) are
// ignored, and so is the folder of `--install-extension <dir>`, which is an
// extension to install, not a project. Relative paths are resolved against
// cwd ("" = this process's).
func folderFromArgs(args []string, cwd string) string {
	for _, arg := range extensions.WithoutInstallArg(args) {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if cwd != "" && !filepath.IsAbs(arg) {
			arg = filepath.Join(cwd, arg)
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}
