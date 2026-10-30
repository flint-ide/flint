package agent

import (
	"os/exec"
	"strings"
	"testing"
)

func TestShellQuoteRoundTrip(t *testing.T) {
	for _, s := range []string{"plain", "it's here", `"$HOME" && rm -rf / ; echo $(x)`, "multi\nline", ""} {
		out, err := exec.Command("/bin/sh", "-c", "printf %s "+ShellQuote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("quoted %q came back as %q", s, out)
		}
	}
}

func TestInteractiveCommand(t *testing.T) {
	cmd, err := InteractiveCommand(Claude, "fix it's conflicts")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != `claude 'fix it'\''s conflicts'` {
		t.Errorf("command = %s", cmd)
	}
	if _, err := InteractiveCommand("vim", "x"); err == nil {
		t.Error("unknown agent must be rejected")
	}
}

func TestCleanCommitMessage(t *testing.T) {
	tests := map[string]string{
		"feat: add x":                   "feat: add x",
		"```\nfeat: add x\n\nbody\n```": "feat: add x\n\nbody",
		"```text\nfix: y\n```":          "fix: y",
		"Commit message: chore: bump":   "chore: bump",
		`"docs: readme"`:                "docs: readme",
		"  \n refactor: split files \n": "refactor: split files",
	}
	for in, want := range tests {
		if got := CleanCommitMessage(in); got != want {
			t.Errorf("CleanCommitMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrompts(t *testing.T) {
	p := CommitMessagePrompt([]string{"feat: a", "fix: b"})
	if !strings.Contains(p, "- feat: a") || !strings.Contains(p, "commit message only") {
		t.Errorf("commit prompt missing parts: %s", p)
	}
	c := ConflictPrompt("rebasing", []string{"a.go", "b/c.ts"})
	for _, want := range []string{"rebasing", "- a.go", "- b/c.ts", "Do NOT commit", "ask me"} {
		if !strings.Contains(c, want) {
			t.Errorf("conflict prompt missing %q", want)
		}
	}
	if !strings.Contains(ConflictPrompt("", []string{"x"}), "merging") {
		t.Error("empty operation should default to merging")
	}
}

func TestValidAndAvailable(t *testing.T) {
	if !Valid(Claude) || !Valid(Codex) || Valid("emacs") {
		t.Error("Valid misclassifies agents")
	}
	for _, k := range Available() {
		if !Valid(k) {
			t.Errorf("Available returned unknown agent %q", k)
		}
	}
}
