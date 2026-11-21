package main

import "testing"

func TestRenameVars(t *testing.T) {
	tests := []struct{ in, want string }{
		{"--border: #e5e7eb;", "--wui-border: #e5e7eb;"},
		{"color: var(--border);", "color: var(--wui-border);"},
		{"--color-border: var(--border)", "--color-border: var(--wui-border)"},
		{"--border-control: var(--light-300);", "--border-control: var(--light-300);"},
		{"background: var(--accent)}", "background: var(--wui-accent)}"},
		{"--accent-foreground: #fff;", "--accent-foreground: #fff;"},
		{"inset 0 -1px 0 var(--border),0 0 var(--accent)", "inset 0 -1px 0 var(--wui-border),0 0 var(--wui-accent)"},
		{"--border", "--wui-border"},
	}
	for _, tt := range tests {
		if got := string(renameVars([]byte(tt.in), renames)); got != tt.want {
			t.Errorf("renameVars(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPlatformAsset(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
		ok                 bool
	}{
		{"darwin", "arm64", "macos-arm64", true},
		{"darwin", "amd64", "macos-x64", true},
		{"linux", "amd64", "linux-x64", true},
		{"linux", "arm64", "linux-arm64", true},
		{"windows", "amd64", "", false},
		{"linux", "386", "", false},
	}
	for _, tt := range tests {
		got, err := platformAsset(tt.goos, tt.goarch)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("platformAsset(%s, %s) = %q, %v", tt.goos, tt.goarch, got, err)
		}
	}
}
