package shellenv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseMarker(t *testing.T) {
	tests := map[string]string{
		"motd noise\n" + marker + "/opt/homebrew/bin:/usr/bin\n": "/opt/homebrew/bin:/usr/bin",
		marker + "/a:/b":                     "/a:/b",
		"no marker here":                     "",
		marker + "/x\n" + marker + "/y\nend": "/y",
	}
	for in, want := range tests {
		if got := ParseMarker(in); got != want {
			t.Errorf("ParseMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergePaths(t *testing.T) {
	got := mergePaths("/opt/homebrew/bin:/usr/bin", "/usr/bin:/bin:")
	if want := "/opt/homebrew/bin:/usr/bin:/bin"; got != want {
		t.Errorf("mergePaths = %q, want %q", got, want)
	}
}

func TestWaitResolves(t *testing.T) {
	Wait() // must return (bounded by the timeout) even in CI shells
}

// A login shell that leaves a background process holding its stdout must not
// keep the PATH import waiting for that process.
func TestLoginPathDoesNotWaitForBackgroundProcesses(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	shell := filepath.Join(dir, "shell")
	script := "#!/bin/sh\nsleep 30 &\necho $! > " + pidFile + "\nprintf '\\n" + marker + "/from/login\\n'\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	t.Setenv("SHELL", shell)
	start := time.Now()
	got := loginPath()
	if took := time.Since(start); took > timeout {
		t.Errorf("loginPath took %v", took)
	}
	if got != "/from/login" {
		t.Errorf("loginPath = %q", got)
	}
}
