package diag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogDirPerPlatform(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := []struct{ goos, state, want string }{
		{"darwin", "/ignored", filepath.Join(home, "Library", "Logs", "Flint")},
		{"linux", "", filepath.Join(home, ".local", "state", "flint")},
		{"linux", "/var/state", filepath.Join("/var/state", "flint")},
		{"linux", "relative", filepath.Join(home, ".local", "state", "flint")},
	}
	for _, tt := range tests {
		if got := logDir(tt.goos, tt.state); got != tt.want {
			t.Errorf("logDir(%q, %q) = %q, want %q", tt.goos, tt.state, got, tt.want)
		}
	}
}
