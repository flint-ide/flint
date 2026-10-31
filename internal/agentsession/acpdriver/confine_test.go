package acpdriver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"flint/internal/acp"
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
	for _, hook := range []string{".git/hooks/pre-commit", ".GIT/hooks/pre-commit", ".git/config"} {
		path := filepath.Join(cwd, hook)
		if err := h.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionID: "s1", Path: path, Content: "#!/bin/sh"}); err == nil {
			t.Errorf("write to %s must be refused", hook)
		}
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Errorf("hook was written: %v", err)
	}
}
