package vscodeimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"flint/internal/settings"
)

// Kinds of imported and skipped entries.
const (
	KindSetting    = "setting"
	KindKeybinding = "keybinding"
	KindTheme      = "theme"
	KindFile       = "file"
)

// Item is one thing the import changes in Flint.
type Item struct {
	Kind string `json:"kind"`
	// Name is the VS Code setting, command or theme.
	Name string `json:"name"`
	// Value is what Flint gets ("14 px", "Shift+Mod+P", "custom").
	Value string `json:"value"`
	// Target is the Flint setting or command id it goes to.
	Target string `json:"target"`
}

// Skipped is something the import leaves out, and why.
type Skipped struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Plan is what an import would change. Nothing is written until the caller
// applies it to the current settings and saves the result.
type Plan struct {
	// ID identifies the plan the user confirms (set by the caller).
	ID      string    `json:"id"`
	Source  Source    `json:"source"`
	Items   []Item    `json:"items"`
	Skipped []Skipped `json:"skipped"`
	changes changes
}

// Apply returns s with the plan's changes.
func (p Plan) Apply(s settings.Settings) settings.Settings { return p.changes.apply(s) }

// vimExtension is the VS Code Vim extension's folder prefix.
const vimExtension = "vscodevim.vim-"

// BuildPlan reads src's settings, keybindings and color theme and maps them
// onto current. Unreadable files are reported as skipped, not as errors.
func BuildPlan(src Source, current settings.Settings) Plan {
	plan := Plan{Source: src, Items: []Item{}, Skipped: []Skipped{}}
	skipFile := func(name string, err error) {
		plan.Skipped = append(plan.Skipped, Skipped{Kind: KindFile, Name: name, Reason: "could not be read: " + err.Error()})
	}

	var raw map[string]json.RawMessage
	if err := readJSONC(filepath.Join(src.UserDir, "settings.json"), maxFileSize, &raw); err != nil && !errors.Is(err, fs.ErrNotExist) {
		skipFile("settings.json", err)
	}
	c, items, skipped := mapSettings(raw, current)
	plan.Items = append(plan.Items, items...)
	plan.Skipped = append(plan.Skipped, skipped...)

	if hasExtension(src.ExtensionsDir, vimExtension) && !current.Editor.VimMode {
		on := true
		c.vimMode = &on
		plan.Items = append(plan.Items, Item{Kind: KindSetting, Name: "Vim extension (vscodevim.vim)", Value: "on", Target: "Editor ▸ Vim mode"})
	}

	themeItems, themeSkipped := mapTheme(raw, src, &c)
	plan.Items = append(plan.Items, themeItems...)
	plan.Skipped = append(plan.Skipped, themeSkipped...)

	var bindings []keybinding
	if err := readJSONC(filepath.Join(src.UserDir, "keybindings.json"), maxFileSize, &bindings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		skipFile("keybindings.json", err)
	}
	keymap, keyItems, keySkipped := mapKeybindings(bindings)
	c.keymap = keymap
	plan.Items = append(plan.Items, keyItems...)
	plan.Skipped = append(plan.Skipped, keySkipped...)

	plan.changes = c
	return plan
}

// mapTheme picks Flint's theme from workbench.colorTheme: the theme's colors
// as a custom theme when an installed extension provides it, else the dark or
// light palette.
func mapTheme(raw map[string]json.RawMessage, src Source, c *changes) ([]Item, []Skipped) {
	var autoDetect bool
	_ = json.Unmarshal(raw["window.autoDetectColorScheme"], &autoDetect)
	if autoDetect {
		c.theme = settings.ThemeSystem
		return []Item{{Kind: KindTheme, Name: "window.autoDetectColorScheme", Value: "System", Target: "Appearance ▸ Theme"}},
			[]Skipped{{Kind: KindTheme, Name: "workbench.preferredDarkColorTheme / preferredLightColorTheme", Reason: "Flint follows macOS with its own dark and light palettes"}}
	}
	var name string
	if json.Unmarshal(raw["workbench.colorTheme"], &name) != nil || strings.TrimSpace(name) == "" {
		return nil, nil
	}
	fallback := func(base, reason string) ([]Item, []Skipped) {
		c.theme = base
		label := "Dark"
		if base == settings.ThemeLight {
			label = "Light"
		}
		return []Item{{Kind: KindTheme, Name: name, Value: label, Target: "Appearance ▸ Theme"}},
			[]Skipped{{Kind: KindTheme, Name: name + " colors", Reason: reason}}
	}
	ref, err := findTheme([]string{src.ExtensionsDir}, name)
	if err != nil {
		base := settings.ThemeDark
		if strings.Contains(strings.ToLower(name), "light") {
			base = settings.ThemeLight
		}
		return fallback(base, err.Error())
	}
	t, notes, err := loadTheme(ref.path, ref.root, 0)
	if err != nil {
		return fallback(ref.base, err.Error())
	}
	custom := convertTheme(t, truncate(name, 90), ref.base)
	if len(custom.Colors) == 0 {
		return fallback(ref.base, "the theme has no colors Flint can use")
	}
	c.theme = settings.ThemeCustom
	c.customTheme = &custom
	var skipped []Skipped
	for _, n := range notes {
		skipped = append(skipped, Skipped{Kind: KindTheme, Name: name, Reason: n})
	}
	value := strconv.Itoa(len(custom.Colors)) + " colors on the " + custom.Base + " palette"
	return []Item{{Kind: KindTheme, Name: name, Value: value, Target: "Appearance ▸ Custom"}}, skipped
}

// hasExtension reports whether an extension folder starts with prefix.
func hasExtension(dir, prefix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			return true
		}
	}
	return false
}

// String summarises a plan for logs.
func (p Plan) String() string {
	return fmt.Sprintf("import from %s: %d changes, %d skipped", p.Source.Name, len(p.Items), len(p.Skipped))
}
