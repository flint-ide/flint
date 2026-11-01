//go:build darwin

package watcher

import "testing"

func TestRemap(t *testing.T) {
	mapping := map[string]string{"/private/var/x": "/var/x", "/Users/me/repo/.git": "/Users/me/repo/.git"}
	tests := map[string]string{
		"private/var/x/a.go":       "/var/x/a.go",
		"/private/var/x":           "/var/x",
		"/private/var/xy/a.go":     "/private/var/xy/a.go", // prefix of another dir, not a child
		"/Users/me/repo/.git/HEAD": "/Users/me/repo/.git/HEAD",
		"/elsewhere/file.txt":      "/elsewhere/file.txt",
	}
	for in, want := range tests {
		if got := remap(in, mapping); got != want {
			t.Errorf("remap(%q) = %q, want %q", in, got, want)
		}
	}
}
