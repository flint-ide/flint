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

	"flint/internal/agentsession"
	"flint/internal/config"
	"flint/internal/dap"
	"flint/internal/db"
	"flint/internal/fuzzy"
	"flint/internal/httpfile"
	"flint/internal/lsp"
	"flint/internal/restore"
	"flint/internal/runner"
	"flint/internal/scratch"
	"flint/internal/settings"
	"flint/internal/terminal"
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
}

// App holds the services shared by all bound APIs.
type App struct {
	ctx       context.Context
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
		argFolder: folderFromArgs(os.Args[1:]),
		db:        db.NewManager(store),
		scratch:   scratches,
		sessions:  restore.New(filepath.Join(dir, "sessions")),
		http:      httpfile.NewClient(),
	}
	a.db.SetHistory(db.OpenHistory(filepath.Join(dir, "history"), db.DefaultHistoryLimit))
	a.terminals = terminal.NewManager(newTerminalEvents(a))
	a.runEvents = newRunEvents(a)
	a.runs = runner.NewManager(a.runEvents)
	a.debugEvents = newDebugEvents(a)
	a.dockerAPI = &DockerAPI{app: a}
	a.debugs = dap.NewManager(a.debugEvents)
	a.lsp = lsp.NewManager()
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
	menuApp = a
}

func (a *App) shutdown(context.Context) {
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

func (a *App) recentFolders() []string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return append([]string{}, a.state.RecentFolders...) // never nil: the frontend expects []
}

// folderFromArgs supports `flint <dir>`; flags (e.g. injected by macOS) are ignored.
func folderFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
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
