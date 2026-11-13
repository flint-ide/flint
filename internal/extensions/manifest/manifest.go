// Package manifest reads and checks an external extension's manifest,
// flint-extension.json (docs/design/extensions.md, level 2): who the
// extension is, the program Flint starts, what it may ask Flint for
// (permissions) and what it adds to the UI (contributions). Everything here
// is data: nothing is started, and a manifest that breaks a rule is refused
// as a whole with an error naming the rule.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"flint/internal/settings"
)

const (
	// FileName is the manifest's name inside the extension's directory.
	FileName = "flint-extension.json"
	// Contract is the major version of the extension contract this Flint
	// speaks (the manifest's "contract" and initialize's "contract").
	Contract = 1
	// maxFileSize bounds the manifest file.
	maxFileSize = 256 << 10
)

// Manifest is a parsed, checked flint-extension.json.
type Manifest struct {
	Contract    int         `json:"contract"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Publisher   string      `json:"publisher,omitempty"`
	Description string      `json:"description,omitempty"`
	Run         Run         `json:"run"`
	Activation  []string    `json:"activation,omitempty"`
	Permissions []string    `json:"permissions,omitempty"`
	Contributes Contributes `json:"contributes"`
}

// Run is how Flint starts the program: a path inside the extension's
// directory and fixed arguments, never a shell or a PATH lookup. darwin and
// linux replace both on that platform.
type Run struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	// Env names variables of the user's environment (the login shell's) the
	// program gets besides Flint's minimal set, e.g. KUBECONFIG (runenv.go).
	Env    []string `json:"env,omitempty"`
	Darwin *RunOS   `json:"darwin,omitempty"`
	Linux  *RunOS   `json:"linux,omitempty"`
}

// RunOS is a platform's Run.
type RunOS struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Contributes is what the extension adds to Flint's UI and to its agents.
type Contributes struct {
	Commands    []Command    `json:"commands,omitempty"`
	Panels      []Panel      `json:"panels,omitempty"`
	Settings    *Schema      `json:"settings,omitempty"`
	StatusItems []StatusItem `json:"statusItems,omitempty"`
	AgentTools  []AgentTool  `json:"agentTools,omitempty"`
	Themes      []Theme      `json:"themes,omitempty"`
	TabViews    []TabView    `json:"tabViews,omitempty"`
	Languages   []Language   `json:"languages,omitempty"`
	Debuggers   []Debugger   `json:"debuggers,omitempty"`
	TestRunners []TestRunner `json:"testRunners,omitempty"`
	// DecorationSets are groups of marks it draws in editors (decorations.go).
	DecorationSets []DecorationSet `json:"decorationSets,omitempty"`
	// Unsupported names contribution points this Flint doesn't offer yet
	// (fileMenu): ignored, and listed as warnings in Settings.
	Unsupported []string `json:"-"`
}

// Command is a palette command; Keys is an optional default shortcut that
// never overrides one Flint or the user already has.
type Command struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
	Keys     string `json:"keys,omitempty"`
}

// Panel is a sidebar panel drawn by Flint from view/resolve's data.
type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Icon is a name from Flint's icon set (frontend/src/components/ui/icons.ts).
	Icon  string `json:"icon"`
	Order int    `json:"order"`
	// View is "tree", "list", "table", "form", "chart" (views.go) or "webview".
	View string `json:"view"`
	// Webview is a "webview" panel's HTML (webview.go).
	Webview *Webview `json:"webview,omitempty"`
}

// StatusItem is a status bar item whose text comes from status/update.
type StatusItem struct {
	ID    string `json:"id"`
	Order int    `json:"order"`
}

// AgentTool is a tool offered to agents on Flint's MCP server: read-only,
// unless Acts.
type AgentTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	// Acts declares that the tool changes something (it starts, sends,
	// writes or deletes): Flint asks the user before every call.
	Acts bool `json:"acts,omitempty"`
}

// Theme is a color theme: the CSS variables and terminal colors of a custom
// theme (settings.CustomTheme), checked by the same rules
// (settings.ValidateCustomTheme). It is data: no program serves it.
type Theme struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Base     string            `json:"base"`
	Colors   map[string]string `json:"colors"`
	Terminal map[string]string `json:"terminal,omitempty"`
}

// CustomTheme is the theme as Settings ▸ Appearance applies it: a custom
// theme whose Source is the theme's id.
func (t Theme) CustomTheme() settings.CustomTheme {
	colors := maps.Clone(t.Colors)
	if colors == nil {
		colors = map[string]string{}
	}
	var terminal map[string]string
	if len(t.Terminal) > 0 {
		terminal = maps.Clone(t.Terminal)
	}
	return settings.CustomTheme{Name: t.Label, Base: t.Base, Colors: colors, Terminal: terminal, Source: t.ID}
}

// Load reads and checks dir's manifest, including that its program, if it
// has one, exists inside dir.
func Load(dir string) (*Manifest, error) {
	path := filepath.Join(dir, FileName)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no %s in %s", FileName, dir)
	}
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is larger than %d KiB", FileName, maxFileSize>>10)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if m.HasProgram() {
		if _, _, err := m.Program(dir); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// HasProgram: the manifest names a program. An extension that only
// contributes data (themes, settings) has none and never runs code.
func (m *Manifest) HasProgram() bool {
	return m.Run.Command != "" || m.Run.Darwin != nil || m.Run.Linux != nil
}

// NeedsProgram: something the manifest contributes is served by a running
// program (commands, panels, status items, agent tools, decoration sets,
// activation events).
func (m *Manifest) NeedsProgram() bool {
	c := m.Contributes
	return len(c.Commands)+len(c.Panels)+len(c.StatusItems)+len(c.AgentTools)+len(c.DecorationSets)+len(m.Activation) > 0
}

// Parse decodes and checks a manifest.
func Parse(data []byte) (*Manifest, error) {
	var top struct {
		Manifest
		Contributes map[string]json.RawMessage `json:"contributes"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	m := top.Manifest
	m.Contributes = Contributes{}
	if top.Contract != Contract {
		return nil, contractError(top.Contract)
	}
	for _, key := range sortedKeys(top.Contributes) {
		raw := top.Contributes[key]
		var err error
		switch key {
		case "commands":
			err = json.Unmarshal(raw, &m.Contributes.Commands)
		case "panels":
			err = json.Unmarshal(raw, &m.Contributes.Panels)
		case "statusItems":
			err = json.Unmarshal(raw, &m.Contributes.StatusItems)
		case "agentTools":
			err = json.Unmarshal(raw, &m.Contributes.AgentTools)
		case "themes":
			err = json.Unmarshal(raw, &m.Contributes.Themes)
		case "tabViews":
			err = json.Unmarshal(raw, &m.Contributes.TabViews)
		case "languages":
			err = json.Unmarshal(raw, &m.Contributes.Languages)
		case "debuggers":
			err = json.Unmarshal(raw, &m.Contributes.Debuggers)
		case "testRunners":
			err = json.Unmarshal(raw, &m.Contributes.TestRunners)
		case "decorationSets":
			err = json.Unmarshal(raw, &m.Contributes.DecorationSets)
		case "settings":
			m.Contributes.Settings, err = parseSchema(raw)
		default:
			m.Contributes.Unsupported = append(m.Contributes.Unsupported, key)
		}
		if err != nil {
			return nil, fmt.Errorf("contributes.%s: %w", key, err)
		}
	}
	if err := m.check(); err != nil {
		return nil, err
	}
	return &m, nil
}

func contractError(v int) error {
	if v == 0 {
		return fmt.Errorf(`"contract" is missing: this Flint supports extension contract %d`, Contract)
	}
	return fmt.Errorf("built for Flint's extension contract %d; this Flint supports %d", v, Contract)
}

// Program is the absolute path of the program to start on this platform and
// its arguments. The path must stay inside dir, symbolic links included, and
// be an executable file.
func (m *Manifest) Program(dir string) (string, []string, error) {
	command, args := m.Run.Command, m.Run.Args
	if o := m.Run.forOS(runtime.GOOS); o != nil {
		command, args = o.Command, o.Args
	}
	if err := checkCommandPath(command); err != nil {
		return "", nil, err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", nil, err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(command)))
	if err != nil {
		return "", nil, fmt.Errorf("run.command %q: %w", command, err)
	}
	if !within(root, path) {
		return "", nil, fmt.Errorf("run.command %q leads outside the extension's folder", command)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", nil, fmt.Errorf("run.command %q is not an executable file", command)
	}
	return path, slices.Clone(args), nil
}

// CommandLine is the program and arguments that run on goos, as the manifest
// writes them (the platform's override, if any): what the install dialog and
// Settings show. "" when the extension has no program.
func (m *Manifest) CommandLine(goos string) string {
	if !m.HasProgram() {
		return ""
	}
	command, args := m.Run.Command, m.Run.Args
	if o := m.Run.forOS(goos); o != nil {
		command, args = o.Command, o.Args
	}
	return strings.Join(append([]string{command}, args...), " ")
}

// ProgramPath is the program's path inside the extension's folder on goos,
// as the manifest writes it (the platform's override, if any); "" when the
// extension has no program.
func (m *Manifest) ProgramPath(goos string) string {
	if o := m.Run.forOS(goos); o != nil {
		return o.Command
	}
	return m.Run.Command
}

func (r Run) forOS(goos string) *RunOS {
	switch goos {
	case "darwin":
		return r.Darwin
	case "linux":
		return r.Linux
	}
	return nil
}

// within: path is root or inside it (both cleaned and absolute).
func within(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// HasPermission reports whether the manifest declares perm ("secrets"); a
// network permission is declared per host and only shown, never checked.
func (m *Manifest) HasPermission(perm string) bool { return slices.Contains(m.Permissions, perm) }

// RunsAtStartup: the extension declared onStartup.
func (m *Manifest) RunsAtStartup() bool { return slices.Contains(m.Activation, "onStartup") }

// Command returns the contributed command id, if there is one.
func (m *Manifest) Command(id string) (Command, bool) {
	i := slices.IndexFunc(m.Contributes.Commands, func(c Command) bool { return c.ID == id })
	if i < 0 {
		return Command{}, false
	}
	return m.Contributes.Commands[i], true
}

// HasPanel reports whether the extension contributes the panel id.
func (m *Manifest) HasPanel(id string) bool {
	return slices.ContainsFunc(m.Contributes.Panels, func(p Panel) bool { return p.ID == id })
}

// HasStatusItem reports whether the extension contributes the status item id.
func (m *Manifest) HasStatusItem(id string) bool {
	return slices.ContainsFunc(m.Contributes.StatusItems, func(s StatusItem) bool { return s.ID == id })
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
