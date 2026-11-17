// Package agenthooks makes the Claude Code settings file that lets a Claude
// session started by Flint report what it is doing, and the pieces around it.
//
// Flint never edits the user's own agent configuration (~/.claude/settings.json
// or any other file the user owns). The hooks live in a file of Flint's own,
// which Flint passes to the one `claude` it starts itself ("New Claude
// Terminal"), and in a snippet the user can choose to copy into their own
// settings.
package agenthooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"flint/internal/config"
)

// Dir is the folder under Flint's config directory that holds the files.
const Dir = "agent-hooks"

// File is the settings file Flint writes for Claude Code.
const File = "claude-status.json"

// hookCommand prints Flint's status sequence (internal/terminal/agentstate.go)
// to the controlling terminal. A hook's stdout is captured by the agent (and,
// for some events, added to its context), so the sequence cannot go there: it
// goes to /dev/tty, which a hook, a child of the program in the terminal, can
// open. With no terminal it does nothing and exits 0, so it never blocks or
// fails the agent.
func hookCommand(state string) string {
	return `{ printf '\033]6977;state=` + state + `\007' > /dev/tty; } 2>/dev/null || true`
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hookGroup struct {
	// Matcher selects tools for the tool events; "*" is all of them.
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

func group(matcher, state string) []hookGroup {
	return []hookGroup{{Matcher: matcher, Hooks: []hookEntry{{Type: "command", Command: hookCommand(state)}}}}
}

// ClaudeSettings is the content of the settings file: a prompt or a tool call
// means working, a notification (a permission request, a wait for input) means
// waiting, the end of a turn means done. The output is deterministic, so an
// up-to-date file can be recognised by comparing bytes.
func ClaudeSettings() []byte {
	settings := map[string]map[string][]hookGroup{"hooks": {
		"UserPromptSubmit": group("", "working"),
		"PreToolUse":       group("*", "working"),
		"Notification":     group("", "waiting"),
		"Stop":             group("", "done"),
	}}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil { // plain strings and structs: cannot happen
		panic(fmt.Sprintf("agenthooks: %v", err))
	}
	return append(out, '\n')
}

// Snippet is what "Copy hook snippet" puts on the clipboard: the same hooks,
// for a user who prefers to merge them into their own Claude Code settings.
func Snippet() []byte { return ClaudeSettings() }

// Ensure writes <configDir>/agent-hooks/claude-status.json when it is missing
// or differs from what this version of Flint wants, and returns its path. It
// touches that one file only.
func Ensure(configDir string) (string, error) {
	if configDir == "" {
		return "", errors.New("no config directory")
	}
	dir := filepath.Join(configDir, Dir)
	path := filepath.Join(dir, File)
	want := ClaudeSettings()
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	if err := config.WriteFileAtomic(path, want, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// QuoteShell quotes s for a POSIX shell: single quotes, with each embedded
// single quote written as '\”. Nothing inside single quotes is interpreted.
func QuoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ClaudeCommand is the line that starts Claude Code with the settings file at
// path (an absolute path without control characters).
func ClaudeCommand(path string) (string, error) {
	switch {
	case !filepath.IsAbs(path):
		return "", errors.New("the hooks file path must be absolute")
	case strings.ContainsFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return "", errors.New("the hooks file path contains a control character")
	}
	return "claude --settings " + QuoteShell(path), nil
}
