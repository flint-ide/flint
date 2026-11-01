package shellenv

import "testing"

func TestDefaultShell(t *testing.T) {
	none := func(string) bool { return false }
	all := func(string) bool { return true }
	tests := []struct {
		goos   string
		exists func(string) bool
		want   string
	}{
		{"darwin", none, "/bin/zsh"},
		{"linux", all, "/bin/bash"},
		{"linux", none, "/bin/sh"},
	}
	for _, tt := range tests {
		if got := defaultShell(tt.goos, tt.exists); got != tt.want {
			t.Errorf("defaultShell(%q) = %q, want %q", tt.goos, got, tt.want)
		}
	}
}
