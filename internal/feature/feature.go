// Package feature is the contract between Flint and a feature module: one
// package (internal/features/<id>) that declares everything the feature adds
// to the Go side, and one line in the list in package main (features.go).
//
// What a module may contribute:
//
//   - Settings: sections of settings.json it owns (settings.Section), loaded,
//     normalized, validated and saved with the built-in keys;
//   - Tools: external programs for the tool check and the bug report;
//   - State: keys of state.json it keeps through Host.LoadState / SaveState;
//   - New: what runs once the app is built: bound Wails services, tools for
//     agents on Flint's MCP server, and Start / Close lifecycle hooks.
//
// Declarations (Settings, Tools, State) are registered by Register before the
// settings are loaded; New is called while the app is built, before the window
// exists, so it must only construct (no I/O, no goroutines): Start is where
// background work begins, after launch.
//
// The frontend half of a feature is a module in frontend/src/features/<id>
// with the same id (see docs/design/extensions.md).
package feature

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/mcpserver"
	"flint/internal/settings"
	"flint/internal/terminal"
	"flint/internal/tools"
)

// Module declares a feature.
type Module struct {
	// ID names the feature ("docker"); lower-case letters, digits and dashes.
	ID string
	// Settings are the settings.json keys the feature owns.
	Settings []settings.AnySection
	// Tools are external programs the feature drives.
	Tools []tools.Tool
	// State lists the state.json keys the feature keeps (Host.LoadState).
	State []string
	// New builds the running parts; nil when the feature has none.
	New func(Host) Instance
}

// Instance is what a module contributes once the app is built. Every field
// is optional.
type Instance struct {
	// Services are bound to the frontend: application.NewService(&API{…})
	// written out in the module, where the bindings generator finds it; the
	// exported methods become calls in frontend/bindings.
	Services []application.Service
	// AgentTools are offered to agents by Flint's MCP server; read on every
	// request, so they can follow the feature's settings.
	AgentTools func() []mcpserver.Tool
	// Start runs once Flint has launched, with a context that ends at quit.
	// It must return quickly: long work belongs in a goroutine.
	Start func(ctx context.Context)
	// Close runs on quit, in parallel with the other features and services.
	Close func()
}

// Host is what Flint gives a feature: the parts of the app it may use. New
// needs go here, not into the feature reaching for App.
type Host interface {
	// Context ends when Flint quits; nil until Flint has launched.
	Context() context.Context
	// Settings is a copy of the current settings (sections: Section.Get).
	Settings() settings.Settings
	// Roots are the open project folders, the primary one first.
	Roots() []string
	// Emit sends an event to the frontend (dropped before launch).
	Emit(event string, data ...any)
	// OpenURL opens an http(s) link in the default browser.
	OpenURL(link string) error
	// Version is Flint's version ("v1.2.0", or "dev…" for builds from source).
	Version() string
	// LoadState decodes the feature's state.json key into v (left as is when
	// the key is absent); SaveState replaces it. Keys must be in Module.State.
	LoadState(key string, v any) error
	SaveState(key string, v any) error
	// StartStream shows st in the terminal tab termID (the frontend picked the
	// id) and lets agents read it like a terminal (get_terminal_output).
	StartStream(termID, title string, st terminal.Stream) error
	// PickFolder shows the native folder picker; "" when cancelled.
	PickFolder(title string) (string, error)
	// Ask shows a native question over the window and reports whether the
	// user chose ok. Go shows it and reads the click, so code in the web view
	// can neither answer nor skip it: use it before anything the user must
	// approve (installing an extension). The server build always says yes.
	Ask(title, message, ok string) bool
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Register checks the modules and registers their settings sections and
// tools; call it once, before settings are loaded.
func Register(mods []Module) error {
	seen := map[string]bool{}
	stateKeys := map[string]string{}
	for _, m := range mods {
		if !idPattern.MatchString(m.ID) {
			return fmt.Errorf("feature: invalid id %q", m.ID)
		}
		if seen[m.ID] {
			return fmt.Errorf("feature: %q listed twice", m.ID)
		}
		seen[m.ID] = true
		for _, key := range m.State {
			if other, ok := stateKeys[key]; ok {
				return fmt.Errorf("feature: state key %q used by %s and %s", key, other, m.ID)
			}
			stateKeys[key] = m.ID
		}
	}
	for _, m := range mods {
		settings.Register(m.Settings...)
		tools.Register(m.Tools...)
	}
	return nil
}

// StateKeys are the state.json keys the modules keep.
func StateKeys(mods []Module) []string {
	var out []string
	for _, m := range mods {
		out = append(out, m.State...)
	}
	return out
}

// Set is the running modules; a nil Set has none.
type Set struct {
	parts []Instance
}

// New builds every module that has running parts, in list order.
func New(mods []Module, h Host) *Set {
	s := &Set{}
	for _, m := range mods {
		if m.New != nil {
			s.parts = append(s.parts, m.New(h))
		}
	}
	return s
}

// Services are every module's bound services.
func (s *Set) Services() []application.Service {
	if s == nil {
		return nil
	}
	var out []application.Service
	for _, p := range s.parts {
		out = append(out, p.Services...)
	}
	return out
}

// AgentTools are every module's tools for agents right now.
func (s *Set) AgentTools() []mcpserver.Tool {
	if s == nil {
		return nil
	}
	var out []mcpserver.Tool
	for _, p := range s.parts {
		if p.AgentTools != nil {
			out = append(out, p.AgentTools()...)
		}
	}
	return out
}

// Start runs the Start hooks.
func (s *Set) Start(ctx context.Context) {
	if s == nil {
		return
	}
	for _, p := range s.parts {
		if p.Start != nil {
			p.Start(ctx)
		}
	}
}

// Closers are the Close hooks, for the app's parallel shutdown.
func (s *Set) Closers() []func() {
	if s == nil {
		return nil
	}
	var out []func()
	for _, p := range s.parts {
		if p.Close != nil {
			out = append(out, p.Close)
		}
	}
	return slices.Clip(out)
}
