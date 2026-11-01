package settings

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// AgentTasks configures agent tasks (an agent on its own branch in its own
// worktree, see internal/agenttask).
type AgentTasks struct {
	// WorktreeDir is where task worktrees go: an absolute path or one starting
	// with "~/", holding "<repo>/<branch>". Empty follows the branch widget:
	// "<repo>.worktrees/<branch>" next to the main checkout.
	WorktreeDir string `json:"worktreeDir"`
	// BranchPrefix starts every task branch, e.g. "agent/" gives
	// "agent/add-rate-limiting". May be empty.
	BranchPrefix string `json:"branchPrefix"`
}

const (
	maxWorktreeDirLen   = 1024
	maxBranchPrefixLen  = 40
	defaultBranchPrefix = "agent/"
)

func defaultAgentTasks() AgentTasks { return AgentTasks{BranchPrefix: defaultBranchPrefix} }

// prefixChars are the characters a prefix may use; git allows more, but these
// keep branch names easy to type and safe in a shell.
var prefixChars = regexp.MustCompile(`^[A-Za-z0-9._/-]*$`)

// CheckAgentTasks reports the first invalid value.
func CheckAgentTasks(a AgentTasks) error {
	if err := checkWorktreeDir(a.WorktreeDir); err != nil {
		return err
	}
	return checkBranchPrefix(a.BranchPrefix)
}

func checkWorktreeDir(dir string) error {
	switch {
	case dir == "":
		return nil
	case len(dir) > maxWorktreeDirLen:
		return fmt.Errorf("worktree folder longer than %d characters", maxWorktreeDirLen)
	case strings.ContainsFunc(dir, unicode.IsControl):
		return errors.New("worktree folder contains control characters")
	case !filepath.IsAbs(dir) && dir != "~" && !strings.HasPrefix(dir, "~/"):
		return fmt.Errorf("worktree folder %q must be an absolute path or start with ~/", dir)
	}
	return nil
}

// checkBranchPrefix applies git's ref-name rules that a prefix alone can
// break (the slug after it is always valid).
func checkBranchPrefix(p string) error {
	switch {
	case len(p) > maxBranchPrefixLen:
		return fmt.Errorf("branch prefix longer than %d characters", maxBranchPrefixLen)
	case !prefixChars.MatchString(p):
		return fmt.Errorf("branch prefix %q may only use letters, digits, '.', '_', '-' and '/'", p)
	case strings.HasPrefix(p, "-") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "."):
		return fmt.Errorf("branch prefix %q may not start with '-', '/' or '.'", p)
	case strings.Contains(p, "..") || strings.Contains(p, "//") || strings.Contains(p, "/."):
		return fmt.Errorf("branch prefix %q may not contain '..', '//' or '/.'", p)
	case strings.Contains(p, ".lock/") || strings.HasSuffix(p, "."):
		return fmt.Errorf("branch prefix %q has a part git does not allow", p)
	}
	return nil
}

// normalizeAgentTasks keeps the valid values and defaults the rest.
func normalizeAgentTasks(a AgentTasks) AgentTasks {
	def := defaultAgentTasks()
	if checkWorktreeDir(a.WorktreeDir) != nil {
		a.WorktreeDir = def.WorktreeDir
	}
	if checkBranchPrefix(a.BranchPrefix) != nil {
		a.BranchPrefix = def.BranchPrefix
	}
	return a
}
