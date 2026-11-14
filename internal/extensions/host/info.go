package host

import (
	"runtime"
	"slices"
	"strings"

	"flint/internal/extensions/manifest"
)

// Info describes an installed extension for Settings ▸ Extensions and for
// the frontend, which turns its contributions into commands, panels, a
// Settings page and status items.
type Info struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
	Dir         string `json:"dir"`
	// Program is the command and arguments that run on this platform, as the
	// manifest has them; "" for an extension that only contributes data.
	Program string `json:"program"`
	Enabled bool   `json:"enabled"`
	// State is one of the State constants.
	State string `json:"state"`
	// Error says why it is invalid, or why its process last ended on its own.
	Error string `json:"error"`
	// Warnings are parts of the manifest this Flint ignores.
	Warnings    []string `json:"warnings"`
	Permissions []string `json:"permissions"`
	// PermissionTexts explain Permissions, in the same order.
	PermissionTexts []string           `json:"permissionTexts"`
	RunsAtStartup   bool               `json:"runsAtStartup"`
	Commands        []manifest.Command `json:"commands"`
	Panels          []manifest.Panel   `json:"panels"`
	StatusItems     []StatusItemInfo   `json:"statusItems"`
	Settings        *manifest.Schema   `json:"settings"`
	AgentTools      []ToolInfo         `json:"agentTools"`
	// TabViews are the kinds of tabs the extension opens (webviews).
	TabViews []manifest.TabView `json:"tabViews"`
	// Themes are color themes, already checked with the custom theme rules.
	Themes []manifest.Theme `json:"themes"`
	// Languages are the programming languages it adds (files, comments,
	// language server, highlighting grammar).
	Languages []manifest.Language `json:"languages"`
	// Debuggers and TestRunners debug and test files of some languages.
	Debuggers   []manifest.Debugger   `json:"debuggers"`
	TestRunners []manifest.TestRunner `json:"testRunners"`
	// DecorationSets are the groups of editor decorations it draws.
	DecorationSets []DecorationSetInfo `json:"decorationSets"`
	// Sandbox says how the program is confined, or why it isn't.
	Sandbox SandboxInfo `json:"sandbox"`
}

// DecorationSetInfo is a contributed decoration set, for Settings and the
// palette: the files it is for in words, and its state.
type DecorationSetInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Default is whether it is on unless the user says otherwise; Enabled
	// whether it is on now.
	Default bool `json:"default"`
	Enabled bool `json:"enabled"`
	// Files names the files it is for ("go, .md, *.test.ts").
	Files string `json:"files"`
	// UnsavedText: it sees the text of unsaved edits.
	UnsavedText bool `json:"unsavedText"`
}

// StatusItemInfo is a contributed status item with its last text.
type StatusItemInfo struct {
	ID       string `json:"id"`
	Order    int    `json:"order"`
	Text     string `json:"text"`
	Tooltip  string `json:"tooltip"`
	Command  string `json:"command"`
	Severity string `json:"severity"`
}

// ToolInfo is a contributed agent tool and the name agents see.
type ToolInfo struct {
	Name        string `json:"name"`
	AgentName   string `json:"agentName"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Acts: the tool changes something, so Flint asks before each call.
	Acts bool `json:"acts"`
}

func (e *ext) info() Info {
	in := Info{
		ID: e.id, Name: e.id, Dir: e.dir, Enabled: e.h.o.App.Enabled(e.id), State: e.state(),
		Warnings: []string{}, Permissions: []string{}, PermissionTexts: []string{}, Commands: []manifest.Command{}, Panels: []manifest.Panel{},
		StatusItems: []StatusItemInfo{}, AgentTools: []ToolInfo{}, Themes: []manifest.Theme{}, TabViews: []manifest.TabView{}, Languages: []manifest.Language{},
		Debuggers: []manifest.Debugger{}, TestRunners: []manifest.TestRunner{}, DecorationSets: []DecorationSetInfo{},
		Sandbox: SandboxInfo{State: SandboxNone, Details: []string{}},
	}
	if e.m == nil {
		in.Error = errText(e.loadErr)
		return in
	}
	m := e.m
	in.Name, in.Version, in.Publisher, in.Description = m.Name, m.Version, m.Publisher, m.Description
	in.Program = m.CommandLine(runtime.GOOS)
	in.Sandbox = e.sandboxInfo()
	for _, t := range m.Contributes.Themes {
		th := t.CustomTheme() // copies of the maps
		in.Themes = append(in.Themes, manifest.Theme{ID: t.ID, Label: t.Label, Base: t.Base, Colors: th.Colors, Terminal: th.Terminal})
	}
	in.Permissions = slices.Clone(m.Permissions)
	for _, p := range m.Permissions {
		in.PermissionTexts = append(in.PermissionTexts, PermissionText(p))
	}
	in.RunsAtStartup = m.RunsAtStartup()
	in.Commands = slices.Clone(m.Contributes.Commands)
	in.Panels = slices.Clone(m.Contributes.Panels)
	in.TabViews = slices.Clone(m.Contributes.TabViews)
	in.Languages = slices.Clone(m.Contributes.Languages)
	in.Debuggers = slices.Clone(m.Contributes.Debuggers)
	in.TestRunners = slices.Clone(m.Contributes.TestRunners)
	in.Settings = m.Contributes.Settings
	for _, s := range m.Contributes.DecorationSets {
		in.DecorationSets = append(in.DecorationSets, DecorationSetInfo{
			ID: s.ID, Title: s.Title, Default: s.On(), Enabled: e.h.o.App.SetEnabled(s.ID, s.On()),
			Files: strings.Join(slices.Concat(s.Languages, s.Extensions, s.Globs), ", "), UnsavedText: s.UnsavedText,
		})
	}
	for _, k := range m.Contributes.Unsupported {
		in.Warnings = append(in.Warnings, "contributes."+k+" is not supported by this version of Flint and is ignored")
	}
	e.mu.Lock()
	in.Error = e.lastErr
	for _, s := range m.Contributes.StatusItems {
		t := e.status[s.ID]
		in.StatusItems = append(in.StatusItems, StatusItemInfo{ID: s.ID, Order: s.Order, Text: t.Text, Tooltip: t.Tooltip, Command: t.Command, Severity: t.Severity})
	}
	e.mu.Unlock()
	names := e.h.ToolNames(e.id)
	for _, t := range m.Contributes.AgentTools {
		in.AgentTools = append(in.AgentTools, ToolInfo{Name: t.Name, AgentName: names[t.Name], Title: t.Title, Description: t.Description, Acts: t.Acts})
	}
	return in
}
