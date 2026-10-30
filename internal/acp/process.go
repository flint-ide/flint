package acp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// stderrInError is how much stderr tail is quoted in AgentExitedError.
const stderrInError = 4 << 10

// ErrClosed is returned by calls made on, or pending during, Close.
var ErrClosed = errors.New("acp: client closed")

// AgentExitedError reports that the agent process ended (or its stdout
// closed) while the connection was in use.
type AgentExitedError struct {
	// Err is the process wait error (*exec.ExitError for a non-zero exit),
	// nil for a clean exit.
	Err error
	// Stderr is the tail of the agent's stderr output.
	Stderr string
}

// Error implements error.
func (e *AgentExitedError) Error() string {
	msg := "acp: agent exited"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += "; stderr: " + s
	}
	return msg
}

// Unwrap exposes the wait error.
func (e *AgentExitedError) Unwrap() error { return e.Err }

// buildEnv returns the agent environment: the current process environment
// with extra appended (later entries win in os/exec).
func buildEnv(extra []string) []string {
	return slices.Concat(os.Environ(), extra)
}

// lookupEnv returns the last value of key in env.
func lookupEnv(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], prefix); ok {
			return v
		}
	}
	return ""
}

// resolveCommand finds name using the PATH of the agent's environment
// rather than Flint's own (a GUI app launched from Finder has a minimal
// PATH, while node-based adapters often live under nvm/volta). Relative PATH
// entries are ignored, mirroring os/exec's ErrDot protection.
func resolveCommand(name string, env []string) (string, error) {
	if name == "" {
		return "", errors.New("acp: empty command")
	}
	if strings.ContainsAny(name, `/\`) || runtime.GOOS == "windows" {
		return lookPath(name)
	}
	for _, dir := range filepath.SplitList(lookupEnv(env, "PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return lookPath(name)
}

func lookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("acp: find agent command %q: %w", name, err)
	}
	return path, nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
