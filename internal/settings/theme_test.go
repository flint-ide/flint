package settings

import (
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

func TestThemeVariablesAreCSSVariables(t *testing.T) {
	for _, v := range ThemeVariables {
		if !strings.HasPrefix(v, "--") || strings.ContainsAny(v, " ;:{}") {
			t.Errorf("bad theme variable %q", v)
		}
	}
}
