package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/agentsession"
	"flint/internal/config"
	"flint/internal/dap"
	"flint/internal/db"
	"flint/internal/diag"
	"flint/internal/fuzzy"
	"flint/internal/git"
	"flint/internal/httpfile"
	"flint/internal/lsp"
	"flint/internal/restore"
	"flint/internal/runner"
	"flint/internal/scratch"
	"flint/internal/settings"
	"flint/internal/terminal"
	"flint/internal/update"
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
	// Update remembers the release check (api_update.go): when GitHub was last
	// asked, its answer, and the release the user skipped.
	Update update.State `json:"update,omitzero"`
}

// App holds the services shared by all bound APIs.
type App struct {
	ctx       context.Context // ends when the application shuts down
	wails     *application.App
	window    application.Window // the main window
	configDir string
	argFolder string

	db          *db.Manager
	scratch     *scratch.Store
	terminals   *terminal.Manager
	runs        *runner.Manager // run configurations and test runs
	runEvents   *runEvents
	debugs      *dap.Manager // debug sessions (dlv dap)
	debugEvents *debugEvents
	dockerAPI   *DockerAPI // bound API that also owns the docker events stream
	profiles    *profiles  // temporary profiles and recordings in progress
	lsp         *lsp.Manager
	agents      *agentsession.Manager
	sessions    *restore.Store
	files       fuzzy.Index      // quick-open file list of the open folder
	http        *httpfile.Client // requests of .http files, sent only on demand

	projMu   sync.Mutex
	roots    []string                    // open project folders, the primary one first
	watchers map[string]*watcher.Watcher // one per root

	stateMu sync.Mutex
	state   appState

	settingsMu sync.Mutex
	settings   settings.Settings // settings.json, loaded once at startup

	updates       *update.Client // GitHub's latest-release endpoint
	updateMu      sync.Mutex
	pendingUpdate *update.Result // a newer release the UI should offer; nil = none

	closeMu      sync.Mutex
	quitting     bool
	closeAskedAt time.Time
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
	store, err := db.OpenStore(dir, db.Keychain{})
	if err != nil {
		return nil, err
	}
	scratches, err := scratch.Open(dir)
	if err != nil {
		return nil, err
	}
	a := &App{
		configDir: dir,
		argFolder: folderFromArgs(os.Args[1:], ""),
		db:        db.NewManager(store),
		scratch:   scratches,
		sessions:  restore.New(filepath.Join(dir, "sessions")),
		http:      httpfile.NewClient(),
		updates:   update.New(diag.Version(version)),
	}
	a.db.SetHistory(db.OpenHistory(filepath.Join(dir, "history"), db.DefaultHistoryLimit))
	a.terminals = terminal.NewManager(newTerminalEvents(a))
	a.runEvents = newRunEvents(a)
	a.runs = runner.NewManager(a.runEvents)
	a.debugEvents = newDebugEvents(a)
	a.dockerAPI = &DockerAPI{app: a}
	a.debugs = dap.NewManager(a.debugEvents)
	a.lsp = lsp.NewManager()
	a.lsp.Trusted = a.isTrusted
	a.lsp.Config = func(language string) lsp.ServerConfig { return a.currentSettings().ServerConfig(language) }
	git.Untrusted = func(dir string) bool { return !a.isTrusted(dir) }
	a.profiles = newProfiles()
	if err := config.LoadJSON(a.statePath(), &a.state); err != nil {
		return nil, err
	}
	// A broken settings file must not keep Flint from starting: defaults are used
	// and the file is left alone until the user saves.
	a.settings, err = settings.Load(settings.Path(dir))
	if err != nil {
		log.Printf("flint: %v (using default settings)", err)
	}
	a.agents = newAgentManager(a)
	return a, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.watchForUpdates(ctx)
}

func (a *App) shutdown() {
	// Language servers and agents each have grace periods; stop them in parallel
	// so quitting stays fast.
	var wg sync.WaitGroup
	for _, stop := range []func(){
		a.terminals.CloseAll,
		a.runs.CloseAll,
		a.debugs.CloseAll,
		a.http.CancelAll,
		a.dockerAPI.close,
		a.lsp.Shutdown,
		func() { _ = a.agents.Close() },
		a.db.CloseAll,
		a.profiles.closeAll,
		a.closeProject,
	} {
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
// ignored. Relative paths are resolved against cwd ("" = this process's).
func folderFromArgs(args []string, cwd string) string {
	for _, arg := range args {
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
