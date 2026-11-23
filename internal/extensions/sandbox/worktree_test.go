package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TakeFingerprint and Changed see every way a script could plant what a
// later step runs (writing, creating, removing, replacing a file by a link,
// another letter case) and nothing else of the worktree.
func TestWorktreeFingerprint(t *testing.T) {
	setup := func(t *testing.T) string {
		wt := t.TempDir()
		for p, text := range map[string]string{
			".git": "gitdir: /repo/.git/worktrees/x\n", ".claude/settings.json": "{}", "CLAUDE.md": "rules\n", "src/a.go": "package a\n",
		} {
			full := filepath.Join(wt, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return wt
	}
	write := func(p, text string) func(string) error {
		return func(wt string) error {
			full := filepath.Join(wt, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			return os.WriteFile(full, []byte(text), 0o644)
		}
	}
	tests := []struct {
		name   string
		change func(wt string) error
		want   []string
	}{
		{"nothing", func(string) error { return nil }, nil},
		{"an ordinary file", write("src/a.go", "package b\n"), nil},
		{"a nested CLAUDE.md is not checked", write("src/CLAUDE.md", "x"), nil},
		{"hooks", write(".claude/settings.json", `{"hooks":{}}`), []string{".claude/settings.json"}},
		{"a new file in .claude", write(".claude/settings.local.json", "{}"), []string{".claude/settings.local.json"}},
		{"instructions", write("CLAUDE.md", "rules, and more\n"), []string{"CLAUDE.md"}},
		{"AGENTS.md created", write("AGENTS.md", "x"), []string{"AGENTS.md"}},
		{"a .flint workflow", write(".flint/workflows/x.md", "x"), []string{".flint", ".flint/workflows", ".flint/workflows/x.md"}},
		{"another case", write(".Codex/config.toml", "x"), []string{".Codex", ".Codex/config.toml"}},
		{"the .git file", write(".git", "gitdir: /tmp/evil\n"), []string{".git"}},
		{".git replaced by a link", func(wt string) error {
			if err := os.Remove(filepath.Join(wt, ".git")); err != nil {
				return err
			}
			return os.Symlink("/tmp/evil", filepath.Join(wt, ".git"))
		}, []string{".git"}},
		{"removed", func(wt string) error { return os.RemoveAll(filepath.Join(wt, ".claude")) }, []string{".claude", ".claude/settings.json"}},
		{"permissions", func(wt string) error { return os.Chmod(filepath.Join(wt, "CLAUDE.md"), 0o755) }, []string{"CLAUDE.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wt := setup(t)
			before, err := TakeFingerprint(wt)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.change(wt); err != nil {
				t.Fatal(err)
			}
			if got := ChangedSince(wt, before); !slices.Equal(got, tt.want) {
				t.Errorf("changed %q, want %q", got, tt.want)
			}
		})
	}
}

// A large file is known by its size and change time, and a tree past the
// bound cannot be checked (the step then fails rather than run unchecked).
func TestWorktreeFingerprintBounds(t *testing.T) {
	wt := t.TempDir()
	big := filepath.Join(wt, "CLAUDE.md")
	if err := os.WriteFile(big, make([]byte, maxHashBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := TakeFingerprint(wt)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(big, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteAt([]byte("x"), 10) // same size
	f.Close()
	if got := ChangedSince(wt, before); !slices.Equal(got, []string{"CLAUDE.md"}) {
		t.Errorf("a large file's change: %q", got)
	}
	dir := filepath.Join(wt, ".idea")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range maxFingerprintEntries {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprint("f", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := TakeFingerprint(wt); err == nil {
		t.Error("a tree past the bound was fingerprinted")
	}
	if got := ChangedSince(wt, before); len(got) != 1 || !strings.Contains(got[0], "cannot be checked") {
		t.Errorf("unchecked tree reported as %q", got)
	}
}

// The macOS profile closes WorktreeProtected inside the worktree's write
// allowance, after it, as subpaths and in any letter case.
func TestScriptProfileDeniesWorktreeConfiguration(t *testing.T) {
	p := Policy{GOOS: "darwin", ExtensionDir: "/w/t", Write: []string{"/w/t"}, ExecAny: true, DenyWrite: worktreeDenyWrite("/w/t")}
	prof, err := p.DarwinProfile()
	if err != nil {
		t.Fatal(err)
	}
	allow := strings.Index(prof, "(allow file-write*")
	deny := strings.Index(prof, "(deny file-write*\n")
	if allow < 0 || deny < allow {
		t.Fatalf("the deny rules do not follow the write allowance:\n%s", prof)
	}
	for _, want := range []string{`(subpath "/w/t/.git")`, `(subpath "/w/t/CLAUDE.md")`, `(regex #"^/w/t/\.[cC][lL][aA][uU][dD][eE](/|$)")`, `(regex #"^/w/t/[aA][gG][eE][nN][tT][sS]\.[mM][dD](/|$)")`} {
		if !strings.Contains(prof, want) {
			t.Errorf("profile lacks %s:\n%s", want, prof)
		}
	}
	if got := anyCase(`/a "b"/x`); got != `^/a \"b\"/[xX](/|$)` {
		t.Errorf("anyCase quoted %q", got)
	}
}
