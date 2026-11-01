package vscodeimport

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// namedKeys maps VS Code key names to Flint's spelling (lib/commands.ts chordToKeys).
var namedKeys = map[string]string{
	"enter": "Enter", "escape": "Escape", "backspace": "Backspace", "delete": "Delete",
	"tab": "Tab", "space": "Space", "home": "Home", "end": "End",
	"pageup": "PageUp", "pagedown": "PageDown",
	"up": "ArrowUp", "down": "ArrowDown", "left": "ArrowLeft", "right": "ArrowRight",
}

// shifted is what ⇧ types on a US layout: Flint names a shortcut by the
// character the key produces (⇧⌘[ arrives as '{').
var shifted = map[string]string{
	"`": "~", "-": "_", "=": "+", "[": "{", "]": "}", `\`: "|", ";": ":", "'": `"`, ",": "<", ".": ">", "/": "?",
}

var (
	fnKey    = regexp.MustCompile(`^f([1-9]|1[0-9])$`)
	scanCode = regexp.MustCompile(`^\[(?:Key([A-Z])|Digit([0-9]))\]$`)
)

// ConvertKey turns a VS Code key binding ("cmd+shift+p") into Flint's
// keymap notation ("Shift+Mod+P", modifiers in canonical order). Chords,
// keys Flint can't name and plain typing keys are refused with the reason.
func ConvertKey(vs string) (string, error) {
	vs = strings.TrimSpace(vs)
	if vs == "" {
		return "", errors.New("no key")
	}
	if strings.Contains(vs, " ") {
		return "", errors.New("multi-key chords are not supported")
	}
	var ctrl, alt, shift, mod bool
	parts := strings.Split(vs, "+")
	// "cmd+shift+=" ends in the key; "cmd++" isn't VS Code syntax.
	key := parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "ctrl":
			ctrl = true
		case "alt":
			alt = true
		case "shift":
			shift = true
		case "cmd", "meta", "win":
			mod = true
		default:
			return "", fmt.Errorf("unknown modifier %q", m)
		}
	}
	name, typing, err := keyName(key)
	if err != nil {
		return "", err
	}
	if typing && !ctrl && !alt && !mod {
		return "", errors.New("a typing key without ⌘, ⌃ or ⌥ would stop that key from typing")
	}
	if typing && alt && len(name) == 1 && !isAlnum(name) {
		return "", errors.New("⌥ with punctuation types a different character on macOS")
	}
	if shift && typing {
		if s, ok := shifted[name]; ok {
			name = s
		}
	}
	mods := []string{}
	for _, m := range []struct {
		on   bool
		name string
	}{{ctrl, "Ctrl"}, {alt, "Alt"}, {shift, "Shift"}, {mod, "Mod"}} {
		if m.on {
			mods = append(mods, m.name)
		}
	}
	return strings.Join(append(mods, name), "+"), nil
}

// keyName returns Flint's name for a VS Code key and whether it types text.
func keyName(key string) (string, bool, error) {
	lower := strings.ToLower(key)
	switch {
	case key == "":
		return "", false, errors.New("no key after the modifiers")
	case namedKeys[lower] != "":
		typing := lower == "space" || lower == "enter" || lower == "backspace" || lower == "tab"
		return namedKeys[lower], typing, nil
	case fnKey.MatchString(lower):
		return strings.ToUpper(lower), false, nil
	case len(key) == 1 && (isAlnum(key) || shifted[key] != ""):
		return strings.ToUpper(key), true, nil
	}
	if m := scanCode.FindStringSubmatch(key); m != nil {
		return m[1] + m[2], true, nil
	}
	switch lower {
	case "shift", "ctrl", "alt", "cmd", "meta":
		return "", false, errors.New("a modifier alone is not a shortcut")
	}
	return "", false, fmt.Errorf("key %q has no Flint equivalent", key)
}

func isAlnum(s string) bool {
	c := s[0]
	return len(s) == 1 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9')
}
