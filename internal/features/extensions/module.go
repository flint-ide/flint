// Package extensions is the feature behind external extensions
// (docs/design/extensions.md, level 2): separate programs the user installs
// into <config>/extensions/<id>/, run by internal/extensions/host and drawn
// by Flint from data. This package is the level-1 module that hosts them: the
// "extensions" settings section, the bound API the frontend uses to show
// their contributions (frontend/src/features/extensions), their tools on
// Flint's MCP server, and the hooks that keep running extensions in step
// with settings and workspace folders.
package extensions

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/extensions/catalog"
	"flint/internal/extensions/host"
	"flint/internal/feature"
	"flint/internal/mcpserver"
	"flint/internal/settings"
	"flint/internal/shellenv"
)

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       "extensions",
	Settings: []settings.AnySection{Section},
	New: func(h feature.Host) feature.Instance {
		langs := &languageSync{}
		hst := host.New(host.Options{
			Dir:     filepath.Join(h.ConfigDir(), "extensions"),
			App:     app{h: h, changed: func() { go langs.sync() }},
			Secrets: host.Keyring{},
		})
		langs.list = hst.List
		cat := catalog.New(catalog.Options{Dir: catalogDir(h), Before: shellenv.Wait})
		api := &API{host: hst, catalog: cat, fh: h, langs: langs}
		return feature.Instance{
			Services: []application.Service{application.NewService(api)},
			AgentTools: func() []mcpserver.Tool {
				s := Section.Get(h.Settings())
				return hst.Tools(s.toolsOffered)
			},
			SettingsChanged: func(prev, next settings.Settings) { go settingsChanged(hst, Section.Get(prev), Section.Get(next)) },
			RootsChanged:    func() { go hst.RootsChanged() },
			Close:           hst.Close,
		}
	},
}

// catalogDir is where the extensions repository is cloned: Flint's cache
// folder, else next to its config (never inside the extensions folder, which
// holds only installed ones).
func catalogDir(h feature.Host) string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "Flint", "extensions-repo")
	}
	return filepath.Join(h.ConfigDir(), "extensions-repo")
}

// settingsChanged tells the host about each extension whose settings or
// enabled state changed.
func settingsChanged(h *host.Host, prev, next Settings) {
	ids := map[string]bool{}
	for id := range prev.Values {
		ids[id] = true
	}
	for id := range next.Values {
		ids[id] = true
	}
	for id := range prev.Enabled {
		ids[id] = true
	}
	for id := range next.Enabled {
		ids[id] = true
	}
	for id := range ids {
		if string(prev.Values[id]) != string(next.Values[id]) || prev.enabled(id) != next.enabled(id) {
			h.SettingsChanged(id)
		}
	}
}

// app is what the host sees of Flint; changed runs when the list or a state
// changed (host.EventChanged), besides telling the frontend.
type app struct {
	h       feature.Host
	changed func()
}

func (a app) Roots() []string          { return a.h.Roots() }
func (a app) Trusted(path string) bool { return a.h.Trusted(path) }
func (a app) Emit(event string, data ...any) {
	if event == host.EventChanged && a.changed != nil {
		a.changed()
	}
	a.h.Emit(event, data...)
}
func (a app) OpenURL(link string) error          { return a.h.OpenURL(link) }
func (a app) Version() string                    { return a.h.Version() }
func (a app) Enabled(id string) bool             { return Section.Get(a.h.Settings()).enabled(id) }
func (a app) Settings(id string) json.RawMessage { return Section.Get(a.h.Settings()).Values[id] }
func (a app) Confirm(title, msg, ok string) bool { return a.h.Confirm(title, msg, ok) }
func (a app) Choose(title, msg string, buttons []string) string {
	return a.h.Choose(title, msg, buttons)
}
