package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTypeScriptServerIsPinnedToItsOwnTypeScript(t *testing.T) {
	prefix := t.TempDir()
	cli := filepath.Join(prefix, "lib", "node_modules", "typescript-language-server", "lib", "cli.mjs")
	ts := filepath.Join(prefix, "lib", "node_modules", "typescript", "lib", "tsserver.js")
	for _, f := range []string{cli, ts} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("//"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(prefix, "bin", "typescript-language-server")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	if err := os.Symlink(cli, bin); err != nil {
		t.Fatal(err)
	}
	opts, err := initializationOptions(bin)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := opts["tsserver"].(map[string]any)["path"].(string)
	want, _ := filepath.EvalSymlinks(ts)
	if got != want {
		t.Fatalf("tsserver path = %q, want %q", got, want)
	}
	if opts, err := initializationOptions("/usr/local/bin/gopls"); opts != nil || err != nil {
		t.Fatal("options for another server")
	}
}

// Without its own TypeScript the server would use the project's: refuse it.
func TestTypeScriptServerWithoutItsOwnTypeScriptIsRefused(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "typescript-language-server")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := initializationOptions(bin); err == nil {
		t.Fatal("unpinned typescript-language-server allowed")
	}
}

// In an untrusted folder rust-analyzer must not start (its build scripts are
// the project's code); trusting the folder starts it for the open file.
func TestRestrictedModeHoldsBackCodeRunningServers(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	ra := filepath.Join(dir, "rust-analyzer")
	if err := os.WriteFile(ra, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	trusted := false
	m := NewManager()
	m.lookPath = func(name string) (string, error) {
		if name == "rust-analyzer" {
			return ra, nil
		}
		return "", os.ErrNotExist
	}
	m.Trusted = func(string) bool { return trusted }
	defer m.Shutdown()

	m.Open(root, filepath.Join(root, "main.rs"), "fn main() {}")
	waitFor(t, func() bool { return hasState(m, "restricted") })
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("rust-analyzer started in an untrusted folder")
	}
	trusted = true
	m.Retrust(root)
	waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
}

func hasState(m *Manager, word string) bool {
	for _, s := range m.Statuses() {
		if strings.Contains(strings.ToLower(s.Message), word) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
