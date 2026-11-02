package vscodeimport

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/settings"
)

func fixtureExtensions() []string {
	return []string{filepath.Join("testdata", "home", ".vscode", "extensions")}
}

func TestFindThemeByLocalizedLabel(t *testing.T) {
	ref, err := findTheme(fixtureExtensions(), "Acme Night")
	if err != nil {
		t.Fatalf("findTheme() error = %v", err)
	}
	if filepath.Base(ref.path) != "night-color-theme.json" || ref.base != settings.ThemeDark {
		t.Errorf("findTheme() = %+v", ref)
	}
	day, err := findTheme(fixtureExtensions(), "Acme Day")
	if err != nil || day.base != settings.ThemeLight {
		t.Errorf("Acme Day = %+v, %v; want a light theme", day, err)
	}
}

func TestFindThemeMissing(t *testing.T) {
	_, err := findTheme(fixtureExtensions(), "Default Dark Modern")
	if err == nil || !strings.Contains(err.Error(), "built into") {
		t.Errorf("findTheme(built-in) error = %v", err)
	}
	if _, err := findTheme([]string{filepath.Join(t.TempDir(), "none")}, "Acme Night"); err == nil {
		t.Error("findTheme in a missing directory succeeded")
	}
}

func TestLoadThemeRefusesTmThemeAndEscapes(t *testing.T) {
	for _, name := range []string{"Acme Day", "Acme Escape"} {
		ref, err := findTheme(fixtureExtensions(), name)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadTheme(ref.path, ref.root, 0); err == nil {
			t.Errorf("loadTheme(%s) succeeded", name)
		}
	}
}

func TestLoadThemeIncludeCycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.json"), `{"include":"./b.json"}`)
	writeFile(t, filepath.Join(dir, "b.json"), `{"include":"./a.json"}`)
	if _, _, err := loadTheme(filepath.Join(dir, "a.json"), dir, 0); err == nil {
		t.Error("an include cycle was accepted")
	}
}

func TestLoadThemeTokenColorsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "t.json"), `{"tokenColors":"./tokens.json"}`)
	writeFile(t, filepath.Join(dir, "tokens.json"), `{"tokenColors":[{"scope":"comment","settings":{"foreground":"#123456"}}]}`)
	th, skipped, err := loadTheme(filepath.Join(dir, "t.json"), dir, 0)
	if err != nil || len(skipped) > 0 {
		t.Fatalf("loadTheme() = %v, %v", skipped, err)
	}
	if got := tokenColor(th.rules, "comment.line"); got != "#123456" {
		t.Errorf("comment = %q", got)
	}
	writeFile(t, filepath.Join(dir, "t.json"), `{"tokenColors":"./tokens.tmTheme"}`)
	if _, skipped, err := loadTheme(filepath.Join(dir, "t.json"), dir, 0); err != nil || len(skipped) != 1 {
		t.Errorf("tmTheme token colors: skipped %v, err %v; want one skipped note", skipped, err)
	}
}

func TestConvertTheme(t *testing.T) {
	ref, err := findTheme(fixtureExtensions(), "Acme Night")
	if err != nil {
		t.Fatal(err)
	}
	th, skipped, err := loadTheme(ref.path, ref.root, 0)
	if err != nil || len(skipped) > 0 {
		t.Fatalf("loadTheme() = %v, %v", skipped, err)
	}
	got := convertTheme(th, ref.label, ref.base)
	want := map[string]string{
		"--bg-editor":      "#1b1d23", // own color overrides the included one
		"--fg":             "#c8ccd4",
		"--bg":             "#16181d", // falls back to the sidebar
		"--bg-sidebar":     "#16181d",
		"--danger":         "#f44747", // from the included theme
		"--fg-faint":       "#495162",
		"--accent":         "#e5a50a",
		"--accent-dim":     "#e5a50a47",
		"--accent-fg":      "#101010",
		"--active-line":    "#2c313a66",
		"--syn-comment":    "#5c6370", // later rule wins a tie
		"--syn-string":     "#98c379",
		"--syn-keyword":    "#e06c75", // keyword.control beats keyword
		"--syn-function":   "#61afef",
		"--syn-type":       "#e5c07b",
		"--syn-definition": "#c8ccd4", // the text color
		"--syn-punct":      "#c678dd", // keyword.operator gets the keyword color, as in VS Code
	}
	for k, v := range want {
		if got.Colors[k] != v {
			t.Errorf("%s = %q, want %q", k, got.Colors[k], v)
		}
	}
	for _, k := range []string{"--selection", "--syn-number", "--syn-property"} {
		if c, ok := got.Colors[k]; ok {
			t.Errorf("%s = %q, want unset (invalid or missing)", k, c)
		}
	}
	if got.Name != "Acme Night" || got.Base != settings.ThemeDark {
		t.Errorf("name/base = %q/%q", got.Name, got.Base)
	}
	s := settings.Default()
	s.Theme, s.CustomTheme = settings.ThemeCustom, got
	if err := settings.Validate(s); err != nil {
		t.Errorf("converted theme fails validation: %v", err)
	}
}

func TestConvertThemeTerminalColors(t *testing.T) {
	th := vsTheme{colors: map[string]string{
		"terminal.ansiRed":          "#bf616a",
		"terminal.ansiBrightBlue":   "#81a1c1",
		"terminal.background":       "#2e3440",
		"terminalCursor.foreground": "#d8dee9",
		"terminal.ansiGreen":        "rgba(0,255,0,1)", // not hex: left out
	}}
	got := convertTheme(th, "Nord", settings.ThemeDark)
	want := map[string]string{"red": "#bf616a", "brightBlue": "#81a1c1", "background": "#2e3440", "cursor": "#d8dee9"}
	if !maps.Equal(got.Terminal, want) {
		t.Errorf("Terminal = %v, want %v", got.Terminal, want)
	}
	if err := settings.ValidateCustomTheme(got); err != nil {
		t.Errorf("converted theme fails validation: %v", err)
	}
	if convertTheme(vsTheme{colors: map[string]string{}}, "Plain", settings.ThemeDark).Terminal != nil {
		t.Error("a theme without terminal colors got a terminal map")
	}
}

func TestTokenColorSelectors(t *testing.T) {
	var rules []tokenRule
	raw := `[
		{"scope":"support.type.property-name","settings":{"foreground":"#010101"}},
		{"scope":["meta.x","variable.other"],"settings":{"foreground":"#020202"}},
		{"scope":"variable.other.property > x","settings":{"foreground":"#030303"}}
	]`
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		t.Fatal(err)
	}
	if got := tokenColor(rules, "support.type.property-name.json"); got != "#010101" {
		t.Errorf("property-name = %q", got)
	}
	if got := tokenColor(rules, "variable.other.property"); got != "#020202" {
		t.Errorf("variable.other.property = %q", got)
	}
	if got := tokenColor(rules, "variable"); got != "" {
		t.Errorf("a less specific token matched a more specific selector: %q", got)
	}
}

func TestWithAlpha(t *testing.T) {
	for in, want := range map[string]string{"#abc": "#aabbcc66", "#112233": "#11223366", "#11223344": "#11223344"} {
		if got := withAlpha(in, "66"); got != want {
			t.Errorf("withAlpha(%s) = %s, want %s", in, got, want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
