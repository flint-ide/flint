package terminal

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCommandLabel(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"npm", "run", "dev"}, "npm run dev"},
		{[]string{"node", "/opt/homebrew/bin/claude", "--resume"}, "claude --resume"},
		{[]string{"python3.12", "manage.py", "runserver"}, "python3.12 manage.py runserver"},
		{[]string{"/usr/local/go/bin/go", "test", "./..."}, "go test ./..."},
		{[]string{"-zsh"}, "zsh"},
		{[]string{"vim", "/Users/me/projects/flint/main.go"}, "vim main.go"},
		{[]string{"docker", "compose", "logs", "-f", "api"}, "docker compose logs -f …"},
		{[]string{"mysql", "-u", "root", "--password=hunter2"}, "mysql -u root …"},
		{[]string{"curl", "-H", "Authorization: Bearer abc", "https://x"}, "curl -H …"},
		{[]string{"echo", strings.Repeat("x", 60)}, "echo " + strings.Repeat("x", 34) + "…"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := commandLabel(tt.args); got != tt.want {
			t.Errorf("commandLabel(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestParseProcArgs(t *testing.T) {
	raw := binary.LittleEndian.AppendUint32(nil, 3)
	raw = append(raw, "/opt/homebrew/bin/node\x00\x00\x00\x00node\x00/opt/homebrew/bin/npm\x00run\x00HOME=/Users/me\x00"...)
	if got, want := parseProcArgs(raw), []string{"node", "/opt/homebrew/bin/npm", "run"}; !slices.Equal(got, want) {
		t.Fatalf("args %q, want %q", got, want)
	}
	if parseProcArgs([]byte{1}) != nil {
		t.Fatal("short input parsed")
	}
}

// nextTitle waits for a reported title matching ok.
func nextTitle(t *testing.T, r *recorder, ok func(Info) bool) Info {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case info := <-r.titles:
			if ok(info) {
				return info
			}
		case <-deadline:
			t.Fatalf("no matching title; output so far: %q", r.output())
		}
	}
}

func TestTitleFollowsForegroundCommand(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	dir := t.TempDir()
	if err := m.Start("t3", Options{Cwd: dir, Shell: "/bin/sh", Args: []string{"-i"}}); err != nil {
		t.Fatal(err)
	}
	defer m.Close("t3")
	idle := nextTitle(t, rec, func(i Info) bool { return i.Command == "" })
	if !strings.HasSuffix(idle.Cwd, strings.TrimPrefix(dir, "/private")) {
		t.Errorf("cwd %q, want %q", idle.Cwd, dir)
	}
	if err := m.Write("t3", []byte("sleep 1\n")); err != nil {
		t.Fatal(err)
	}
	if got := nextTitle(t, rec, func(i Info) bool { return i.Command != "" }); got.Command != "sleep 1" {
		t.Errorf("command %q, want %q", got.Command, "sleep 1")
	}
	nextTitle(t, rec, func(i Info) bool { return i.Command == "" })
}
