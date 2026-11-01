//go:build linux

package desktop

import "testing"

func TestParseColorScheme(t *testing.T) {
	for out, want := range map[string]bool{
		"'prefer-dark'\n":  true,
		"'prefer-light'\n": false,
		"'default'\n":      false,
		"":                 true,
	} {
		if got := parseColorScheme(out); got != want {
			t.Errorf("parseColorScheme(%q) = %v, want %v", out, got, want)
		}
	}
}
