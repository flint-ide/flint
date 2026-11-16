package settings

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func validCustom() CustomTheme {
	return CustomTheme{Name: "Monokai", Base: ThemeDark, Colors: map[string]string{"--bg-editor": "#272822", "--syn-keyword": "#F92672cc"}}
}

func TestCustomThemeSavesAndLoads(t *testing.T) {
	s := Default()
	s.Theme = ThemeCustom
	s.CustomTheme = validCustom()
	path := write(t, "{}")
	if _, err := Save(path, s); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != ThemeCustom || got.CustomTheme.Name != "Monokai" || got.CustomTheme.Colors["--syn-keyword"] != "#F92672cc" {
		t.Errorf("Load() = %+v", got.CustomTheme)
	}
}

func TestValidateCustomTheme(t *testing.T) {
	cases := map[string]func(*CustomTheme){
		"unknown variable":  func(c *CustomTheme) { c.Colors["--evil"] = "#fff" },
		"not a css var":     func(c *CustomTheme) { c.Colors["background"] = "#fff" },
		"css injection":     func(c *CustomTheme) { c.Colors["--bg"] = "red; } body { display:none" },
		"named color":       func(c *CustomTheme) { c.Colors["--bg"] = "red" },
		"bad hex length":    func(c *CustomTheme) { c.Colors["--bg"] = "#12345" },
		"bad base":          func(c *CustomTheme) { c.Base = "system" },
		"control char name": func(c *CustomTheme) { c.Name = "a\nb" },
		"long name":         func(c *CustomTheme) { c.Name = strings.Repeat("x", 101) },
		"unknown terminal":  func(c *CustomTheme) { c.Terminal = map[string]string{"orange": "#f80"} },
		"terminal css var":  func(c *CustomTheme) { c.Terminal = map[string]string{"--bg": "#000"} },
		"terminal not hex":  func(c *CustomTheme) { c.Terminal = map[string]string{"red": "rgb(255,0,0)"} },
		"control source":    func(c *CustomTheme) { c.Source = "a\x00b" },
		"long source":       func(c *CustomTheme) { c.Source = strings.Repeat("x", MaxThemeSourceLen+1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := Default()
			c := validCustom()
			mutate(&c)
			s.CustomTheme = c
			if err := Validate(s); err == nil {
				t.Errorf("Validate() accepted %+v", c)
			}
		})
	}
	s := Default()
	s.Theme = ThemeCustom
	s.CustomTheme = validCustom()
	if err := Validate(s); err != nil {
		t.Errorf("Validate(valid) = %v", err)
	}
}

func TestLoadDropsInvalidCustomColors(t *testing.T) {
	path := write(t, `{"theme":"custom","customTheme":{"name":"X","base":"neon","colors":{"--bg":"#111","--evil":"#222","--fg":"url(x)"}}}`)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != ThemeCustom || got.CustomTheme.Base != ThemeDark {
		t.Errorf("Theme = %q base %q, want custom on dark", got.Theme, got.CustomTheme.Base)
	}
	if len(got.CustomTheme.Colors) != 1 || got.CustomTheme.Colors["--bg"] != "#111" {
		t.Errorf("Colors = %v, want only --bg", got.CustomTheme.Colors)
	}
	if err := Validate(got); err != nil {
		t.Errorf("normalized settings fail Validate: %v", err)
	}
}

func TestTerminalColorsSaveAndLoad(t *testing.T) {
	s := Default()
	s.Theme = ThemeCustom
	s.CustomTheme = validCustom()
	s.CustomTheme.Terminal = map[string]string{"red": "#bf616a", "brightBlue": "#81a1c1", "background": "#2e3440"}
	s.CustomTheme.Source = "acme.nord.dark"
	path := write(t, "{}")
	if _, err := Save(path, s); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomTheme.Terminal["red"] != "#bf616a" || len(got.CustomTheme.Terminal) != 3 || got.CustomTheme.Source != "acme.nord.dark" {
		t.Errorf("Load() = %+v", got.CustomTheme)
	}
}

func TestLoadDropsInvalidTerminalColors(t *testing.T) {
	path := write(t, `{"theme":"custom","customTheme":{"name":"X","base":"dark","colors":{},"terminal":{"red":"#f00","pink":"#f0f","blue":"blue"},"source":"x\u0000y"}}`)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CustomTheme.Terminal) != 1 || got.CustomTheme.Terminal["red"] != "#f00" {
		t.Errorf("Terminal = %v, want only red", got.CustomTheme.Terminal)
	}
	if got.CustomTheme.Source != "" {
		t.Errorf("Source = %q, want it dropped", got.CustomTheme.Source)
	}
	if err := Validate(got); err != nil {
		t.Errorf("normalized settings fail Validate: %v", err)
	}
}

func TestCloneCopiesTerminalColors(t *testing.T) {
	s := Default()
	s.CustomTheme = validCustom()
	s.CustomTheme.Terminal = map[string]string{"red": "#f00"}
	c := s.Clone()
	c.CustomTheme.Terminal["red"] = "#000"
	if s.CustomTheme.Terminal["red"] != "#f00" {
		t.Error("Clone shares the terminal map")
	}
}

// The frontend's terminal palette has exactly these colors (lib/terminalTheme.ts).
func TestTerminalColorsMatchFrontend(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "terminalTheme.ts"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(src), "TERMINAL_COLOR_NAMES")
	end := strings.Index(string(src[start:]), "])")
	if start < 0 || end < 0 {
		t.Fatal("TERMINAL_COLOR_NAMES not found in lib/terminalTheme.ts")
	}
	got := regexp.MustCompile(`'([A-Za-z]+)'`).FindAllStringSubmatch(string(src[start:start+end]), -1)
	var names []string
	for _, m := range got {
		names = append(names, m[1])
	}
	if !slices.Equal(names, TerminalColors) {
		t.Errorf("frontend TERMINAL_COLOR_NAMES = %v\nGo TerminalColors = %v", names, TerminalColors)
	}
}

func TestAppearance(t *testing.T) {
	s := Default()
	for theme, want := range map[string]string{ThemeDark: ThemeDark, ThemeLight: ThemeLight, ThemeSystem: ThemeSystem} {
		s.Theme = theme
		if got := Appearance(s); got != want {
			t.Errorf("Appearance(%s) = %q, want %q", theme, got, want)
		}
	}
	s.Theme = ThemeCustom
	s.CustomTheme = CustomTheme{Base: ThemeLight}
	if got := Appearance(s); got != ThemeLight {
		t.Errorf("Appearance(custom light) = %q", got)
	}
}

func TestCloneCopiesCustomColors(t *testing.T) {
	s := Default()
	s.CustomTheme = validCustom()
	c := s.Clone()
	c.CustomTheme.Colors["--bg"] = "#000"
	if _, ok := s.CustomTheme.Colors["--bg"]; ok {
		t.Error("Clone shares the colors map")
	}
}

func TestVimModeRoundtrip(t *testing.T) {
	path := write(t, `{"editor":{"fontSize":14,"vimMode":true}}`)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Editor.VimMode || got.Editor.FontSize != 14 {
		t.Errorf("Editor = %+v, want vim on, size 14", got.Editor)
	}
	if Default().Editor.VimMode {
		t.Error("vim mode is on by default")
	}
}

func TestSaveActionsRoundtrip(t *testing.T) {
	path := write(t, `{"editor":{"formatOnSave":true,"organizeImportsOnSave":true}}`)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Editor.FormatOnSave || !got.Editor.OrganizeImportsOnSave {
		t.Errorf("Editor = %+v, want both save actions on", got.Editor)
	}
	if d := Default().Editor; d.FormatOnSave || d.OrganizeImportsOnSave {
		t.Error("save actions are on by default")
	}
}

// The frontend filters cached settings with the same allow-list (lib/settings.ts).
func TestThemeVariablesMatchFrontend(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "settings.ts"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(src), "THEME_VARIABLES")
	end := strings.Index(string(src[start:]), "])")
	if start < 0 || end < 0 {
		t.Fatal("THEME_VARIABLES not found in lib/settings.ts")
	}
	got := regexp.MustCompile(`'(--[a-z0-9-]+)'`).FindAllStringSubmatch(string(src[start:start+end]), -1)
	var names []string
	for _, m := range got {
		names = append(names, m[1])
	}
	if !slices.Equal(names, ThemeVariables) {
		t.Errorf("frontend THEME_VARIABLES = %v\nGo ThemeVariables = %v", names, ThemeVariables)
	}
}

func TestThemeVariablesAreCSSVariables(t *testing.T) {
	for _, v := range ThemeVariables {
		if !strings.HasPrefix(v, "--") || strings.ContainsAny(v, " ;:{}") {
			t.Errorf("bad theme variable %q", v)
		}
	}
}
