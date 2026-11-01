package vscodeimport

import "testing"

func TestConvertKey(t *testing.T) {
	cases := map[string]string{
		"cmd+shift+p":         "Shift+Mod+P",
		"Cmd+P":               "Mod+P",
		"ctrl+`":              "Ctrl+`",
		"alt+cmd+b":           "Alt+Mod+B",
		"meta+k":              "Mod+K",
		"shift+alt+f":         "Alt+Shift+F",
		"f5":                  "F5",
		"shift+f8":            "Shift+F8",
		"cmd+f12":             "Mod+F12",
		"cmd+enter":           "Mod+Enter",
		"ctrl+escape":         "Ctrl+Escape",
		"cmd+up":              "Mod+ArrowUp",
		"alt+left":            "Alt+ArrowLeft",
		"cmd+pageup":          "Mod+PageUp",
		"cmd+backspace":       "Mod+Backspace",
		"ctrl+space":          "Ctrl+Space",
		"cmd+=":               "Mod+=",
		"cmd+shift+=":         "Shift+Mod++",
		"cmd+shift+[":         "Shift+Mod+{",
		"cmd+/":               "Mod+/",
		"cmd+[KeyJ]":          "Mod+J",
		"ctrl+shift+[Digit2]": "Ctrl+Shift+2",
		" cmd+j ":             "Mod+J",
	}
	for in, want := range cases {
		got, err := ConvertKey(in)
		if err != nil {
			t.Errorf("ConvertKey(%q) error = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ConvertKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertKeyRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"cmd+k cmd+s", // chord
		"a",           // would stop typing
		"shift+a",
		"cmd+numpad1",
		"hyper+k",
		"cmd+",
		"cmd+[Semicolon]",
		"alt+[",
		"cmd+shift",
	} {
		if got, err := ConvertKey(in); err == nil {
			t.Errorf("ConvertKey(%q) = %q, want an error", in, got)
		}
	}
}
