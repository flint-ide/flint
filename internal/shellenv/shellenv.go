// Package shellenv imports PATH from the user's login shell. macOS GUI apps inherit a
// minimal launchd environment, so without this git hooks (husky, pre-commit), rg and
// tools installed via brew/nvm/asdf wouldn't be found. Linux desktop sessions
// usually start from a login shell, but a launcher (a .desktop file, an AppImage)
// still misses what .bashrc / .zshrc add (nvm, asdf, ~/.local/bin), so the
// import runs there too.
package shellenv

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	marker = "__FLINT_PATH__="
	// timeout is generous: right after login the machine is busy and a .zshrc
	// with nvm/rvm/sdkman can take several seconds; a miss leaves the app with
	// launchd's PATH until restart.
	timeout = 10 * time.Second
	// waitDelay bounds the wait for stdout to close after the shell exits.
	waitDelay = time.Second
)

var (
	once sync.Once
	done = make(chan struct{})
)

// Load starts resolving the login-shell PATH in the background. Safe to call repeatedly.
func Load() {
	once.Do(func() {
		go func() {
			defer close(done)
			current := os.Getenv("PATH")
			if path := loginPath(); path != "" {
				current = mergePaths(path, current)
			}
			// Always add the usual install locations: when the login shell
			// timed out or failed, Homebrew & co. are still found.
			home, _ := os.UserHomeDir()
			fallback := strings.Join(fallbackDirs(home, dirExists), ":")
			_ = os.Setenv("PATH", mergePaths(current, fallback))
		}()
	})
}

// fallbackDirs lists the common tool directories that exist on this machine,
// in the order a typical login shell would put them.
func fallbackDirs(home string, exists func(string) bool) []string {
	candidates := []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin"}
	if home != "" {
		for _, rel := range []string{".local/bin", "go/bin", ".cargo/bin", ".bun/bin", ".docker/bin"} {
			candidates = append(candidates, home+"/"+rel)
		}
	}
	out := make([]string, 0, len(candidates))
	for _, d := range candidates {
		if exists(d) {
			out = append(out, d)
		}
	}
	return out
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Wait blocks until the PATH import finished (or timed out). Call before spawning tools.
func Wait() {
	Load()
	<-done
}

// UserShell is the user's shell ($SHELL), else the system's default: zsh on
// macOS, bash (or sh) on Linux.
func UserShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return defaultShell(runtime.GOOS, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func defaultShell(goos string, exists func(string) bool) string {
	if goos == "darwin" {
		return "/bin/zsh"
	}
	if exists("/bin/bash") {
		return "/bin/bash"
	}
	return "/bin/sh"
}

func loginPath() string {
	shell := UserShell()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// -i loads .zshrc/.bashrc too, where nvm & co. usually extend PATH.
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '\n`+marker+`%s\n' "$PATH"`)
	cmd.Stdin = nil
	// A startup file may leave a background process (an agent, a daemon)
	// holding stdout; Output would wait for it to exit, and with it every
	// caller of Wait (git, agents, language servers).
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	return ParseMarker(string(out))
}

// ParseMarker extracts the PATH printed after the marker, ignoring any noise that
// shell startup files print before it.
func ParseMarker(out string) string {
	i := strings.LastIndex(out, marker)
	if i < 0 {
		return ""
	}
	line := out[i+len(marker):]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	return strings.TrimSpace(line)
}

// mergePaths puts the login PATH first and keeps any extra entries of the current one.
func mergePaths(login, current string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(strings.Split(login, ":"), strings.Split(current, ":")...) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, ":")
}
