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

	"flint/internal/shellenv"
)

// Kind identifies an agent CLI.
type Kind string

const (
	Claude Kind = "claude"
	Codex  Kind = "codex"
)

var known = []Kind{Claude, Codex}

// Available lists agent CLIs found on the login-shell PATH, in preference order.
func Available() []Kind {
	shellenv.Wait()
	out := []Kind{}
	for _, k := range known {
		if _, err := exec.LookPath(string(k)); err == nil {
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
func Headless(ctx context.Context, k Kind, dir, prompt, input string, untrusted bool) (string, error) {
	shellenv.Wait()
	var cmd *exec.Cmd
	switch k {
	case Claude:
		// `claude -p` prints only the final response; stdin becomes context.
		args := []string{"-p", prompt}
		if untrusted {
			args = append(args, "--setting-sources", "user")
		}
		cmd = exec.CommandContext(ctx, "claude", args...)
		cmd.Stdin = strings.NewReader(input)
	case Codex:
		// `codex exec` runs non-interactively and prints the last message on stdout.
		cmd = exec.CommandContext(ctx, "codex", "exec", prompt+"\n\n"+input)
	default:
		return "", fmt.Errorf("unsupported agent %q", k)
	}
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
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

// InteractiveCommand is the shell command that starts an interactive session of k
// with prompt as the first message, ready to be typed into a terminal.
func InteractiveCommand(k Kind, prompt string) (string, error) {
	if !Valid(k) {
		return "", fmt.Errorf("unsupported agent %q", k)
	}
	return string(k) + " " + ShellQuote(prompt), nil
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
