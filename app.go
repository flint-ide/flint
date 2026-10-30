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

	"flint/internal/config"
	"flint/internal/db"
	"flint/internal/scratch"
	"flint/internal/terminal"
	"flint/internal/watcher"
)

const maxRecentFolders = 10

// appState is remembered between launches.
type appState struct {
	RecentFolders []string `json:"recentFolders"`
	Agent         string   `json:"agent"` // preferred coding agent: "claude" or "codex"
}

// App holds the services shared by all bound APIs.
type App struct {
	ctx       context.Context
	configDir string
	argFolder string

	db        *db.Manager
	scratch   *scratch.Store
	terminals *terminal.Manager

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
	}
	a.terminals = terminal.NewManager(terminalEvents{app: a})
	if err := config.LoadJSON(a.statePath(), &a.state); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(context.Context) {
	a.terminals.CloseAll()
	a.db.CloseAll()
	a.setWatcher(nil)
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
