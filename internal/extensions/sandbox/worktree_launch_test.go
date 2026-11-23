//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// A sandboxed script on macOS cannot write what later steps of its run
// execute or read in its worktree (WorktreeProtected), in any letter case,
// by writing, creating, renaming or removing; an ordinary file of the
// worktree it writes as before.
func TestScriptSandboxKeepsWorktreeConfiguration(t *testing.T) {
	s := Probe()
	if !s.Available {
		t.Skipf("no sandbox on this system: %s", s.Reason)
	}
	pl := newScriptPlace(t)
	wt := pl.worktree
	writeT := func(p, text string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeT(filepath.Join(wt, ".git"), "gitdir: /repo/.git/worktrees/fix\n")
	writeT(filepath.Join(wt, ".claude", "settings.json"), "{}")
	writeT(filepath.Join(wt, "CLAUDE.md"), "Be careful.\n")
	before, err := TakeFingerprint(wt)
	if err != nil {
		t.Fatal(err)
	}
	ops := []op{
		{"write", filepath.Join(wt, "src.go")},                   // an ordinary file
		{"write", filepath.Join(wt, ".claude", "settings.json")}, // hooks of the next agent
		{"write", filepath.Join(wt, ".git")},                     // the worktree's .git file
		{"sh", "echo 'gitdir: /tmp/evil' > " + filepath.Join(wt, "g") + " && mv " + filepath.Join(wt, "g") + " " + filepath.Join(wt, ".git")},
		{"remove", filepath.Join(wt, ".git")},                                           //
		{"write", filepath.Join(wt, "CLAUDE.md")},                                       // standing instructions
		{"write", filepath.Join(wt, "AGENTS.md")},                                       // created
		{"mkdir", filepath.Join(wt, ".codex")},                                          //
		{"mkdir", filepath.Join(wt, ".flint")},                                          //
		{"mkdir", filepath.Join(wt, ".vscode")},                                         //
		{"mkdir", filepath.Join(wt, ".idea")},                                           //
		{"mkdir", filepath.Join(wt, ".CLAUDE")},                                         // another letter case
		{"mkdir", filepath.Join(wt, ".CoDeX")},                                          // a name not there yet, another case
		{"write", filepath.Join(wt, "claude.md")},                                       //
		{"sh", "mv " + filepath.Join(wt, ".claude") + " " + filepath.Join(wt, "moved")}, // moved away
	}
	results := tryAll(t, place{program: pl.program}, pl.policy(t, false), s, ops...)
	const denied = "operation not permitted"
	expect(t, ops, results, "ok", denied, denied, "exit status", denied, denied, denied, denied, denied, denied, denied, "exists", denied, denied, "exit status")
	after, err := TakeFingerprint(wt)
	if err != nil {
		t.Fatal(err)
	}
	if changed := before.Changed(after); len(changed) != 0 {
		t.Errorf("the script changed %v", changed)
	}
	if data, _ := os.ReadFile(filepath.Join(wt, "src.go")); string(data) != "written" {
		t.Errorf("the ordinary file holds %q", data)
	}
}
