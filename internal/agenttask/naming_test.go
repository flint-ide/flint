package agenttask

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Add rate limiting to the public API", "add-rate-limiting-to-the-public-api"},
		{"  Fix: crash when   saving!!  ", "fix-crash-when-saving"},
		{"Zażółć gęślą jaźń", "zazolc-gesla-jazn"},
		{"Straße über Café", "strasse-uber-cafe"},
		{"日本語だけ", "task"},
		{"", "task"},
		{"---", "task"},
		{"refactor the session restore code so that dormant chats wake up faster", "refactor-the-session-restore-code-so"},
		{"averyveryveryveryveryveryveryveryveryverylongword", "averyveryveryveryveryveryveryveryveryver"},
		{"first line\nsecond line is ignored", "first-line"},
		{"..lock ref.lock", "lock-ref-lock"},
	}
	for _, tt := range tests {
		got := Slug(tt.in)
		if got != tt.want {
			t.Errorf("Slug(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if len(got) > maxSlug {
			t.Errorf("Slug(%q) longer than %d", tt.in, maxSlug)
		}
	}
}

func TestTitle(t *testing.T) {
	if got := Title("\n\n  Add   a   button \nmore"); got != "Add a button" {
		t.Errorf("Title = %q", got)
	}
	long := strings.Repeat("ab ", 60)
	if got := Title(long); len([]rune(got)) != maxTitle || !strings.HasSuffix(got, "…") {
		t.Errorf("Title(long) = %q", got)
	}
	if Title("   ") != "Agent task" {
		t.Error("empty title")
	}
}

func TestUnique(t *testing.T) {
	taken := map[string]bool{"agent/x": true, "agent/x-2": true}
	got, err := Unique("agent/x", func(s string) bool { return taken[s] })
	if err != nil || got != "agent/x-3" {
		t.Errorf("Unique = %q, %v", got, err)
	}
	if got, _ := Unique("agent/y", func(string) bool { return false }); got != "agent/y" {
		t.Errorf("free name changed: %q", got)
	}
	if _, err := Unique("a", func(string) bool { return true }); err == nil {
		t.Error("no error when every name is taken")
	}
}

func TestWorktreePath(t *testing.T) {
	main := "/src/flint"
	if got := WorktreePath("", "/home/me", main, "agent/fix-x"); got != "/src/flint.worktrees/agent-fix-x" {
		t.Errorf("default = %q", got)
	}
	if got := WorktreePath("~/worktrees", "/home/me", main, "agent/fix-x"); got != filepath.FromSlash("/home/me/worktrees/flint/agent-fix-x") {
		t.Errorf("home = %q", got)
	}
	if got := WorktreePath("/tmp/wt/", "/home/me", main, "b"); got != "/tmp/wt/flint/b" {
		t.Errorf("absolute = %q", got)
	}
}
