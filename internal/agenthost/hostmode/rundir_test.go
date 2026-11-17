package hostmode

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func hash8(configDir string) string {
	sum := sha256.Sum256([]byte(configDir))
	return hex.EncodeToString(sum[:4])
}

func TestRunDir(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	// A config dir for which <configDir>/run/host.sock is exactly the limit, and one byte over.
	exact := "/" + strings.Repeat("e", maxSockPath-len("/run/host.sock")-1)
	over := exact + "e"

	tests := []struct {
		name      string
		goos      string
		configDir string
		xdg       string
		uid       int
		want      string
	}{
		{"linux uses XDG_RUNTIME_DIR", "linux", "/home/a/.config/flint", "/run/user/1000", 1000,
			"/run/user/1000/flint-" + hash8("/home/a/.config/flint")},
		{"linux without XDG_RUNTIME_DIR", "linux", "/home/a/.config/flint", "", 1000,
			"/tmp/flint-1000-" + hash8("/home/a/.config/flint")},
		{"linux with a long XDG_RUNTIME_DIR", "linux", "/home/a/.config/flint", long, 1000,
			"/tmp/flint-1000-" + hash8("/home/a/.config/flint")},
		{"darwin keeps it in the config dir", "darwin", "/Users/a/Library/Application Support/flint", "", 501,
			"/Users/a/Library/Application Support/flint/run"},
		{"darwin ignores XDG_RUNTIME_DIR", "darwin", "/Users/a/cfg", "/run/user/501", 501, "/Users/a/cfg/run"},
		{"darwin with a long config dir falls back", "darwin", long, "", 501, "/tmp/flint-501-" + hash8(long)},
		{"a socket path of exactly the limit stays", "darwin", exact, "", 501, exact + "/run"},
		{"one byte over falls back", "darwin", over, "", 501, "/tmp/flint-501-" + hash8(over)},
		{"the config dir is cleaned before hashing", "linux", "/home/a/cfg/", "/run/user/7", 7,
			"/run/user/7/flint-" + hash8("/home/a/cfg")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runDir(tt.goos, tt.configDir, tt.xdg, tt.uid)
			if got != tt.want {
				t.Errorf("runDir = %q, want %q", got, tt.want)
			}
			if sock := filepath.Join(got, sockName); len(sock) > maxSockPath {
				t.Errorf("socket path %q is %d bytes, over %d", sock, len(sock), maxSockPath)
			}
		})
	}
}

func TestRunDirDiffersPerConfigDir(t *testing.T) {
	a := runDir("linux", "/a", "/run/user/1", 1)
	b := runDir("linux", "/b", "/run/user/1", 1)
	if a == b {
		t.Errorf("two config dirs share %q", a)
	}
}

func TestPathsAreInsideTheRunDir(t *testing.T) {
	p := pathsIn("/r")
	if p.Lock != "/r/host.lock" || p.Sock != "/r/host.sock" || p.Token != "/r/host.token" {
		t.Errorf("paths = %+v", p)
	}
}
