// Package agent hands work to coding agents (Claude Code, Codex CLI): headless runs
// whose answer Flint uses (commit messages) and interactive sessions started in a
// terminal where the user can follow along and answer questions (merge conflicts).
package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"flint/internal/proctree"
	"flint/internal/redact"
	"flint/internal/shellenv"
)

// Kind identifies an agent CLI.
type Kind string

const (
	Claude Kind = "claude"
	Codex  Kind = "codex"
)

var known = []Kind{Claude, Codex}

// Available lists agent CLIs found on the login-shell PATH (or at their
// configured path), in preference order.
func Available(configs map[Kind]Config) []Kind {
	shellenv.Wait()
	out := []Kind{}
	for _, k := range known {
		if _, err := exec.LookPath(configs[k].Binary(k)); err == nil {
			out = append(out, k)
		}
	}
	return out
}

// Valid reports whether k is a supported agent.
func Valid(k Kind) bool {
	for _, x := range known {
		if x == k {
			return true
		}
	}
	return false
}

// Headless runs a one-shot prompt in dir and returns the agent's final answer.
// input is extra context (e.g. a diff) that is piped or appended to the prompt.
// In an untrusted folder Claude loads only the user's settings: the project's
// hooks and permission rules would otherwise run (-p skips the trust prompt).
// cfg's extra arguments go before Flint's own (see ComposeArgs).
func Headless(ctx context.Context, k Kind, cfg Config, dir, prompt, input string, untrusted bool) (string, error) {
	shellenv.Wait()
	args, stdin, err := headlessArgs(k, cfg, prompt, input, untrusted)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, cfg.Binary(k), args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Dir = dir
	// The run gets its own process group: cancelling kills the CLI and what
	// it started (Claude Code's MCP servers, shells), and so does the end of
	// a run, so nothing outlives it.
	proctree.Isolate(cmd)
	cmd.Cancel = func() error { return proctree.Kill(cmd.Process) }
	// A child that keeps the output pipes open must not hold the run open.
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	_ = proctree.Kill(cmd.Process)
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil // the CLI answered; only a leftover child held the pipes
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%s did not answer in time", k)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s failed: %s", k, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// headlessTools are the only built-in tools of a headless Claude run.
const headlessTools = "Read,Grep,Glob"

// headlessArgs is the command line and the stdin of a headless run. NUL
// bytes are dropped: they can't be passed and mean nothing to a model.
func headlessArgs(k Kind, cfg Config, prompt, input string, untrusted bool) ([]string, string, error) {
	prompt = strings.ReplaceAll(prompt, "\x00", "")
	input = strings.ReplaceAll(input, "\x00", "")
	var flint []string
	stdin := input
	switch k {
	case Claude:
		// `claude -p` prints only the final response; stdin becomes context.
		// The prompt carries repository content that could try to steer the
		// agent, so it gets read-only tools whatever the user's settings
		// allow, may not read credential files, and gets no MCP servers.
		// The deny rules end at the next flag (--disallowedTools takes a list).
		flint = append([]string{"-p", prompt, "--tools", headlessTools, "--disallowedTools"}, HeadlessDenyRules()...)
		flint = append(flint, "--strict-mcp-config")
		if cfg.Model != "" {
			flint = append(flint, "--model", cfg.Model)
		}
		if untrusted {
			flint = append(flint, "--setting-sources", "user")
		}
	case Codex:
		// `codex exec` runs non-interactively and prints the last message on stdout.
		// The prompt carries the repository's diff, so the run may only read,
		// whatever the user's Codex configuration says. "-" reads the prompt
		// from stdin, so a large diff never hits the argument size limit.
		flint = []string{"exec", "--sandbox", "read-only"}
		if cfg.Model != "" {
			flint = append(flint, "--model", cfg.Model)
		}
		flint = append(flint, "-")
		stdin = prompt + "\n\n" + input
	default:
		return nil, "", fmt.Errorf("unsupported agent %q", k)
	}
	if err := checkConfig(cfg); err != nil {
		return nil, "", fmt.Errorf("%s: %w", k, err)
	}
	args, err := ComposeArgs(k, cfg.Args, flint, untrusted)
	return args, stdin, err
}

// homeSecrets are credential folders and files outside any project.
var homeSecrets = []string{"~/.ssh/**", "~/.aws/**", "~/.kube/**", "~/.gnupg/**", "~/.config/gcloud/**", "~/.azure/**", "~/.docker/config.json", "~/.netrc", "~/.git-credentials"}

// HeadlessDenyRules are Claude Code permission rules that keep a headless run
// out of credential files: the ones redact.SecretFile recognises anywhere in
// the project, and the usual credential folders in the home directory.
// Claude Code applies Read rules to Grep and Glob as well.
func HeadlessDenyRules() []string {
	globs := redact.SecretFileGlobs()
	rules := make([]string, 0, len(globs)+len(homeSecrets))
	for _, g := range globs {
		rules = append(rules, "Read(**/"+g+")")
	}
	for _, h := range homeSecrets {
		rules = append(rules, "Read("+h+")")
	}
	return rules
}

// InteractiveCommand is the shell command that starts an interactive session of k
// with prompt as the first message, ready to be typed into a terminal. The
// prompt comes before the extra arguments, so an option taking several values
// (--add-dir a b) cannot swallow it; in Restricted Mode they are left out, as
// for every agent run.
func InteractiveCommand(k Kind, cfg Config, prompt string, untrusted bool) (string, error) {
	if !Valid(k) {
		return "", fmt.Errorf("unsupported agent %q", k)
	}
	if err := checkConfig(cfg); err != nil {
		return "", fmt.Errorf("%s: %w", k, err)
	}
	var args []string
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	args = append(args, prompt)
	if !untrusted {
		if err := CheckArgs(k, cfg.Args); err != nil {
			return "", fmt.Errorf("%s extra arguments: %w", k, err)
		}
		args = append(args, cfg.Args...)
	}
	parts := []string{string(k)}
	if cfg.Path != "" {
		parts[0] = ShellQuote(cfg.Path)
	}
	for _, a := range args {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " "), nil
}

// checkConfig validates the path and the model; the arguments are checked
// where they are composed.
func checkConfig(cfg Config) error {
	if err := CheckPath(cfg.Path); err != nil {
		return err
	}
	return CheckModel(cfg.Model)
}

// ShellQuote quotes s for POSIX shells (single quotes, embedded ones escaped).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CleanCommitMessage strips wrappers agents sometimes add despite instructions:
// surrounding code fences, quotes, and a "Commit message:" preamble.
func CleanCommitMessage(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if nl := strings.IndexByte(s, '\n'); nl >= 0 && !strings.Contains(s[:nl], " ") {
			s = s[nl+1:] // drop a language tag like ```text
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	for _, p := range []string{"Commit message:", "commit message:"} {
		s = strings.TrimPrefix(strings.TrimSpace(s), p)
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}
