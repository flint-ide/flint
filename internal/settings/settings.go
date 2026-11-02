// Package settings holds the user's preferences (settings.json in the config
// directory): theme (and a custom palette), editor fonts and Vim mode, terminal
// fonts, the default agent, agent CLIs, custom ACP agents, language servers
// and keyboard shortcut overrides, plus the sections features
// register for their own keys (section.go). Loading is forgiving (a hand-edited file never stops Flint
// from starting: bad values fall back to defaults, unknown keys are ignored);
// saving is strict. Nothing secret belongs here: credentials live in the Keychain.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"flint/internal/agent"
	"flint/internal/config"
)

// FileName is the settings file inside the config directory.
const FileName = "settings.json"

// Version is the current schema version, written on every save. Older files are
// read as-is (missing keys get defaults); a migration goes in decode when a key
// changes meaning.
const Version = 1

// Themes.
const (
	ThemeDark   = "dark"
	ThemeLight  = "light"
	ThemeSystem = "system" // follows macOS appearance
)

// Limits for numeric and free-text values.
const (
	MinFontSize      = 8
	MaxFontSize      = 32
	MinLineHeight    = 1.0
	MaxLineHeight    = 2.5
	maxFontFamilyLen = 200
	maxKeymapEntries = 1000
	maxCommandIDLen  = 200
	maxKeysLen       = 64
	maxExcludes      = 100
	maxExcludeLen    = 200
)

// Font is a monospace font setup shared by the editor and terminal sections.
type Font struct {
	FontFamily string  `json:"fontFamily"` // CSS font-family list
	FontSize   int     `json:"fontSize"`   // px
	LineHeight float64 `json:"lineHeight"` // multiple of the font size
}

// EditorSettings is the editor's font plus editing behaviour.
type EditorSettings struct {
	Font
	// VimMode turns on Vim key bindings in every editor.
	VimMode bool `json:"vimMode"`
}

// Settings is the whole settings.json document.
type Settings struct {
	Version  int            `json:"version"`
	Theme    string         `json:"theme"`
	Editor   EditorSettings `json:"editor"`
	Terminal Font           `json:"terminal"`
	// CustomTheme is the palette used when Theme is "custom".
	CustomTheme CustomTheme `json:"customTheme"`
	// DefaultAgent starts new agent chats ("claude", "codex" or a custom
	// agent's id); empty means the first installed one.
	DefaultAgent string `json:"defaultAgent"`
	// AutoSave saves modified files when the window loses focus.
	AutoSave bool `json:"autoSave"`
	// NotifyInBackground shows a notification and a Dock badge when a run, a
	// test run or an agent finishes while Flint is in the background.
	NotifyInBackground bool `json:"notifyInBackground"`
	// Keymap overrides command shortcuts: command id → "Mod+Shift+X", or "" to
	// remove the shortcut. Commands not listed keep their default keys.
	Keymap map[string]string `json:"keymap"`
	// WatchExclude are folder names (or globs over one) the file watcher
	// doesn't watch on Linux, on top of the dependency folders (watch.go).
	WatchExclude []string `json:"watchExclude"`
	// Agents configures the agent CLIs by name ("claude", "codex").
	Agents map[string]AgentSettings `json:"agents"`
	// CustomAgents are ACP agents added by the user (see CustomAgent).
	CustomAgents []CustomAgent `json:"customAgents"`
	// LanguageServers configures language servers by language ("go", …).
	LanguageServers map[string]LanguageServerSettings `json:"languageServers"`
	// MCPServer configures what Flint's own MCP server tells agents.
	MCPServer MCPServer `json:"mcpServer"`
	// Completions configures inline suggestions from a code model (editorai.go).
	Completions Completions `json:"completions"`
	// InlineEdit configures "Edit with Agent" on the selection (editorai.go).
	InlineEdit InlineEdit `json:"inlineEdit"`
	// AgentTasks configures agent tasks (worktree folder, branch prefix).
	AgentTasks AgentTasks `json:"agentTasks"`
	// Usage configures the usage dashboard: retention, budgets, prices (usage.go).
	Usage Usage `json:"usage"`
	// Prompts configures the prompt library (prompts.go).
	Prompts Prompts `json:"prompts"`
	// AgentReview configures Review Changes with Agent (review.go).
	AgentReview AgentReview `json:"agentReview"`
	// CI configures following your pull requests' checks (ci.go).
	CI CI `json:"ci"`
	// GitHub configures the GitHub host, sign-in and API limits (github.go).
	GitHub GitHub `json:"github"`

	// sections are the values of the keys features own (section.go), by key.
	sections map[string]any
}

// Default returns the settings Flint uses when nothing is configured.
func Default() Settings {
	return Settings{
		Version: Version,
		Theme:   ThemeDark,
		Editor: EditorSettings{Font: Font{
			FontFamily: `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace`,
			FontSize:   13,
			LineHeight: 1.55,
		}},
		Terminal: Font{
			FontFamily: `"JetBrains Mono", "SF Mono", Menlo, monospace`,
			FontSize:   13,
			LineHeight: 1.2,
		},
		NotifyInBackground: true,
		Keymap:             map[string]string{},
		WatchExclude:       defaultWatchExclude(),
		Agents:             map[string]AgentSettings{},
		CustomAgents:       []CustomAgent{},
		LanguageServers:    map[string]LanguageServerSettings{},
		MCPServer:          defaultMCPServer(),
		Completions:        defaultCompletions(),
		InlineEdit:         defaultInlineEdit(),
		AgentTasks:         defaultAgentTasks(),
		Usage:              defaultUsage(),
		Prompts:            defaultPrompts(),
		AgentReview:        defaultAgentReview(),
		CI:                 defaultCI(),
		GitHub:             defaultGitHub(),
		CustomTheme:        defaultCustomTheme(),
		sections:           sectionDefaults(),
	}
}

// Load reads path. A missing file gives the defaults; an unreadable or malformed
// one gives the defaults and an error (the caller logs it and keeps going).
// Values of the wrong type or out of range fall back to their defaults.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("read %s: %w", path, err)
	}
	s, err := decode(data)
	if err != nil {
		return Default(), fmt.Errorf("decode %s: %w", path, err)
	}
	return s, nil
}

// decode overlays data on the defaults, so missing keys keep them.
func decode(data []byte) (Settings, error) {
	s := Default()
	err := json.Unmarshal(data, &s)
	// A value of the wrong type ("fontSize": "big") only loses that value:
	// encoding/json keeps decoding the rest.
	var typeErr *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &typeErr) {
		return Default(), err
	}
	return Normalize(s), nil
}

// Normalize replaces every invalid value with its default and drops invalid
// keymap entries; the result always passes Validate.
func Normalize(s Settings) Settings {
	def := Default()
	out := s
	out.Version = Version
	if !validTheme(s.Theme) {
		out.Theme = def.Theme
	}
	out.Editor.Font = normalizeFont(s.Editor.Font, def.Editor.Font)
	out.CustomTheme = normalizeCustomTheme(s.CustomTheme)
	out.Terminal = normalizeFont(s.Terminal, def.Terminal)
	out.CustomAgents = normalizeCustomAgents(s.CustomAgents)
	if !validAgent(s.DefaultAgent, out.CustomAgents) {
		out.DefaultAgent = ""
	}
	out.Keymap = make(map[string]string, len(s.Keymap))
	for id, keys := range s.Keymap {
		if len(out.Keymap) >= maxKeymapEntries {
			break
		}
		if validKeymapEntry(id, keys) == nil {
			out.Keymap[id] = keys
		}
	}
	out.WatchExclude = cleanWatchExcludes(s.WatchExclude)
	out.Agents = normalizeAgents(s.Agents)
	out.LanguageServers = normalizeLanguageServers(s.LanguageServers)
	out.MCPServer = normalizeMCPServer(s.MCPServer)
	out.Completions = normalizeCompletions(s.Completions)
	out.InlineEdit = normalizeInlineEdit(s.InlineEdit)
	out.AgentTasks = normalizeAgentTasks(s.AgentTasks)
	out.Usage = normalizeUsage(s.Usage)
	out.AgentReview = normalizeAgentReview(s.AgentReview, out.CustomAgents)
	out.CI = normalizeCI(s.CI)
	out.GitHub = normalizeGitHub(s.GitHub)
	out.sections = normalizeSections(s)
	return out
}

// CleanExcludes keeps the valid exclude patterns, trimmed and unique, up to the limit.
func CleanExcludes(patterns []string) []string {
	out := []string{}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if len(out) < maxExcludes && validExclude(p) == nil && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func normalizeFont(f, def Font) Font {
	out := f
	family, err := cleanFontFamily(f.FontFamily)
	if err != nil || family == "" {
		family = def.FontFamily
	}
	out.FontFamily = family
	if f.FontSize < MinFontSize || f.FontSize > MaxFontSize {
		out.FontSize = def.FontSize
	}
	if !validLineHeight(f.LineHeight) {
		out.LineHeight = def.LineHeight
	}
	return out
}

// Validate reports the first invalid value (what Save refuses to write).
func Validate(s Settings) error {
	if !validTheme(s.Theme) {
		return fmt.Errorf("unknown theme %q", s.Theme)
	}
	for _, f := range []struct {
		name string
		font Font
	}{{"editor", s.Editor.Font}, {"terminal", s.Terminal}} {
		if err := validateFont(f.font); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if err := validateCustomTheme(s.CustomTheme); err != nil {
		return err
	}
	if err := validateCustomAgents(s.CustomAgents); err != nil {
		return err
	}
	if !validAgent(s.DefaultAgent, s.CustomAgents) {
		return fmt.Errorf("unsupported agent %q", s.DefaultAgent)
	}
	if len(s.Keymap) > maxKeymapEntries {
		return fmt.Errorf("too many shortcut overrides (max %d)", maxKeymapEntries)
	}
	for id, keys := range s.Keymap {
		if err := validKeymapEntry(id, keys); err != nil {
			return err
		}
	}
	if err := validateWatchExcludes(s.WatchExclude); err != nil {
		return err
	}
	if err := validateAgents(s.Agents); err != nil {
		return err
	}
	if err := validateLanguageServers(s.LanguageServers); err != nil {
		return err
	}
	if err := validateMCPServer(s.MCPServer); err != nil {
		return err
	}
	if err := validateCompletions(s.Completions); err != nil {
		return err
	}
	if err := validateInlineEdit(s.InlineEdit); err != nil {
		return err
	}
	if err := CheckAgentTasks(s.AgentTasks); err != nil {
		return err
	}
	if err := CheckUsage(s.Usage); err != nil {
		return err
	}
	if err := validateAgentReview(s.AgentReview, s.CustomAgents); err != nil {
		return err
	}
	if err := CheckCI(s.CI); err != nil {
		return err
	}
	if err := CheckGitHub(s.GitHub); err != nil {
		return err
	}
	return checkSections(s)
}

// CheckExcludes reports the first pattern of a list that would not be saved;
// what names the list in the error ("TODO" → "too many TODO exclude patterns").
func CheckExcludes(what string, patterns []string) error {
	if len(patterns) > maxExcludes {
		return fmt.Errorf("too many %s exclude patterns (max %d)", what, maxExcludes)
	}
	for _, p := range patterns {
		if err := validExclude(p); err != nil {
			return err
		}
	}
	return nil
}

// validExclude accepts one gitignore-style glob. Search options join excludes
// with commas, and rg reads a leading "!" as "include", so neither is allowed.
func validExclude(p string) error {
	switch {
	case p == "" || p != strings.TrimSpace(p):
		return fmt.Errorf("invalid exclude pattern %q", p)
	case len(p) > maxExcludeLen:
		return fmt.Errorf("exclude pattern longer than %d characters", maxExcludeLen)
	case strings.HasPrefix(p, "!") || strings.ContainsFunc(p, func(r rune) bool { return r == ',' || unicode.IsControl(r) }):
		return fmt.Errorf("exclude pattern %q may not start with ! or contain commas", p)
	}
	return nil
}

func validateFont(f Font) error {
	family, err := cleanFontFamily(f.FontFamily)
	if err != nil {
		return err
	}
	if family == "" {
		return errors.New("font family is empty")
	}
	if f.FontSize < MinFontSize || f.FontSize > MaxFontSize {
		return fmt.Errorf("font size %d out of range %d–%d", f.FontSize, MinFontSize, MaxFontSize)
	}
	if !validLineHeight(f.LineHeight) {
		return fmt.Errorf("line height %g out of range %g–%g", f.LineHeight, MinLineHeight, MaxLineHeight)
	}
	return nil
}

// CheckFontFamily reports why a font family would not be saved (nil when it would).
func CheckFontFamily(family string) error {
	family, err := cleanFontFamily(family)
	if err == nil && family == "" {
		err = errors.New("font family is empty")
	}
	return err
}

// cleanFontFamily trims a CSS font-family list and rejects characters that could
// end the declaration it is put in.
func cleanFontFamily(family string) (string, error) {
	family = strings.TrimSpace(family)
	if len(family) > maxFontFamilyLen {
		return "", fmt.Errorf("font family longer than %d characters", maxFontFamilyLen)
	}
	if strings.ContainsAny(family, ";{}<>\\\n\r") {
		return "", fmt.Errorf("font family %q contains invalid characters", family)
	}
	return family, nil
}

func validTheme(t string) bool {
	return t == ThemeDark || t == ThemeLight || t == ThemeSystem || t == ThemeCustom
}

func validLineHeight(h float64) bool {
	return !math.IsNaN(h) && h >= MinLineHeight && h <= MaxLineHeight
}

// validAgent: empty (the first installed), a built-in agent or a custom one.
func validAgent(name string, custom []CustomAgent) bool {
	return name == "" || agent.Valid(agent.Kind(name)) ||
		slices.ContainsFunc(custom, func(c CustomAgent) bool { return c.ID == name })
}

// keysPattern matches shortcuts in the frontend's notation ('Mod+Shift+P',
// 'Alt+F7', 'Mod++'): modifiers joined with '+', then one key.
var keysPattern = regexp.MustCompile(`^(?i:(?:mod|cmd|meta|ctrl|control|alt|option|shift)\+)*(?:\+|[^+\s]+)$`)

func validKeymapEntry(id, keys string) error {
	if id == "" || len(id) > maxCommandIDLen {
		return fmt.Errorf("invalid command id %q", id)
	}
	if keys == "" {
		return nil // shortcut removed
	}
	if len(keys) > maxKeysLen || !keysPattern.MatchString(keys) {
		return fmt.Errorf("invalid shortcut %q for %s", keys, id)
	}
	return nil
}

// Save validates s and writes it atomically (owner-only permissions); it returns
// what was written.
func Save(path string, s Settings) (Settings, error) {
	if err := Validate(s); err != nil {
		return Settings{}, err
	}
	out := s.Clone()
	out.Version = Version
	out.Editor.FontFamily, _ = cleanFontFamily(s.Editor.FontFamily)
	out.Terminal.FontFamily, _ = cleanFontFamily(s.Terminal.FontFamily)
	if err := config.SaveJSON(path, out); err != nil {
		return Settings{}, err
	}
	return out, nil
}

// Path is the settings file in the config directory dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }
