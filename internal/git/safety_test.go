package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A repository's own config must not run programs when Flint only reads it:
// core.fsmonitor names a command git would start on every status.
func TestRepoConfigCannotRunFSMonitor(t *testing.T) {
	r := newRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "monitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.Root, "config", "core.fsmonitor", hook)
	writeFile(t, r.Root, "README.md", "changed\n")

	if _, err := r.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("core.fsmonitor from the repository's config was executed")
	}
}
