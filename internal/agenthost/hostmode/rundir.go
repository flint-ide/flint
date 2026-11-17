package hostmode

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// File names inside the run directory (docs/ARCHITECTURE.md, "Where Flint
// stores data").
const (
	lockName  = "host.lock"
	sockName  = "host.sock"
	tokenName = "host.token"
)

// maxSockPath is the longest socket path used. The kernel's limit is 104
// bytes on macOS and 108 on Linux (sockaddr_un.sun_path including the
// terminating NUL); a margin keeps a fallback decision from depending on the
// platform.
const maxSockPath = 100

// Paths are the files of one host, all inside its run directory.
type Paths struct {
	Dir   string
	Lock  string // flock: one host per config directory
	Sock  string // the Unix socket clients connect to
	Token string // the secret clients present in hello
}

func pathsIn(dir string) Paths {
	return Paths{
		Dir: dir, Lock: filepath.Join(dir, lockName),
		Sock: filepath.Join(dir, sockName), Token: filepath.Join(dir, tokenName),
	}
}

// RunDir is the run directory of the host that serves configDir: the one
// place both the host and the windows look for its socket and token.
func RunDir(configDir string) string {
	return runDir(runtime.GOOS, configDir, os.Getenv("XDG_RUNTIME_DIR"), os.Getuid())
}

// runDir decides the run directory from its inputs only, so the rule can be
// tested without sockets.
//
// On Linux it is $XDG_RUNTIME_DIR/flint-<hash>: that directory is private to
// the user, on a tmpfs and cleaned at logout, which is what a socket and a
// token want. macOS has no equivalent that is both stable and short, so the
// run directory lives in the config directory, which is the user's own. The
// hash keeps hosts of different config directories (FLINT_CONFIG_DIR) apart
// inside a shared parent.
//
// A path the kernel would refuse for a socket (or no XDG_RUNTIME_DIR) falls
// back to /tmp/flint-<uid>-<hash>; the uid keeps users out of each other's
// names, and the owner check in prepareDir catches a squatter.
func runDir(goos, configDir, xdgRuntime string, uid int) string {
	configDir = filepath.Clean(configDir)
	sum := sha256.Sum256([]byte(configDir))
	hash := hex.EncodeToString(sum[:4]) // 8 hex digits

	var dir string
	switch {
	case goos == "linux" && xdgRuntime != "":
		dir = filepath.Join(xdgRuntime, "flint-"+hash)
	case goos == "linux":
		// No per-user runtime directory (a minimal container, su without a session).
	default:
		dir = filepath.Join(configDir, "run")
	}
	if dir != "" && len(filepath.Join(dir, sockName)) <= maxSockPath {
		return dir
	}
	// Literally /tmp, not $TMPDIR: on macOS that is a long per-user path.
	return filepath.Join("/tmp", "flint-"+strconv.Itoa(uid)+"-"+hash)
}
