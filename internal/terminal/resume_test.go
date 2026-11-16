package terminal

import (
	"os"
	"path/filepath"
	"testing"
)

const testSession = "0f8fad5b-d9cb-469f-a165-70867728950e"

func writeSession(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResumeOf(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "100.json", `{"pid": 100, "sessionId": "`+testSession+`", "cwd": "/ws"}`)

	tests := []struct {
		name string
		args []string
		pid  int
		want Resume
	}{
		{"claude with its session", []string{"claude"}, 100, Resume{Agent: "claude", Session: testSession}},
		{"claude run by node", []string{"node", "/opt/homebrew/bin/claude", "--resume"}, 100, Resume{Agent: "claude", Session: testSession}},
		{"claude with a prompt", []string{"/usr/local/bin/claude", "fix the login form"}, 100, Resume{Agent: "claude", Session: testSession}},
		{"claude without a record", []string{"claude"}, 200, Resume{Agent: "claude"}},
		{"codex", []string{"codex"}, 300, Resume{Agent: "codex"}},
		{"codex resume", []string{"codex", "resume", "--last"}, 300, Resume{Agent: "codex"}},
		{"claude print mode", []string{"claude", "-p", "summarize"}, 100, Resume{}},
		{"claude subcommand", []string{"claude", "mcp", "list"}, 100, Resume{}},
		{"codex exec", []string{"codex", "exec", "run the tests"}, 300, Resume{}},
		{"another program", []string{"npm", "run", "dev"}, 100, Resume{}},
		{"a program named alike", []string{"claude-monitor"}, 100, Resume{}},
		{"a script interpreted by node", []string{"node", "server.js"}, 100, Resume{}},
		{"nothing", nil, 100, Resume{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resumeOf(tt.args, tt.pid, dir); got != tt.want {
				t.Errorf("resumeOf(%q, %d) = %+v, want %+v", tt.args, tt.pid, got, tt.want)
			}
		})
	}
}

func TestClaudeSessionTrustsOnlyAValidRecord(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "1.json", `{"pid": 1, "sessionId": "`+testSession+`"}`)
	writeSession(t, dir, "2.json", `{"pid": 7, "sessionId": "`+testSession+`"}`)              // another process's record
	writeSession(t, dir, "3.json", `{"pid": 3, "sessionId": "x; rm -rf ~"}`)                  // not an id
	writeSession(t, dir, "4.json", `{"pid": 4, "sessionId": "`+testSession+`\n--dangerous"}`) // an id and more
	writeSession(t, dir, "5.json", `not json`)
	writeSession(t, dir, "6.json", `{"pid": 6}`)

	if got := claudeSession(dir, 1); got != testSession {
		t.Errorf("claudeSession(1) = %q, want the session", got)
	}
	for _, pid := range []int{2, 3, 4, 5, 6, 99, 0, -1} {
		if got := claudeSession(dir, pid); got != "" {
			t.Errorf("claudeSession(%d) = %q, want nothing", pid, got)
		}
	}
	if got := claudeSession("", 1); got != "" {
		t.Errorf("claudeSession without a directory = %q", got)
	}
}

func TestClaudeSessionsDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/claude")
	if got := claudeSessionsDir(); got != "/custom/claude/sessions" {
		t.Errorf("claudeSessionsDir() = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/home/someone")
	if got := claudeSessionsDir(); got != "/home/someone/.claude/sessions" {
		t.Errorf("claudeSessionsDir() = %q", got)
	}
}

func TestResumeOfUnknownTerminal(t *testing.T) {
	m := NewManager(nil)
	if _, err := m.Resume("missing"); err == nil {
		t.Error("Resume of a terminal that does not exist succeeded")
	}
}
