package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/agenthooks"
	"flint/internal/settings"
)

func TestClaudeCommandWritesHooksOnlyForATrustedFolder(t *testing.T) {
	// A config folder whose name needs quoting.
	cfg := filepath.Join(t.TempDir(), "it's a config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	a := &App{configDir: cfg, settings: settings.Default(), ctx: context.Background()}
	a.openProject(project, nil)
	api := &TerminalAPI{app: a}

	if _, err := api.ClaudeCommand(project); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("untrusted folder: %v", err)
	}
	if hooksFileExists(cfg) {
		t.Fatal("the hooks file was written for an untrusted folder")
	}
	if _, err := api.ClaudeCommand(t.TempDir()); err == nil {
		t.Error("a folder outside the window was accepted")
	}
	if _, err := api.ClaudeCommand(""); err == nil {
		t.Error("an empty folder was accepted")
	}

	if err := a.setTrust(project, true); err != nil {
		t.Fatal(err)
	}
	line, err := api.ClaudeCommand(project)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg, agenthooks.Dir, agenthooks.File)
	if want := `claude --settings '` + strings.ReplaceAll(path, "'", `'\''`) + `'`; line != want {
		t.Errorf("line = %s, want %s", line, want)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(agenthooks.ClaudeSettings()) {
		t.Errorf("hooks file = %q, %v", got, err)
	}

	// An out-of-date file is regenerated the next time.
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := api.ClaudeCommand(project); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(agenthooks.ClaudeSettings()) {
		t.Errorf("stale hooks file kept: %s", got)
	}
}

func TestHookSnippetIsTheSameHooks(t *testing.T) {
	api := &TerminalAPI{app: &App{}}
	if api.HookSnippet() != string(agenthooks.ClaudeSettings()) {
		t.Error("the snippet differs from the hooks file")
	}
}
