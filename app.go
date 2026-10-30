package main

import (
	"context"
	"fmt"
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
	"flint/internal/lsp"
	"flint/internal/restore"
	"flint/internal/runner"
	"flint/internal/scratch"
	"flint/internal/terminal"
	"flint/internal/watcher"
)

const maxRecentFolders = 10

// appState is remembered between launches.
type appState struct {
	RecentFolders []string `json:"recentFolders"`
	Agent         string   `json:"agent"` // preferred coding agent: "claude" or "codex"
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
	lsp         *lsp.Manager
	agents      *agentsession.Manager
	sessions    *restore.Store
	files       fuzzy.Index // quick-open file list of the open folder

	watchMu sync.Mutex
	watch   *watcher.Watcher

	stateMu sync.Mutex
	state   appState

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
	}
	a.terminals = terminal.NewManager(newTerminalEvents(a))
	a.runEvents = newRunEvents(a)
	a.runs = runner.NewManager(a.runEvents)
	a.debugEvents = newDebugEvents(a)
	a.debugs = dap.NewManager(a.debugEvents)
	a.lsp = lsp.NewManager()
	if err := config.LoadJSON(a.statePath(), &a.state); err != nil {
		return nil, err
	}
	a.agents = newAgentManager(a)
	return a, nil
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(context.Context) {
	// Language servers and agents each have grace periods; stop them in parallel
	// so quitting stays fast.
	var wg sync.WaitGroup
	for _, stop := range []func(){
		a.terminals.CloseAll,
		a.runs.CloseAll,
		a.debugs.CloseAll,
		a.lsp.Shutdown,
		func() { _ = a.agents.Close() },
		a.db.CloseAll,
		func() { a.setWatcher(nil) },
	} {
		wg.Add(1)
		go func(stop func()) {
			defer wg.Done()
			stop()
		}(stop)
	}
	wg.Wait()
}

// setWatcher replaces the active project watcher (nil just stops the current one).
func (a *App) setWatcher(w *watcher.Watcher) {
	a.watchMu.Lock()
	prev := a.watch
	a.watch = w
	a.watchMu.Unlock()
	if prev != nil {
		prev.Close()
	}
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
