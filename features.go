package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/config"
	"flint/internal/diag"
	"flint/internal/feature"
	"flint/internal/features/database"
	"flint/internal/features/extensions"
	"flint/internal/features/todo"
	"flint/internal/features/update"
	"flint/internal/mcptools"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// features are the feature modules (internal/feature): adding a feature is a
// package under internal/features and one line here. Its frontend half is
// listed in frontend/src/features/index.ts.
var features = []feature.Module{
	database.Module,
	todo.Module,
	update.Module,
	extensions.Module,
}

// registerFeatures registers the modules' settings sections and tools, once
// per process and before settings are loaded.
var registerFeatures = sync.OnceValue(func() error { return feature.Register(features) })

// featureStateKeys are the state.json keys features keep (appState.features).
var featureStateKeys = sync.OnceValue(func() []string { return feature.StateKeys(features) })

// featureServices are the modules' bound services, after Flint's own.
func (a *App) featureServices() []application.Service { return a.features.Services() }

// featureHost is what the features see of App.
type featureHost struct{ a *App }

var _ feature.Host = featureHost{}

func (h featureHost) Context() context.Context    { return h.a.ctx }
func (h featureHost) Settings() settings.Settings { return h.a.currentSettings() }
func (h featureHost) Roots() []string             { return h.a.projectRoots() }
func (h featureHost) Trusted(path string) bool    { return h.a.isTrusted(path) }
func (h featureHost) ConfigDir() string           { return h.a.configDir }
func (h featureHost) Confirm(title, msg, ok string) bool {
	return h.a.askUser(title, msg, ok)
}
func (h featureHost) Choose(title, msg string, buttons []string) string {
	return h.a.chooseUser(title, msg, buttons)
}
func (h featureHost) Emit(event string, data ...any)    { h.a.emit(event, data...) }
func (h featureHost) OpenURL(link string) error         { return h.a.openURL(link) }
func (h featureHost) Version() string                   { return diag.Version(version) }
func (h featureHost) LoadState(key string, v any) error { return h.a.loadFeatureState(key, v) }
func (h featureHost) SaveState(key string, v any) error { return h.a.saveFeatureState(key, v) }

// StartStream registers the console for agents (terminal tools) and starts
// the terminal session.
func (h featureHost) StartStream(termID, title string, st terminal.Stream) error {
	a := h.a
	opened := a.mcpSources.Terminals.OpenNew(termID, mcptools.KindTerminal, title, "")
	if err := a.terminals.StartStream(termID, st); err != nil {
		if opened {
			a.mcpSources.Terminals.Close(termID)
		}
		return fmt.Errorf("start %s: %w", title, err)
	}
	return nil
}

func (a *App) loadFeatureState(key string, v any) error {
	if !slices.Contains(featureStateKeys(), key) {
		return fmt.Errorf("state key %q is not declared by a feature", key)
	}
	a.stateMu.Lock()
	raw, ok := a.state.features[key]
	a.stateMu.Unlock()
	if !ok {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func (a *App) saveFeatureState(key string, v any) error {
	if !slices.Contains(featureStateKeys(), key) {
		return fmt.Errorf("state key %q is not declared by a feature", key)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	next := a.state
	next.features = maps.Clone(a.state.features)
	if next.features == nil {
		next.features = map[string]json.RawMessage{}
	}
	next.features[key] = raw
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	return nil
}

// plainState has appState's fields without its JSON methods.
type plainState appState

// MarshalJSON writes the features' keys after appState's own.
func (s appState) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(plainState(s))
	if err != nil || len(s.features) == 0 {
		return data, err
	}
	out := bytes.TrimSuffix(data, []byte("}"))
	for _, key := range slices.Sorted(maps.Keys(s.features)) {
		k, _ := json.Marshal(key)
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = append(append(append(out, k...), ':'), s.features[key]...)
	}
	return append(out, '}'), nil
}

// UnmarshalJSON keeps the declared feature keys as they are; the rest is
// decoded as usual (unknown keys are dropped, as before features had state).
func (s *appState) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, (*plainState)(s)); err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	s.features = nil
	for _, key := range featureStateKeys() {
		if raw, ok := doc[key]; ok {
			if s.features == nil {
				s.features = map[string]json.RawMessage{}
			}
			s.features[key] = raw
		}
	}
	return nil
}
