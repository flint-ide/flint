package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"flint/internal/extensions/manifest"
)

const nordManifest = `{"contract":1,"id":"me.nord","name":"Nord","version":"1.0.0",
 "contributes":{"themes":[{"id":"me.nord.dark","label":"Nord","base":"dark",
   "colors":{"--bg-editor":"#2e3440"},"terminal":{"red":"#bf616a"}}]}}`

// newHost is a host over an empty extensions folder that confirms everything.
func newHost(t *testing.T) (*Host, *fakeApp) {
	t.Helper()
	app := &fakeApp{confirm: true, trusted: map[string]bool{}, disabled: map[string]bool{}, settings: map[string]json.RawMessage{}}
	h := New(Options{Dir: filepath.Join(t.TempDir(), "extensions"), App: app, Secrets: &memSecrets{m: map[string]string{}}, Environ: MinimalEnv})
	t.Cleanup(h.Close)
	return h, app
}

func writeFolder(t *testing.T, man string) string {
	t.Helper()
	dir := realDir(t)
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallThemeOnly(t *testing.T) {
	h, app := newHost(t)
	info, err := h.Install(writeFolder(t, nordManifest))
	if err != nil {
		t.Fatalf("Install() = %v", err)
	}
	if info.Program != "" || len(info.Themes) != 1 || info.Themes[0].Terminal["red"] != "#bf616a" || info.Themes[0].Colors["--bg-editor"] != "#2e3440" {
		t.Errorf("Info = %+v", info)
	}
	msg := app.asked[0]
	if !strings.Contains(msg, "Color themes: 1") || !strings.Contains(msg, "runs no code") || strings.Contains(msg, "Program:") {
		t.Errorf("install dialog = %q", msg)
	}
}

func TestInstallRefusesLinksOutsideAndFIFOs(t *testing.T) {
	h, _ := newHost(t)
	src := writeFolder(t, nordManifest)
	secret := filepath.Join(realDir(t), "secret.txt")
	if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "secret.txt")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := h.Install(src); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("Install(link outside) = %v", err)
	}
	if err := os.Remove(filepath.Join(src, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skip("mkfifo unsupported:", err)
	}
	if _, err := h.Install(src); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("Install(FIFO) = %v", err)
	}
}

func TestInstallKeepsLinksInside(t *testing.T) {
	h, _ := newHost(t)
	src := writeFolder(t, nordManifest)
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("README.md", filepath.Join(src, "README-link.md")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	info, err := h.Install(src)
	if err != nil {
		t.Fatalf("Install() = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(info.Dir, "README-link.md"))
	if err != nil || string(data) != "hi" {
		t.Errorf("README-link.md = %q, %v; want a copy of the file it points to", data, err)
	}
}
