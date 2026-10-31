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
	cmd, err := InteractiveCommand(Claude, Config{}, "fix it's conflicts", false)
	if err != nil {
		t.Fatal(err)
	}
	if cmd != `claude 'fix it'\''s conflicts'` {
		t.Errorf("command = %s", cmd)
	}
	if _, err := InteractiveCommand("vim", Config{}, "x", false); err == nil {
		t.Error("unknown agent must be rejected")
	}
}

func TestInteractiveCommandWithConfig(t *testing.T) {
	cfg := Config{Path: "/opt/my tools/claude", Model: "opus", Args: []string{"--add-dir", "/tmp/notes"}}
	cmd, err := InteractiveCommand(Claude, cfg, "go", false)
	if err != nil {
		t.Fatal(err)
	}
	// The prompt precedes the extra arguments: --add-dir takes several values.
	if want := `'/opt/my tools/claude' '--model' 'opus' 'go' '--add-dir' '/tmp/notes'`; cmd != want {
		t.Errorf("command = %s, want %s", cmd, want)
	}
	restricted, err := InteractiveCommand(Claude, cfg, "go", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(restricted, "--add-dir") {
		t.Errorf("Restricted Mode kept the extra arguments: %s", restricted)
	}
	if _, err := InteractiveCommand(Claude, Config{Args: []string{"--resume"}}, "go", false); err == nil {
		t.Error("reserved flag accepted")
	}
	if _, err := InteractiveCommand(Claude, Config{Path: "bin/claude"}, "go", false); err == nil {
		t.Error("relative path accepted")
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
	for _, k := range Available(nil) {
		if !Valid(k) {
			t.Errorf("Available returned unknown agent %q", k)
		}
	}
}
