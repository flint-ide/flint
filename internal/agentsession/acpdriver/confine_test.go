package acpdriver

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"flint/internal/acp"
	as "flint/internal/agentsession"
)

func TestConfine(t *testing.T) {
	cwd := t.TempDir()
	inside := filepath.Join(cwd, "a.go")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cwd, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := confine(cwd, inside); err != nil {
		t.Errorf("inside path rejected: %v", err)
	}
	for _, bad := range []string{"/etc/hosts", filepath.Join(cwd, "..", "x"), "rel/a.go", filepath.Join(cwd, "link", "x")} {
		if _, err := confine(cwd, bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestHandlerChecksSessionAndGitDir(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &handler{s: &session{id: "s1", cwd: cwd}}
	ctx := context.Background()
	file := filepath.Join(cwd, "a.txt")
	if err := h.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionID: "s1", Path: file, Content: "ok"}); err != nil {
		t.Errorf("write inside the project: %v", err)
	}
	if got, err := h.ReadTextFile(ctx, acp.ReadTextFileRequest{SessionID: "s1", Path: file}); err != nil || got != "ok" {
		t.Errorf("read = %q, %v", got, err)
	}
	if _, err := h.ReadTextFile(ctx, acp.ReadTextFileRequest{SessionID: "other", Path: file}); err == nil {
		t.Error("foreign session id must be rejected")
	}
	for _, hook := range []string{
		".git/hooks/pre-commit", ".GIT/hooks/pre-commit", ".git/config",
		"vendor/x/.git/config", // a nested repository's config (core.fsmonitor)
		"x/.git/HEAD",          // creating a repository
		"x/.Git",               // a .git file pointing at a git dir elsewhere
	} {
		path := filepath.Join(cwd, hook)
		if err := h.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionID: "s1", Path: path, Content: "#!/bin/sh"}); err == nil {
			t.Errorf("write to %s must be refused", hook)
		}
	}
	for _, p := range []string{".git/hooks/pre-commit", "vendor", "x"} {
		if _, err := os.Stat(filepath.Join(cwd, p)); !os.IsNotExist(err) {
			t.Errorf("%s was written: %v", p, err)
		}
	}
}

// A write into agent or editor configuration is shown to the user like a
// permission request and only happens when they allow it.
func TestProtectedWriteAsksTheUser(t *testing.T) {
	cwd := t.TempDir()
	var asked []as.Event
	var mu sync.Mutex
	var s *session
	answer := "reject"
	s = &session{id: "s1", cwd: cwd, pending: map[string]chan string{}, sink: func(e as.Event) {
		if e.Kind != as.KindPermission {
			return
		}
		mu.Lock()
		asked = append(asked, e)
		mu.Unlock()
		go func() {
			s.permMu.Lock()
			ch := s.pending[e.Permission.ID]
			s.permMu.Unlock()
			ch <- answer
		}()
	}}
	h := &handler{s: s}
	ctx := context.Background()
	for _, rel := range []string{".vscode/tasks.json", "sub/.Claude/settings.json"} {
		path := filepath.Join(cwd, rel)
		answer = "reject"
		if err := h.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionID: "s1", Path: path, Content: "{}"}); err == nil {
			t.Errorf("rejected write to %s succeeded", rel)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("rejected write to %s happened: %v", rel, err)
		}
		answer = "allow"
		if err := h.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionID: "s1", Path: path, Content: "{}"}); err != nil {
			t.Errorf("approved write to %s: %v", rel, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 4 {
		t.Fatalf("asked %d times, want 4", len(asked))
	}
	tc := asked[0].Permission.ToolCall
	if tc.Kind != as.ToolEdit || len(tc.Paths) != 1 || tc.Paths[0] != filepath.Join(cwd, ".vscode/tasks.json") || len(tc.Diffs) != 1 {
		t.Errorf("permission tool call = %+v", tc)
	}
	// A cancelled request (the turn ended) refuses the write.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	s.sink = func(as.Event) {}
	if err := h.WriteTextFile(cancelled, acp.WriteTextFileRequest{SessionID: "s1", Path: filepath.Join(cwd, ".idea/x.xml"), Content: "x"}); err == nil {
		t.Error("write without an answer succeeded")
	}
}
