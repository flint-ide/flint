package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"flint/internal/agenthost/hostmode"
	"flint/internal/extensions/sandbox"
	"flint/internal/feature"
	"flint/internal/runconfig"
)

// A workflow's script step in the sandbox (workflows.sandbox: "on"): every
// process of its run configuration starts through the sandbox launcher
// (Flint itself, sandbox.Main) with the policy of sandbox.BuildScript, the
// mechanism extensions run in. Nothing here decides what is allowed; it
// gathers the folders and wraps the commands.

// probeSandbox is what this system can enforce (tests replace it).
var probeSandbox = sandbox.Probe

// sandboxLauncher is the program that confines a process: Flint's own
// executable (tests replace it).
var sandboxLauncher = sandbox.Launcher

// stepSandbox is the sandbox of one script step.
type stepSandbox struct {
	support  sandbox.Support
	policy   sandbox.Policy
	launcher string
	home     string
}

// stepSandbox builds the sandbox of a script step that runs in worktree. It
// fails, so the step fails and nothing runs unconfined, when this system has
// no sandbox or the worktree cannot be granted.
func (a *App) stepSandbox(worktree string, sb feature.Sandbox) (*stepSandbox, error) {
	s := probeSandbox()
	if !s.Available {
		return nil, fmt.Errorf("the sandbox is not available here (%s): switch off “Run script steps in a sandbox” in Settings ▸ Workflows to run it without one (Flint asks you once per folder)", s.Reason)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("the sandbox cannot run this step: %w", err)
	}
	home = realPath(home)
	// The background host's run directory is derived from the
	// configuration folder as the host sees it, links not resolved.
	runDir := hostmode.RunDir(a.configDir)
	policy, err := sandbox.BuildScript(sandbox.ScriptRequest{
		GOOS: runtime.GOOS, Home: home, ConfigDir: realPath(a.configDir), RunDir: runDir,
		Worktree: worktree, TempDirs: scriptTempDirs(), Writable: sb.Writable, Network: sb.Network,
		Resolve: filepath.EvalSymlinks, ReadDir: sandbox.DirNames,
	})
	if err != nil {
		return nil, fmt.Errorf("the sandbox cannot run this step: %w", err)
	}
	launcher, err := sandboxLauncher()
	if err != nil {
		return nil, fmt.Errorf("the sandbox cannot run this step: %w", err)
	}
	return &stepSandbox{support: s, policy: policy, launcher: launcher, home: home}, nil
}

func realPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// scriptTempDirs are the system's temporary folders that exist: $TMPDIR (or
// its default), /tmp, and on Linux /dev/shm (shared memory, which test
// runners and browsers use like a temporary folder).
func scriptTempDirs() []string {
	dirs := []string{os.TempDir(), "/tmp"}
	if runtime.GOOS == "linux" {
		dirs = append(dirs, "/dev/shm")
	}
	out := dirs[:0]
	for _, d := range dirs {
		if isDir(d) {
			out = append(out, d)
		}
	}
	return out
}

// confine rewrites each command of the launch to start through the sandbox
// launcher, keeping its label, and returns the line the run tab shows first.
func (sb *stepSandbox) confine(l runconfig.Launch) (runconfig.Launch, string, error) {
	steps := make([]runconfig.Command, len(l.Steps))
	for i, c := range l.Steps {
		program, err := programPath(c.Program, l.Cwd)
		if err != nil {
			return runconfig.Launch{}, "", err
		}
		args, env, err := sandbox.Command(sb.launcher, sb.policy, sb.support, append([]string{program}, c.Args...))
		if err != nil {
			return runconfig.Launch{}, "", fmt.Errorf("the sandbox cannot run %s: %w", c.Label, err)
		}
		steps[i] = runconfig.Command{Label: c.Label, Program: args[0], Args: args[1:], Env: append(append([]string{}, c.Env...), env...)}
	}
	out := l
	out.Steps = steps
	return out, sb.banner(), nil
}

// programPath is the absolute path of a command's program, found as the
// runner would find it (exec.Command: PATH for a bare name, the working
// folder for a relative path): the launcher execs it by path.
func programPath(program, cwd string) (string, error) {
	switch {
	case filepath.IsAbs(program):
		return program, nil
	case strings.ContainsRune(program, '/'):
		return filepath.Join(cwd, program), nil
	}
	found, err := exec.LookPath(program)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return "", fmt.Errorf("cannot start %s: %w", program, err)
	}
	if err != nil || !filepath.IsAbs(found) {
		return "", fmt.Errorf("cannot start %s: it is only found in the current folder", program)
	}
	return found, nil
}

// banner says that the step runs sandboxed, where it may write, whether it
// has the network, and what was asked for but not granted.
func (sb *stepSandbox) banner() string {
	p := sb.policy
	var where []string
	for _, w := range p.Write {
		switch {
		case w == p.ExtensionDir:
			where = append(where, "the worktree")
		case strings.HasPrefix(w, "/dev/"):
		default:
			where = append(where, tildePath(w, sb.home))
		}
	}
	network := "no network"
	if p.Network {
		network = "network allowed"
	}
	lines := []string{fmt.Sprintf("Sandboxed (%s, workflows.sandbox): writes only to %s; %s. Keys, credentials, shell history and Flint's folder cannot be read.",
		sb.support.Mechanism, strings.Join(where, ", "), network)}
	if runtime.GOOS == "linux" {
		for _, gap := range sb.support.Gaps {
			lines = append(lines, "sandbox: "+gap)
		}
	}
	for _, note := range p.Notes {
		lines = append(lines, "sandbox: "+note)
	}
	return strings.Join(lines, "\n")
}

func tildePath(p, home string) string {
	if home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
