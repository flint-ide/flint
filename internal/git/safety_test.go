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

// Filters and textconv drivers defined by the repository itself (a cloned
// archive ships its .git/config and .gitattributes) must not run on status,
// blame or diff.
func TestRepoConfigCannotRunFiltersOrTextconv(t *testing.T) {
	r := newRepo(t)
	dir := t.TempDir()
	marker := func(name string) string { return filepath.Join(dir, name) }
	script := func(name string) string {
		p := filepath.Join(dir, name+".sh")
		body := "#!/bin/sh\ntouch " + marker(name) + "\ncat\n"
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mustGit(t, r.Root, "config", "filter.evil.clean", script("clean"))
	mustGit(t, r.Root, "config", "filter.evil.smudge", script("smudge"))
	mustGit(t, r.Root, "config", "diff.evil.textconv", script("textconv"))
	// Also through an included file, as an archive could ship.
	inc := filepath.Join(r.Root, ".git", "extra.cfg")
	if err := os.WriteFile(inc, []byte("[filter \"inc\"]\n\tclean = "+script("included")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.Root, "config", "include.path", inc)
	if err := os.WriteFile(filepath.Join(r.Root, ".git", "info", "attributes"), []byte("*.md filter=evil diff=evil\n*.txt filter=inc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.Root, "README.md", "changed\n")
	writeFile(t, r.Root, "notes.txt", "new\n")
	ctx := context.Background()
	if _, err := r.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Blame(ctx, "README.md", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ChangesDiff(ctx, []string{"README.md"}, nil, 1<<20); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clean", "smudge", "textconv", "included"} {
		if _, err := os.Stat(marker(name)); err == nil {
			t.Errorf("the repository's %s driver was executed", name)
		}
	}
}

// In Restricted Mode a commit must not run the folder's hooks.
func TestUntrustedRepoRunsNoHooks(t *testing.T) {
	r := newRepo(t)
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := filepath.Join(r.Root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	Untrusted = func(string) bool { return true }
	defer func() { Untrusted = nil }()
	writeFile(t, r.Root, "README.md", "changed\n")
	if _, err := r.git(context.Background(), "commit", "-am", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("pre-commit hook ran in an untrusted folder")
	}
}
