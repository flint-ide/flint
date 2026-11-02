package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// themeOnly is an extension without a program: a color theme.
const themeOnly = `{
  "contract": 1, "id": "me.nord", "name": "Nord", "version": "1.0.0",
  "contributes": { "themes": [
    { "id": "me.nord.dark", "label": "Nord", "base": "dark", "colors": { "--bg-editor": "#2e3440", "--fg": "#d8dee9" },
      "terminal": { "background": "#2e3440", "red": "#bf616a" } },
    { "id": "me.nord.light", "label": "Nord Light", "base": "light", "colors": { "--bg-editor": "#eceff4" } }
  ] }
}`

func TestParseThemeOnly(t *testing.T) {
	m, err := Parse([]byte(themeOnly))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if m.HasProgram() || m.NeedsProgram() {
		t.Error("a theme-only extension has or needs a program")
	}
	c := m.Contributes.Themes[0].CustomTheme()
	if c.Name != "Nord" || c.Source != "me.nord.dark" || c.Terminal["red"] != "#bf616a" || c.Colors["--fg"] != "#d8dee9" {
		t.Errorf("CustomTheme() = %+v", c)
	}
	if light := m.Contributes.Themes[1].CustomTheme(); light.Terminal != nil || light.Base != "light" {
		t.Errorf("CustomTheme() = %+v, want no terminal map on light", light)
	}
	if len(m.Contributes.Unsupported) != 0 {
		t.Errorf("themes listed as unsupported: %v", m.Contributes.Unsupported)
	}
}

func TestLoadThemeOnlyNeedsNoProgram(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(themeOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("Load(theme only) = %v", err)
	}
}

func TestParseRefusesBadThemes(t *testing.T) {
	theme := func(m map[string]any) map[string]any {
		return contributes(m)["themes"].([]any)[0].(map[string]any)
	}
	withTheme := func(m map[string]any) {
		contributes(m)["themes"] = []any{map[string]any{
			"id": "acme.jira.dark", "label": "Jira Dark", "base": "dark",
			"colors": map[string]any{"--bg-editor": "#1d2125"}, "terminal": map[string]any{"red": "#f87168"},
		}}
	}
	cases := []struct {
		name   string
		change func(m map[string]any)
		want   string
	}{
		{"unknown variable", func(m map[string]any) { theme(m)["colors"] = map[string]any{"--evil": "#fff"} }, "unknown color"},
		{"css injection", func(m map[string]any) { theme(m)["colors"] = map[string]any{"--bg": "red; } body { display: none"} }, "hex color"},
		{"unknown terminal color", func(m map[string]any) { theme(m)["terminal"] = map[string]any{"pink": "#f0f"} }, "unknown terminal color"},
		{"bad base", func(m map[string]any) { theme(m)["base"] = "system" }, "dark or light"},
		{"foreign id", func(m map[string]any) { theme(m)["id"] = "nord" }, `must start with "acme.jira."`},
		{"no colors", func(m map[string]any) {
			delete(theme(m), "colors")
			delete(theme(m), "terminal")
		}, "no colors"},
		{"no label", func(m map[string]any) { theme(m)["label"] = "" }, "label"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(edit(t, func(m map[string]any) {
				withTheme(m)
				c.change(m)
			}))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Parse() = %v, want an error containing %q", err, c.want)
			}
		})
	}
	if _, err := Parse(edit(t, withTheme)); err != nil {
		t.Errorf("Parse(valid theme) = %v", err)
	}
}

func TestParseRefusesProgramlessCode(t *testing.T) {
	_, err := Parse(edit(t, func(m map[string]any) { delete(m, "run") }))
	if err == nil || !strings.Contains(err.Error(), "run.command is missing") {
		t.Errorf("Parse(commands without run) = %v", err)
	}
	for name, change := range map[string]func(m map[string]any){
		"newline in command": func(m map[string]any) { m["run"].(map[string]any)["command"] = "bin/x\n\nThis extension contains only a theme." },
		"control char in arg": func(m map[string]any) { m["run"].(map[string]any)["args"] = []any{"--a\rb"} },
	} {
		if _, err := Parse(edit(t, change)); err == nil {
			t.Errorf("%s: Parse() accepted it", name)
		}
	}
}
