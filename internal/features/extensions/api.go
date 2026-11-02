package extensions

import (
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"flint/internal/extensions/installed"
	"flint/internal/extensions/manifest"
	"flint/internal/feature"
)

// EventChanged tells the frontend that the installed extensions changed.
const EventChanged = "extensions:changed"

// API is the extensions service bound to the frontend.
type API struct {
	host feature.Host
	root func() (string, error)
	mu   sync.Mutex // one install or uninstall at a time
}

// Extension is an installed extension as Settings ▸ Extensions shows it.
type Extension struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
	Dir         string `json:"dir"`
	// Error is why the extension can't be used (a broken manifest); the
	// other fields but ID and Dir are then empty.
	Error       string   `json:"error,omitempty"`
	Permissions []string `json:"permissions"`
	// Program: the extension has a program to run for commands, views and
	// the like, which this version of Flint does not start yet.
	Program     bool     `json:"program"`
	Contributes []string `json:"contributes"`
	Themes      []Theme  `json:"themes"`
}

// Theme is a color theme an extension contributes, already checked with
// the custom theme rules (settings.ValidateCustomTheme).
type Theme struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Base     string            `json:"base"`
	Colors   map[string]string `json:"colors"`
	Terminal map[string]string `json:"terminal"`
}

// List returns the installed extensions, sorted by id.
func (a *API) List() ([]Extension, error) {
	root, err := a.root()
	if err != nil {
		return nil, err
	}
	exts, err := installed.Discover(root)
	out := make([]Extension, 0, len(exts))
	for _, e := range exts {
		out = append(out, describe(e))
	}
	return out, err
}

// InstallFromFolder asks for a folder, shows its manifest and permissions in
// a native dialog and, once the user agrees, copies it into the extensions
// folder. nil without an error means the user cancelled.
func (a *API) InstallFromFolder() (*Extension, error) {
	src, err := a.host.PickFolder("Install Extension from Folder")
	if err != nil || src == "" {
		return nil, err
	}
	m, err := manifest.Load(src)
	if err != nil {
		return nil, fmt.Errorf("can't install %s: %w", src, err)
	}
	root, err := a.root()
	if err != nil {
		return nil, err
	}
	replaces, _ := a.installed(root, m.ID)
	if !a.host.Ask(fmt.Sprintf("Install the extension %q?", m.Name), installMessage(m, src, replaces), "Install") {
		return nil, nil
	}
	a.mu.Lock()
	ext, err := installed.Install(src, root, m)
	a.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("install %s: %w", m.ID, err)
	}
	a.host.Emit(EventChanged)
	d := describe(ext)
	return &d, nil
}

// Uninstall deletes an extension's folder after the user confirms it in a
// native dialog; false means they cancelled. A theme taken from it stays in
// use: Settings keeps its own copy.
func (a *API) Uninstall(id string) (bool, error) {
	root, err := a.root()
	if err != nil {
		return false, err
	}
	// The web view sends the id: only an exact folder name of an installed
	// extension gets a dialog, so page code can't show text of its choosing.
	ext, ok := a.installed(root, id)
	if !ok {
		return false, fmt.Errorf("%q is not installed", id)
	}
	name := ext.ID()
	if ext.Err == nil {
		name = ext.Manifest.Name
	}
	if !a.host.Ask(fmt.Sprintf("Uninstall %q?", name), fmt.Sprintf("Flint deletes the folder of %s from its extensions folder. A color theme you took from it stays selected.", ext.ID()), "Uninstall") {
		return false, nil
	}
	a.mu.Lock()
	err = installed.Remove(root, filepath.Base(ext.Dir))
	a.mu.Unlock()
	if err != nil {
		return false, err
	}
	a.host.Emit(EventChanged)
	return true, nil
}

// installed finds the extension whose folder is named exactly id.
func (a *API) installed(root, id string) (installed.Extension, bool) {
	exts, _ := installed.Discover(root)
	for _, e := range exts {
		if filepath.Base(e.Dir) == id {
			return e, true
		}
	}
	return installed.Extension{}, false
}

func describe(e installed.Extension) Extension {
	if e.Err != nil {
		return Extension{ID: e.ID(), Dir: e.Dir, Error: e.Err.Error(), Permissions: []string{}, Contributes: []string{}, Themes: []Theme{}}
	}
	m := e.Manifest
	out := Extension{
		ID: m.ID, Name: m.Name, Version: m.Version, Publisher: m.Publisher, Description: m.Description, Dir: e.Dir,
		Permissions: append([]string{}, m.Permissions...), Program: m.Run != nil, Contributes: contributions(m), Themes: []Theme{},
	}
	for _, t := range m.Contributes.Themes {
		terminal := maps.Clone(t.Terminal)
		if terminal == nil {
			terminal = map[string]string{}
		}
		out.Themes = append(out.Themes, Theme{ID: t.ID, Label: t.Label, Base: t.Base, Colors: maps.Clone(t.Colors), Terminal: terminal})
	}
	return out
}

// contributions describes what a manifest adds, one phrase per kind.
func contributions(m manifest.Manifest) []string {
	c := m.Contributes
	settingsCount := 0
	if c.Settings != nil {
		settingsCount = len(c.Settings.Properties)
	}
	kinds := []struct {
		n             int
		single, multi string
	}{
		{len(c.Themes), "color theme", "color themes"},
		{len(c.Commands), "command", "commands"},
		{len(c.Panels), "panel", "panels"},
		{len(c.TabViews), "tab view", "tab views"},
		{len(c.StatusItems), "status bar item", "status bar items"},
		{len(c.FileMenu), "file menu entry", "file menu entries"},
		{len(c.AgentTools), "agent tool", "agent tools"},
		{settingsCount, "setting", "settings"},
	}
	out := []string{}
	for _, k := range kinds {
		switch {
		case k.n == 1:
			out = append(out, "1 "+k.single)
		case k.n > 1:
			out = append(out, fmt.Sprintf("%d %s", k.n, k.multi))
		}
	}
	return out
}

// installMessage is the install dialog's text: everything the user agrees
// to. replaces is the installed extension with the same id, if any.
func installMessage(m manifest.Manifest, src string, replaces installed.Extension) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s (%s)", m.Name, m.Version, m.ID)
	if m.Publisher != "" {
		fmt.Fprintf(&b, " by %s", m.Publisher)
	}
	b.WriteString("\nFrom: " + src + "\n")
	switch {
	case replaces.Dir != "" && replaces.Err == nil:
		old := replaces.Manifest
		fmt.Fprintf(&b, "\nReplaces the installed %s %s", old.Name, old.Version)
		if old.Publisher != "" {
			fmt.Fprintf(&b, " by %s", old.Publisher)
		}
		b.WriteString(".\n")
	case replaces.Dir != "":
		b.WriteString("\nReplaces the installed, disabled extension with this id.\n")
	}
	if m.Description != "" {
		b.WriteString("\n" + m.Description + "\n")
	}
	if list := contributions(m); len(list) > 0 {
		b.WriteString("\nAdds: " + strings.Join(list, ", ") + "\n")
	}
	if m.Run != nil {
		prog := m.Run.For(runtime.GOOS)
		fmt.Fprintf(&b, "\nProgram: %s\nIt runs with your user's permissions when its features are used. This version of Flint doesn't start extension programs yet: only themes take effect.\n", strings.Join(append([]string{prog.Command}, prog.Args...), " "))
		for _, ev := range m.Activation {
			if ev == "onStartup" {
				b.WriteString("It asks to run at startup.\n")
			}
		}
	}
	if len(m.Permissions) > 0 {
		b.WriteString("\nPermissions: " + strings.Join(m.Permissions, ", ") + "\n")
	}
	b.WriteString("\nInstall only extensions you trust.")
	return b.String()
}
