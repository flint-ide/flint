package vscodeimport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourcesFindsMacOSAndLinuxLocations(t *testing.T) {
	home := t.TempDir()
	mac := filepath.Join(home, "Library", "Application Support", "Code", "User")
	linux := filepath.Join(home, ".config", "Cursor", "User")
	for _, dir := range []string{mac, linux} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for _, s := range Sources(home) {
		got[s.ID] = s.UserDir
	}
	if got["vscode"] != mac || got["cursor"] != linux || len(got) != 2 {
		t.Errorf("Sources() = %v", got)
	}
}
