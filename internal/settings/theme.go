package settings

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ThemeCustom uses CustomTheme: its colors laid over the dark or light palette.
const ThemeCustom = "custom"

// MaxThemeNameLen is the longest custom theme name, in bytes.
const MaxThemeNameLen = 100

// CustomTheme is a user palette, for example one imported from VS Code: CSS
// variables of style.css overridden on top of a built-in palette.
type CustomTheme struct {
	// Name is shown in Settings ▸ Appearance.
	Name string `json:"name"`
	// Base is the palette ("dark" or "light") the colors are laid over; it also
	// picks the native window appearance and the terminal's ANSI colors.
	Base string `json:"base"`
	// Colors maps a CSS variable ("--bg-editor") to a hex color ("#1e1e1e").
	Colors map[string]string `json:"colors"`
}

// ThemeVariables are the CSS variables a custom theme may set (style.css).
// Anything else is refused: the values end up in the page's style.
var ThemeVariables = []string{
	"--bg", "--bg-sidebar", "--bg-editor", "--bg-raised", "--bg-hover", "--bg-active", "--border",
	"--fg", "--fg-muted", "--fg-faint", "--fg-name",
	"--accent", "--accent-dim", "--accent-fg", "--danger", "--warn", "--info", "--ok",
	"--selection", "--selection-match", "--search-match", "--active-line",
	"--git-added", "--git-modified", "--git-deleted", "--diff-ins-bg", "--diff-del-bg",
	"--syn-keyword", "--syn-string", "--syn-number", "--syn-comment", "--syn-function",
	"--syn-type", "--syn-property", "--syn-definition", "--syn-punct",
}

// hexColor matches #rgb, #rgba, #rrggbb and #rrggbbaa.
var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// ValidColor reports whether c is a hex color a custom theme may use.
func ValidColor(c string) bool { return hexColor.MatchString(c) }

func defaultCustomTheme() CustomTheme {
	return CustomTheme{Base: ThemeDark, Colors: map[string]string{}}
}

// Appearance is the theme mode the window follows: "dark", "light" or "system";
// a custom theme follows its base palette.
func Appearance(s Settings) string {
	if s.Theme == ThemeCustom {
		return s.CustomTheme.Base
	}
	return s.Theme
}

func validateCustomTheme(c CustomTheme) error {
	if err := validThemeName(c.Name); err != nil {
		return err
	}
	if c.Base != ThemeDark && c.Base != ThemeLight {
		return fmt.Errorf("custom theme base %q must be dark or light", c.Base)
	}
	for name, color := range c.Colors {
		if err := validThemeColor(name, color); err != nil {
			return err
		}
	}
	return nil
}

func validThemeName(name string) error {
	if len(name) > MaxThemeNameLen || strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("invalid custom theme name %q", name)
	}
	return nil
}

func validThemeColor(name, color string) error {
	if !slices.Contains(ThemeVariables, name) {
		return fmt.Errorf("custom theme: unknown color %q", name)
	}
	if !ValidColor(color) {
		return fmt.Errorf("custom theme: %s must be a hex color, not %q", name, color)
	}
	return nil
}

func normalizeCustomTheme(c CustomTheme) CustomTheme {
	out := CustomTheme{Name: c.Name, Base: c.Base, Colors: map[string]string{}}
	if validThemeName(c.Name) != nil {
		out.Name = ""
	}
	if c.Base != ThemeDark && c.Base != ThemeLight {
		out.Base = ThemeDark
	}
	for name, color := range c.Colors {
		if validThemeColor(name, color) == nil {
			out.Colors[name] = color
		}
	}
	return out
}

func (c CustomTheme) clone() CustomTheme {
	out := c
	out.Colors = maps.Clone(c.Colors)
	if out.Colors == nil {
		out.Colors = map[string]string{}
	}
	return out
}
