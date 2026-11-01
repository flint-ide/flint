package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/agent"
	"flint/internal/settings"
)

// fakeClaude writes an executable that stands in for `claude -p`: it saves
// its arguments and stdin next to itself and runs body.
func fakeClaude(t *testing.T, body string) (path, dir string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args\"\ncat > \"" + dir + "/stdin\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, dir
}

func inlineEditApp(t *testing.T, claude string, trusted bool) (*AgentAPI, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Default()
	s.DefaultAgent = "claude"
	s.Agents = map[string]settings.AgentSettings{"claude": {Path: claude, Args: []string{"--verbose"}}}
	a := &App{configDir: t.TempDir(), settings: s, roots: []string{root}}
	if trusted {
		a.state.TrustedFolders = []string{root}
	}
	return &AgentAPI{app: a}, root
}

func TestInlineEdit(t *testing.T) {
	claude, out := fakeClaude(t, "printf '```go\\nif err := do(); err != nil {\\n\\treturn err\\n}\\n```\\n'")
	api, root := inlineEditApp(t, claude, true)
	req := agent.EditRequest{
		Path: filepath.Join(root, "pkg", "x.go"), Language: "go", Instruction: "add error handling",
		Before: "func f() error {\n", Selection: "\tdo()\n", After: "}\n",
	}
	got, err := api.InlineEdit(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if want := "if err := do(); err != nil {\n\treturn err\n}"; got != want {
		t.Errorf("InlineEdit() = %q, want %q", got, want)
	}
	stdin, _ := os.ReadFile(filepath.Join(out, "stdin"))
	if !strings.Contains(string(stdin), "File: pkg/x.go") || !strings.Contains(string(stdin), "\tdo()\n") {
		t.Errorf("agent input:\n%s", stdin)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args"))
	if !strings.Contains(string(args), "add error handling") || !strings.Contains(string(args), "--verbose") {
		t.Errorf("agent arguments:\n%s", args)
	}
	if strings.Contains(string(args), "--setting-sources") {
		t.Errorf("trusted folder ran with restricted settings:\n%s", args)
	}
}

func TestInlineEditRestrictedMode(t *testing.T) {
	claude, out := fakeClaude(t, "echo 'x()'")
	api, root := inlineEditApp(t, claude, false)
	if _, err := api.InlineEdit(context.Background(), agent.EditRequest{Path: filepath.Join(root, "a.go"), Instruction: "x", Selection: "y"}); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args"))
	if !strings.Contains(string(args), "--setting-sources\nuser") || strings.Contains(string(args), "--verbose") {
		t.Errorf("untrusted folder: agent arguments\n%s", args)
	}
}

func TestInlineEditRefusesFilesOutsideTheProject(t *testing.T) {
	claude, _ := fakeClaude(t, "echo x")
	api, _ := inlineEditApp(t, claude, true)
	for _, p := range []string{"/etc/hosts", "relative/a.go"} {
		if _, err := api.InlineEdit(context.Background(), agent.EditRequest{Path: p, Instruction: "x", Selection: "y"}); err == nil {
			t.Errorf("InlineEdit(%s) accepted", p)
		}
	}
}

func TestInlineEditCancelStopsTheAgent(t *testing.T) {
	claude, _ := fakeClaude(t, "exec sleep 30")
	api, root := inlineEditApp(t, claude, true)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := api.InlineEdit(ctx, agent.EditRequest{Path: filepath.Join(root, "a.go"), Instruction: "x", Selection: "y"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("cancelled run took %s", d)
	}
}
