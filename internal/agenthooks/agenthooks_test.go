package agenthooks

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"flint/internal/terminal"
)

type settingsFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func parse(t *testing.T, data []byte) settingsFile {
	t.Helper()
	var s settingsFile
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("the settings are not JSON: %v\n%s", err, data)
	}
	return s
}

func TestClaudeSettingsMapsEventsToStates(t *testing.T) {
	s := parse(t, ClaudeSettings())
	want := map[string]string{
		"UserPromptSubmit": "working", "PreToolUse": "working", "Notification": "waiting", "Stop": "done",
	}
	if len(s.Hooks) != len(want) {
		t.Errorf("hooks for %d events, want %d", len(s.Hooks), len(want))
	}
	for event, state := range want {
		groups := s.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: %+v", event, groups)
		}
		h := groups[0].Hooks[0]
		if h.Type != "command" || !strings.Contains(h.Command, "6977;state="+state) || !strings.Contains(h.Command, "/dev/tty") {
			t.Errorf("%s: %+v", event, h)
		}
	}
	if m := s.Hooks["PreToolUse"][0].Matcher; m != "*" {
		t.Errorf("PreToolUse matcher = %q: it must cover every tool", m)
	}
}

func TestClaudeSettingsAreDeterministic(t *testing.T) {
	if string(ClaudeSettings()) != string(ClaudeSettings()) {
		t.Fatal("two calls differ: the file would be rewritten every time")
	}
	if string(Snippet()) != string(ClaudeSettings()) {
		t.Error("the snippet users copy differs from the file Flint writes")
	}
}

type stateRecorder struct{ states chan string }

func (r *stateRecorder) Output(string, []byte)       {}
func (r *stateRecorder) Exited(string, int)          {}
func (r *stateRecorder) Title(string, terminal.Info) {}
func (r *stateRecorder) AgentState(_, state string)  { r.states <- state }
func newStateRecorder() *stateRecorder               { return &stateRecorder{states: make(chan string, 8)} }

// Each hook command, run the way Claude Code runs one (through a shell, as a
// child of the program in the terminal, output captured), produces the state
// the terminal's scanner understands.
func TestHookCommandsReachTheTerminalScanner(t *testing.T) {
	s := parse(t, ClaudeSettings())
	for event, groups := range s.Hooks {
		command := groups[0].Hooks[0].Command
		t.Run(event, func(t *testing.T) {
			rec := newStateRecorder()
			m := terminal.NewManager(rec)
			if err := m.Start("t1", terminal.Options{Cwd: t.TempDir(), Shell: "/bin/sh"}); err != nil {
				t.Fatal(err)
			}
			defer m.Close("t1")
			// sh -c with stdout and stderr captured, like a spawned hook.
			line := "sh -c " + QuoteShell(command) + " </dev/null 2>&1 | cat >/dev/null\n"
			if err := m.Write("t1", []byte(line)); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-rec.states:
				if !strings.Contains(command, "state="+got) {
					t.Errorf("state %q from %q", got, command)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("no state from %q", command)
			}
		})
	}
}

func TestHookCommandsDoNothingWithoutATerminal(t *testing.T) {
	// No controlling terminal (a hook run by something else): it must exit 0
	// and print nothing, or it would feed text to the agent's context.
	s := parse(t, ClaudeSettings())
	for event, groups := range s.Hooks {
		out, code := runDetached(t, groups[0].Hooks[0].Command)
		if code != 0 || out != "" {
			t.Errorf("%s without a terminal: exit %d, output %q", event, code, out)
		}
	}
}

// runDetached runs command in a new session: no controlling terminal.
func runDetached(t *testing.T, command string) (string, int) {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

func TestQuoteShell(t *testing.T) {
	tests := map[string]string{
		"/plain/path.json":     `'/plain/path.json'`,
		"/with space/a b.json": `'/with space/a b.json'`,
		"/it's/here":           `'/it'\''s/here'`,
		"$HOME/`x`;rm -rf":     "'$HOME/`x`;rm -rf'",
		"":                     `''`,
	}
	for in, want := range tests {
		if got := QuoteShell(in); got != want {
			t.Errorf("QuoteShell(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestClaudeCommandQuotesThePath(t *testing.T) {
	got, err := ClaudeCommand("/Users/me/Library/Application Support/Flint/agent-hooks/claude-status.json")
	if err != nil {
		t.Fatal(err)
	}
	want := `claude --settings '/Users/me/Library/Application Support/Flint/agent-hooks/claude-status.json'`
	if got != want {
		t.Errorf("got %s", got)
	}
	for _, bad := range []string{"", "relative/path.json", "/has\nnewline", "/has\x00nul"} {
		if _, err := ClaudeCommand(bad); err == nil {
			t.Errorf("ClaudeCommand(%q) accepted", bad)
		}
	}
}

func TestEnsureWritesRegeneratesAndLeavesOtherFilesAlone(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "agent-hooks", "mine.json")
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "agent-hooks", "claude-status.json") {
		t.Errorf("path = %s", path)
	}
	if got, _ := os.ReadFile(path); string(got) != string(ClaudeSettings()) {
		t.Errorf("content = %s", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0o022 != 0 {
		t.Errorf("mode %v: other users could rewrite the hooks", info.Mode())
	}

	// Unchanged content is not rewritten; changed content is.
	before, _ := os.Stat(path)
	if _, err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("an up-to-date file was rewritten")
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(ClaudeSettings()) {
		t.Errorf("a stale file was not regenerated: %s", got)
	}
	if got, _ := os.ReadFile(user); string(got) != "keep me" {
		t.Error("another file in the folder was touched")
	}
}

func TestEnsureRefusesAnEmptyDirectory(t *testing.T) {
	if _, err := Ensure(""); err == nil {
		t.Error("Ensure(\"\") wrote somewhere relative")
	}
}
