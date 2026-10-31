// Package git drives the git CLI. Using the real binary (instead of a Go reimplementation)
// keeps behaviour identical to the terminal: worktrees, hooks, credential helpers, config.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"flint/internal/shellenv"
)

// ErrNotRepository is returned when the folder isn't inside a git work tree.
var ErrNotRepository = errors.New("not a git repository")

// Repo is one git work tree (the main checkout or a linked worktree).
type Repo struct {
	// Root is the top-level directory of the work tree.
	Root string
	// GitDir is this work tree's private git dir (HEAD, index); for linked worktrees it
	// lives under the main repository's .git/worktrees.
	GitDir string
	// CommonDir holds refs and objects shared by all worktrees.
	CommonDir string
}

var (
	binOnce sync.Once
	binPath string
)

func gitBinary() string {
	binOnce.Do(func() {
		shellenv.Wait()
		if p, err := exec.LookPath("git"); err == nil {
			binPath = p
			return
		}
		for _, c := range []string{"/opt/homebrew/bin/git", "/usr/local/bin/git", "/usr/bin/git"} {
			if _, err := os.Stat(c); err == nil {
				binPath = c
				return
			}
		}
		binPath = "git"
	})
	return binPath
}

// Open discovers the repository containing dir.
func Open(ctx context.Context, dir string) (*Repo, error) {
	out, err := run(ctx, dir, nil, "rev-parse", "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return nil, ErrNotRepository
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("unexpected rev-parse output: %q", out)
	}
	common := lines[2]
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return &Repo{Root: lines[0], GitDir: lines[1], CommonDir: filepath.Clean(common)}, nil
}

// git runs a git command in the work tree.
func (r *Repo) git(ctx context.Context, args ...string) ([]byte, error) {
	return run(ctx, r.Root, nil, args...)
}

// gitInput runs a git command feeding stdin.
func (r *Repo) gitInput(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return run(ctx, r.Root, stdin, args...)
}

// safeConfig is set on every git Flint runs: a repository's own .git/config
// (e.g. from a downloaded archive) must not get commands run just by opening the
// folder. core.fsmonitor names a program git starts on status/diff.
var safeConfig = []string{"-c", "core.fsmonitor=false"}

func run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, gitBinary(), append(slices.Clone(safeConfig), args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",  // never fight an agent's git for index.lock on read-only commands
		"GIT_TERMINAL_PROMPT=0", // no hidden password prompts: fail fast instead
		"GIT_EDITOR=true",       // never open an editor (e.g. merge commit messages)
		"LC_ALL=C",              // stable, parseable messages
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.Bytes(), &Error{Args: args, Msg: msg, Err: err}
	}
	return stdout.Bytes(), nil
}

// Error carries git's own stderr, which is what the user needs to see.
type Error struct {
	Args []string
	Msg  string
	Err  error
}

func (e *Error) Error() string { return e.Msg }

func (e *Error) Unwrap() error { return e.Err }

// Rel converts an absolute path inside the work tree to a slash-separated relative path.
// Root comes from git with symlinks resolved (/private/var on macOS), so a path
// given in unresolved form is retried after resolving its directory.
func (r *Repo) Rel(path string) (string, error) {
	if rel, ok := relInside(r.Root, path); ok {
		return rel, nil
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		if rel, ok := relInside(r.Root, filepath.Join(dir, filepath.Base(path))); ok {
			return rel, nil
		}
	}
	return "", fmt.Errorf("%s is outside the repository %s", path, r.Root)
}

func relInside(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// splitNUL splits -z output, dropping the trailing empty element.
func splitNUL(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}
