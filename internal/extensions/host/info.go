package host

import (
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
	// Program is run.command and its arguments, as the manifest has them.
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
}

func (e *ext) info() Info {
	in := Info{
		ID: e.id, Name: e.id, Dir: e.dir, Enabled: e.h.o.App.Enabled(e.id), State: e.state(),
		Warnings: []string{}, Permissions: []string{}, PermissionTexts: []string{}, Commands: []manifest.Command{}, Panels: []manifest.Panel{},
		StatusItems: []StatusItemInfo{}, AgentTools: []ToolInfo{},
	}
	if e.m == nil {
		in.Error = errText(e.loadErr)
		return in
	}
	m := e.m
	in.Name, in.Version, in.Publisher, in.Description = m.Name, m.Version, m.Publisher, m.Description
	in.Program = strings.Join(append([]string{m.Run.Command}, m.Run.Args...), " ")
	in.Permissions = slices.Clone(m.Permissions)
	for _, p := range m.Permissions {
		in.PermissionTexts = append(in.PermissionTexts, PermissionText(p))
	}
	in.RunsAtStartup = m.RunsAtStartup()
	in.Commands = slices.Clone(m.Contributes.Commands)
	in.Panels = slices.Clone(m.Contributes.Panels)
	in.Settings = m.Contributes.Settings
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
		in.AgentTools = append(in.AgentTools, ToolInfo{Name: t.Name, AgentName: names[t.Name], Title: t.Title, Description: t.Description})
	}
	return in
}
