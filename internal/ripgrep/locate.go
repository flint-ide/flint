// Package ripgrep locates the rg binary. GUI apps on macOS don't inherit the shell PATH,
// so besides PATH we probe the usual package-manager install locations.
package ripgrep

import (
	"os"
	"os/exec"
	"sync"
)

var candidates = []string{
	"/opt/homebrew/bin/rg",
	"/usr/local/bin/rg",
	"/usr/bin/rg",
	"/run/current-system/sw/bin/rg",
}

var (
	once sync.Once
	path string
)

// Path returns the absolute path to rg, or "" when it isn't installed.
// FLINT_RG overrides discovery. The result is cached for the process lifetime.
func Path() string {
	once.Do(func() { path = locate() })
	return path
}

func locate() string {
	if p := os.Getenv("FLINT_RG"); p != "" {
		return p
	}
	if p, err := exec.LookPath("rg"); err == nil {
		return p
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}
