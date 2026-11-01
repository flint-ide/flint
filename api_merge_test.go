package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/git"
	"flint/internal/settings"
)

// conflictedRepo makes a repository at base/name with a merge conflict in f.txt.
func conflictedRepo(t *testing.T, base, name string) string {
	t.Helper()
	repo := filepath.Join(base, name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q", "-b", "main")
	gitT(t, repo, "config", "user.name", "T")
	gitT(t, repo, "config", "user.email", "t@example.com")
	gitT(t, repo, "config", "commit.gpgsign", "false")
	commitT(t, repo, "f.txt", "a\nb\nc\n", "base")
	gitT(t, repo, "switch", "-q", "-c", "feature")
	commitT(t, repo, "f.txt", "a\nfeature\nc\n", "feature")
	gitT(t, repo, "switch", "-q", "main")
	commitT(t, repo, "f.txt", "a\nmain\nc\n", "main")
	cmd := exec.Command("git", "merge", "feature")
	cmd.Dir = repo
	if err := cmd.Run(); err == nil {
		t.Fatal("merge should conflict")
	}
	return repo
}

func TestMarkResolvedOnlyInsideOpenFolders(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	open := conflictedRepo(t, base, "open")
	other := conflictedRepo(t, base, "other")
	a := &App{settings: settings.Default(), ctx: context.Background(), roots: []string{open}}
	g := &GitAPI{app: a}

	if err := g.MarkResolved(filepath.Join(other, "f.txt"), "x\n", "", false, true); err == nil {
		t.Fatal("a file outside the open folders must be refused")
	}
	if err := g.MarkResolved("f.txt", "x\n", "", false, true); err == nil {
		t.Fatal("a relative path must be refused")
	}
	if err := g.SaveMergeResult(filepath.Join(other, "f.txt"), "x\n", "", true); err == nil {
		t.Fatal("Save outside the open folders must be refused")
	}

	path := filepath.Join(open, "f.txt")
	c, err := g.Conflict(path)
	if err != nil || !git.HasConflictMarkers(c.Diff3) || c.Sides.Incoming.Label != "feature" {
		t.Fatalf("Conflict = %+v, %v", c, err)
	}
	if err := g.MarkResolved(path, c.Working.Content, c.Working.Content, false, false); err == nil || !strings.Contains(err.Error(), git.ErrConflictMarkers.Error()) {
		t.Fatalf("markers left: err = %v", err)
	}
	if err := g.SaveMergeResult(path, "a\nmain\nc\n", "stale", false); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("stale save: err = %v", err)
	}
	if err := g.MarkResolved(path, "a\nmain\nfeature\nc\n", c.Working.Content, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Conflict(path); !errors.Is(err, git.ErrNoConflict) {
		t.Fatalf("after resolving: err = %v, want ErrNoConflict", err)
	}
}

func TestRedactForAgent(t *testing.T) {
	g := &GitAPI{app: &App{}}
	got := g.RedactForAgent("/p/config.go", []string{`token := "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"`, "plain()"})
	if got.SecretFile || len(got.Texts) != 2 || strings.Contains(got.Texts[0], "ghp_0123") || got.Texts[1] != "plain()" {
		t.Fatalf("redacted = %+v", got)
	}
	if got := g.RedactForAgent("/p/.env", []string{"API_KEY=abc"}); !got.SecretFile || len(got.Texts) != 0 {
		t.Fatalf("a secret file passed its contents on: %+v", got)
	}
}
