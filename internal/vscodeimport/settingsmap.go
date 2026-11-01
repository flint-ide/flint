package vscodeimport

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"flint/internal/settings"
)

// changes are the Flint settings an import sets; nil and empty mean "keep".
type changes struct {
	editorFamily, terminalFamily *string
	editorSize, terminalSize     *int
	editorLine, terminalLine     *float64
	autoSave, vimMode            *bool
	theme                        string
	customTheme                  *settings.CustomTheme
	keymap                       map[string]string
}

// apply returns s with the changes; the keymap is merged into the user's.
func (c changes) apply(s settings.Settings) settings.Settings {
	out := s.Clone()
	set(&out.Editor.FontFamily, c.editorFamily)
	set(&out.Editor.FontSize, c.editorSize)
	set(&out.Editor.LineHeight, c.editorLine)
	set(&out.Terminal.FontFamily, c.terminalFamily)
	set(&out.Terminal.FontSize, c.terminalSize)
	set(&out.Terminal.LineHeight, c.terminalLine)
	set(&out.AutoSave, c.autoSave)
	set(&out.Editor.VimMode, c.vimMode)
	if c.customTheme != nil {
		out.CustomTheme = *c.customTheme
	}
	if c.theme != "" {
		out.Theme = c.theme
	}
	for id, keys := range c.keymap {
		out.Keymap[id] = keys
	}
	return out
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// handledElsewhere are settings read by the theme step.
var handledElsewhere = []string{
	"workbench.colorTheme", "window.autoDetectColorScheme",
	"workbench.preferredDarkColorTheme", "workbench.preferredLightColorTheme",
}

// notImported explains common VS Code settings Flint has no equivalent for.
var notImported = map[string]string{
	"editor.tabSize":            "Flint detects indentation from each file",
	"editor.insertSpaces":       "Flint detects indentation from each file",
	"editor.detectIndentation":  "Flint detects indentation from each file",
	"editor.wordWrap":           "Flint has no word wrap setting",
	"editor.formatOnSave":       "Flint has no format-on-save setting",
	"editor.formatOnPaste":      "Flint has no format-on-paste setting",
	"editor.defaultFormatter":   "Flint has no formatter setting",
	"files.exclude":             "Flint has no exclude setting for the file tree; it hides what .gitignore ignores",
	"search.exclude":            "Flint has no search exclude setting; search skips what .gitignore ignores",
	"files.watcherExclude":      "Flint has no watcher exclude setting",
	"editor.minimap.enabled":    "Flint has no minimap",
	"workbench.iconTheme":       "Flint has no icon themes",
	"terminal.integrated.shell": "Flint starts your login shell",
}

// mapSettings maps VS Code's settings.json onto Flint settings. current gives
// the font size a pixel line height is relative to when the import sets none.
func mapSettings(raw map[string]json.RawMessage, current settings.Settings) (changes, []Item, []Skipped) {
	var c changes
	var items []Item
	var skipped []Skipped
	add := func(name, value, target string) {
		items = append(items, Item{Kind: KindSetting, Name: name, Value: value, Target: target})
	}
	skip := func(name, reason string) {
		skipped = append(skipped, Skipped{Kind: KindSetting, Name: name, Reason: reason})
	}
	family := func(key, target string, dst **string) {
		var v string
		if !decode(raw, key, &v, skip) {
			return
		}
		if err := settings.CheckFontFamily(v); err != nil {
			skip(key, err.Error())
			return
		}
		v = strings.TrimSpace(v)
		*dst = &v
		add(key, v, target)
	}
	size := func(key, target string, dst **int) {
		var v float64
		if !decode(raw, key, &v, skip) {
			return
		}
		n := int(math.Round(v))
		if n < settings.MinFontSize || n > settings.MaxFontSize {
			skip(key, fmt.Sprintf("%g is outside Flint's range %d–%d", v, settings.MinFontSize, settings.MaxFontSize))
			return
		}
		*dst = &n
		add(key, strconv.Itoa(n)+" px", target)
	}
	lineHeight := func(key, target string, pixelsOf int, dst **float64) {
		var v float64
		if !decode(raw, key, &v, skip) {
			return
		}
		h := v
		if v == 0 {
			skip(key, "0 means automatic in VS Code; Flint keeps its own")
			return
		}
		if v >= 8 { // VS Code: pixels from 8 up, a multiple of the font size below
			h = v / float64(pixelsOf)
		}
		h = math.Round(h*100) / 100
		if h < settings.MinLineHeight || h > settings.MaxLineHeight {
			skip(key, fmt.Sprintf("%g is outside Flint's range %g–%g", h, settings.MinLineHeight, settings.MaxLineHeight))
			return
		}
		*dst = &h
		add(key, strconv.FormatFloat(h, 'f', -1, 64)+"×", target)
	}

	family("editor.fontFamily", "Editor ▸ Font family", &c.editorFamily)
	size("editor.fontSize", "Editor ▸ Font size", &c.editorSize)
	editorPx := current.Editor.FontSize
	if c.editorSize != nil {
		editorPx = *c.editorSize
	}
	lineHeight("editor.lineHeight", "Editor ▸ Line height", editorPx, &c.editorLine)
	family("terminal.integrated.fontFamily", "Terminal ▸ Font family", &c.terminalFamily)
	size("terminal.integrated.fontSize", "Terminal ▸ Font size", &c.terminalSize)
	terminalPx := current.Terminal.FontSize
	if c.terminalSize != nil {
		terminalPx = *c.terminalSize
	}
	lineHeight("terminal.integrated.lineHeight", "Terminal ▸ Line height", terminalPx, &c.terminalLine)

	var autoSave string
	if decode(raw, "files.autoSave", &autoSave, skip) {
		switch autoSave {
		case "onFocusChange", "onWindowChange":
			on := true
			c.autoSave = &on
			add("files.autoSave", "on", "Editor ▸ Save files when Flint loses focus")
		case "off":
			off := false
			c.autoSave = &off
			add("files.autoSave", "off", "Editor ▸ Save files when Flint loses focus")
		default:
			skip("files.autoSave", fmt.Sprintf("%q: Flint saves only when its window loses focus", autoSave))
		}
	}

	known := []string{
		"editor.fontFamily", "editor.fontSize", "editor.lineHeight",
		"terminal.integrated.fontFamily", "terminal.integrated.fontSize", "terminal.integrated.lineHeight", "files.autoSave",
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		switch {
		case slices.Contains(known, k) || slices.Contains(handledElsewhere, k):
		case notImported[k] != "":
			skip(k, notImported[k])
		case strings.HasPrefix(k, "["):
			skip(k, "language-specific settings are not imported")
		case strings.HasPrefix(k, "vim."):
			skip(k, "Vim extension options are not imported; Flint's Vim mode is on or off")
		default:
			skip(k, "no equivalent in Flint")
		}
	}
	return c, items, skipped
}

// decode reads raw[key] into v; false when the key is absent or of the wrong type
// (reported as skipped).
func decode(raw map[string]json.RawMessage, key string, v any, skip func(name, reason string)) bool {
	data, ok := raw[key]
	if !ok {
		return false
	}
	if err := json.Unmarshal(data, v); err != nil {
		skip(key, "unexpected value "+truncate(string(data), 40))
		return false
	}
	return true
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
