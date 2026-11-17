package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"flint/internal/agenthooks"
)

// ClaudeCommand makes sure Flint's Claude Code hooks file exists and returns
// the command line that starts Claude with it, for the "New Claude Terminal"
// command to type into a terminal opened in folder. The file is written only
// for a folder of the window that is trusted: Restricted Mode starts nothing
// on its own, and a hooks file is code that runs when the agent does. It is
// the only file written: the user's own agent settings are never touched.
func (t *TerminalAPI) ClaudeCommand(folder string) (string, error) {
	a := t.app
	if folder == "" {
		return "", errors.New("no folder is open")
	}
	if _, err := a.rootOf(folder); err != nil {
		return "", err
	}
	if !a.isTrusted(folder) {
		return "", fmt.Errorf("%s is not trusted: trust the folder first (Restricted Mode starts no agent hooks)", filepath.Base(folder))
	}
	path, err := agenthooks.Ensure(a.configDir)
	if err != nil {
		return "", err
	}
	return agenthooks.ClaudeCommand(path)
}

// HookSnippet is the Claude Code settings fragment that reports an agent's
// state to Flint's terminal tabs, for a user who wants it in their own
// configuration. Flint only offers it; nothing is written for it.
func (t *TerminalAPI) HookSnippet() string { return string(agenthooks.Snippet()) }

// hooksFileExists tells tests whether the hooks file was written.
func hooksFileExists(configDir string) bool {
	_, err := os.Stat(filepath.Join(configDir, agenthooks.Dir, agenthooks.File))
	return err == nil
}
