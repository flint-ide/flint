// Package host runs Flint's external extensions (docs/design/extensions.md,
// level 2): it discovers the installed ones (<config>/extensions/<id>/), starts
// an extension's process on first use, talks JSON-RPC to it (package rpc),
// answers what it asks of Flint within its declared permissions, stops it
// when idle and restarts it after a crash, with limits on every step.
//
// Nothing here runs before it is asked: discovery reads manifests on the
// first List or Tools call, and a process starts only on a command, a shown
// panel, an agent tool call or a declared onStartup (StartAtLaunch, after the
// window). No goroutine runs while no extension does.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"flint/internal/extensions/manifest"
	"flint/internal/extensions/sandbox"
)

// App is what the host needs from Flint.
type App interface {
	// Roots are the open project folders; Trusted says which ones the user
	// trusts (Restricted Mode is off). Extensions only ever see trusted ones.
	Roots() []string
	Trusted(path string) bool
	// Emit sends an event to the frontend.
	Emit(event string, data ...any)
	// OpenURL opens an http(s) link in the browser.
	OpenURL(link string) error
	// Confirm asks in a native dialog (Cancel is the default).
	Confirm(title, message, ok string) bool
	// Choose asks a native question with these buttons; "" when dismissed.
	Choose(title, message string, actions []string) string
	// Enabled reports whether the user left the extension on (Settings).
	Enabled(id string) bool
	// SetEnabled reports whether a decoration set is on: the user's choice
	// in Settings, else def (the manifest's default).
	SetEnabled(set string, def bool) bool
	// Settings is the extension's stored settings object (nil: none).
	Settings(id string) json.RawMessage
	// Version is Flint's version.
	Version() string
}

// Options configure a Host.
type Options struct {
	// Dir holds the installed extensions, one folder per id.
	Dir string
	App App
	// Secrets is the OS keyring (Keyring{}), a fake in tests.
	Secrets Secrets
	// Environ is the base environment of extension processes; nil uses
	// MinimalEnv after the login-shell PATH was imported.
	Environ func() []string
	// LanguageOf names a file's language (Flint's id, "" for none), which
	// decoration sets select files by; nil knows no languages.
	LanguageOf func(path string) string
	// Sandbox reports what this system can confine; nil asks the system
	// (sandbox.Probe). Launcher is the program started as the sandbox
	// launcher; "" is Flint's own executable, whose main calls sandbox.Main.
	Sandbox  func() sandbox.Support
	Launcher string
	// RequireSandbox: the user only runs extensions that are sandboxed
	// (the "extensions.requireSandbox" setting); nil means no.
	RequireSandbox func() bool
	// Timeouts; zero values use the defaults below.
	IdleTimeout    time.Duration
	InitTimeout    time.Duration
	ResolveTimeout time.Duration
	CallTimeout    time.Duration
	ToolTimeout    time.Duration
	StopTimeout    time.Duration
}

// Defaults (docs/design/extensions.md, "Activation and limits").
const (
	defaultIdle    = 5 * time.Minute
	defaultInit    = 10 * time.Second
	defaultResolve = 5 * time.Second
	defaultCall    = 30 * time.Second
	defaultTool    = 120 * time.Second
	defaultStop    = 2 * time.Second
)

// Events sent to the frontend.
const (
	// EventChanged: the list or a state changed; the frontend asks List again.
	EventChanged = "extensions:changed"
	// EventStatus carries a StatusEvent.
	EventStatus = "extensions:status"
	// EventRefresh carries a RefreshEvent.
	EventRefresh = "extensions:refresh"
	// EventMessage carries a MessageEvent.
	EventMessage = "extensions:message"
	// EventOpenFile carries an OpenFileEvent.
	EventOpenFile = "extensions:openFile"
	// EventAsk carries an AskEvent.
	EventAsk = "extensions:ask"
)

// ErrCancelled: the user said no in a confirmation.
var ErrCancelled = errors.New("cancelled")

// Host owns the installed extensions and their processes.
type Host struct {
	o Options

	mu         sync.Mutex
	exts       map[string]*ext
	docs       map[string]*document // open in editors, by path (decorations.go)
	discovered bool
	closed     bool
}

// New returns a host; nothing is read or started yet.
func New(o Options) *Host {
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = defaultIdle
	}
	if o.InitTimeout <= 0 {
		o.InitTimeout = defaultInit
	}
	if o.ResolveTimeout <= 0 {
		o.ResolveTimeout = defaultResolve
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = defaultCall
	}
	if o.ToolTimeout <= 0 {
		o.ToolTimeout = defaultTool
	}
	if o.StopTimeout <= 0 {
		o.StopTimeout = defaultStop
	}
	if o.Environ == nil {
		o.Environ = loginEnv
	}
	return &Host{o: o, exts: map[string]*ext{}, docs: map[string]*document{}}
}

// Discover reads the manifests again: new folders are added, changed ones
// replace the old (stopping its process), removed ones are dropped.
func (h *Host) Discover() {
	found := scan(h.o.Dir)
	var stop []*ext
	h.mu.Lock()
	h.discovered = true
	next := map[string]*ext{}
	for _, f := range found {
		old := h.exts[f.id]
		if old != nil && reflect.DeepEqual(old.m, f.m) && errText(old.loadErr) == errText(f.err) && old.dir == f.dir {
			next[f.id] = old
			continue
		}
		if old != nil {
			stop = append(stop, old)
		}
		e := newExt(h, f.id, f.dir, f.m, f.err)
		if old != nil {
			e.log = old.log // the log of the previous version stays readable
		}
		next[f.id] = e
	}
	for id, old := range h.exts {
		if next[id] == nil {
			stop = append(stop, old)
		}
	}
	h.exts = next
	h.mu.Unlock()
	for _, e := range stop {
		e.stop("replaced or removed")
		e.dropDocuments()
	}
	h.syncDocuments() // a new extension may decorate open documents
	h.changed()
}

func (h *Host) ensureDiscovered() {
	h.mu.Lock()
	done := h.discovered
	h.mu.Unlock()
	if !done {
		h.Discover()
	}
}

type found struct {
	id, dir string
	m       *manifest.Manifest
	err     error
}

// scan reads every folder's manifest; a folder's name must be its id.
func scan(dir string) []found {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // no extensions installed yet
	}
	var out []found
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") || !de.IsDir() {
			continue // install leftovers, files
		}
		f := found{id: name, dir: filepath.Join(dir, name)}
		f.m, f.err = manifest.Load(f.dir)
		switch {
		case f.err != nil:
			f.m = nil
		case f.m.ID != name:
			f.m, f.err = nil, fmt.Errorf("the folder is named %q but the manifest's id is %q", name, f.m.ID)
		}
		out = append(out, f)
	}
	return out
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// get returns the extension, discovering first if needed.
func (h *Host) get(id string) (*ext, error) {
	h.ensureDiscovered()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errors.New("Flint is quitting")
	}
	e := h.exts[id]
	if e == nil {
		return nil, fmt.Errorf("extension %q is not installed", id)
	}
	return e, nil
}

func (h *Host) all() []*ext {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*ext, 0, len(h.exts))
	for _, e := range h.exts {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b *ext) int { return strings.Compare(a.id, b.id) })
	return out
}

func (h *Host) changed() { h.o.App.Emit(EventChanged) }

// List describes every installed extension, discovering them on the first call.
func (h *Host) List() []Info {
	h.ensureDiscovered()
	exts := h.all()
	out := make([]Info, 0, len(exts))
	for _, e := range exts {
		out = append(out, e.info())
	}
	return out
}

// StartAtLaunch starts the extensions that declared onStartup, in the
// background; Flint calls it once its window is up.
func (h *Host) StartAtLaunch() {
	for _, e := range h.all() {
		if e.m != nil && e.m.RunsAtStartup() && h.o.App.Enabled(e.id) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), h.o.InitTimeout+time.Second)
				defer cancel()
				_, _ = e.session(ctx)
			}()
		}
	}
}

// SettingsChanged tells a running extension its settings changed, and stops
// one the user turned off.
func (h *Host) SettingsChanged(id string) {
	h.mu.Lock()
	e := h.exts[id] // not discovered yet: nothing runs
	h.mu.Unlock()
	if e == nil || e.m == nil {
		return
	}
	if !h.o.App.Enabled(id) {
		e.stop("turned off")
		h.syncDocuments()
		h.changed()
		return
	}
	h.syncDocuments() // a decoration set turned on or off
	p := e.running()
	switch {
	case p != nil && e.m.Contributes.Settings != nil:
		_ = p.conn.Notify("settings/changed", map[string]any{"settings": e.settings()})
	case p == nil && e.m.RunsAtStartup():
		go func() { // turned back on
			ctx, cancel := context.WithTimeout(context.Background(), h.o.InitTimeout+time.Second)
			defer cancel()
			_, _ = e.session(ctx)
		}()
	}
	h.changed()
}

// SetsChanged follows the user turning decoration sets on or off: extensions
// hear of the open documents their sets now apply to, or no longer do.
func (h *Host) SetsChanged() {
	h.syncDocuments()
	h.changed()
}

// RootsChanged tells running extensions the trusted folders changed.
func (h *Host) RootsChanged() {
	defer h.syncDocuments() // open documents of a folder that is trusted now, or no more
	for _, e := range h.all() {
		if p := e.running(); p != nil {
			if e.rootsOutgrew(p) {
				go e.restartForRoots()
				continue
			}
			_ = p.conn.Notify("workspace/rootsChanged", map[string]any{"roots": e.roots()})
		}
	}
}

// Restart clears a crash-stopped extension so the next use starts it again.
func (h *Host) Restart(id string) error {
	e, err := h.get(id)
	if err != nil {
		return err
	}
	e.stop("restarted by the user")
	e.resetCrashes()
	h.changed()
	if len(e.followed()) > 0 { // open documents it decorates: start it again
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), h.o.InitTimeout+time.Second)
			defer cancel()
			_, _ = e.session(ctx)
		}()
	}
	return nil
}

// Log is the extension's log: its stderr, its log notifications and what
// Flint saw happen to its process.
func (h *Host) Log(id string) (string, error) {
	h.ensureDiscovered()
	h.mu.Lock()
	e := h.exts[id] // also after Close: the log says how the processes ended
	h.mu.Unlock()
	if e == nil {
		return "", fmt.Errorf("extension %q is not installed", id)
	}
	return e.log.String(), nil
}

// Close stops every process (quit), in parallel.
func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range h.all() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.stop("Flint quit")
		}()
	}
	wg.Wait()
}
