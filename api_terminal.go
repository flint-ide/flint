package main

import (
	"errors"
	"os"
	"slices"
	"sync"

	"flint/internal/mcptools"
	"flint/internal/remote"
	"flint/internal/terminal"
)

// Events emitted by running terminals.
const (
	eventTermData  = "term:data"
	eventTermExit  = "term:exit"
	eventTermTitle = "term:title"
)

// TermData carries raw PTY output; Data is base64-encoded by JSON so split UTF-8
// sequences survive until xterm.js reassembles them.
type TermData struct {
	ID   string `json:"id"`
	Seq  uint64 `json:"seq"` // per terminal, from 1; lets the UI restore order
	Data []byte `json:"data"`
}

// TermExit reports that a terminal's shell exited.
type TermExit struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

// terminalEvents forwards terminal output to the frontend.
type terminalEvents struct {
	app *App

	mu  sync.Mutex
	seq map[string]uint64
}

func newTerminalEvents(app *App) *terminalEvents {
	return &terminalEvents{app: app, seq: map[string]uint64{}}
}

func (e *terminalEvents) Output(id string, data []byte) {
	e.mu.Lock()
	defer e.mu.Unlock() // stamp and emit together so sequence order equals send order
	e.seq[id]++
	e.app.emit(eventTermData, TermData{ID: id, Seq: e.seq[id], Data: data})
	e.app.mcpSources.Terminals.Write(id, data)
}

func (e *terminalEvents) Exited(id string, code int) {
	e.app.mcpSources.Terminals.Exit(id, code)
	e.app.emit(eventTermExit, TermExit{ID: id, Code: code})
	e.mu.Lock()
	delete(e.seq, id)
	e.mu.Unlock()
}

// TermTitle reports what a terminal runs, for its tab name.
type TermTitle struct {
	ID string `json:"id"`
	terminal.Info
}

func (e *terminalEvents) Title(id string, info terminal.Info) {
	e.app.mcpSources.Terminals.SetInfo(id, info.Command, info.Cwd)
	e.app.emit(eventTermTitle, TermTitle{ID: id, Info: info})
}

// TerminalAPI runs interactive shells (e.g. for claude / codex) inside Flint.
type TerminalAPI struct {
	app *App
}

// Start launches the user's login shell in cwd (home when empty) with extra
// environment variables (e.g. AWS_PROFILE). The frontend picks the id.
func (t *TerminalAPI) Start(id, cwd string, cols, rows int, env map[string]string) error {
	if remote.IsRemoteID(id) {
		return errors.New("a terminal on a machine starts with StartRemote: no local folder or environment applies there")
	}
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	vars, err := terminalEnv(env)
	if err != nil {
		return err
	}
	// Kept before the shell starts, so its first output is too.
	opened := t.app.mcpSources.Terminals.OpenNew(id, mcptools.KindTerminal, "", cwd)
	if err := t.app.terminals.Start(id, terminal.Options{Cwd: cwd, Cols: cols, Rows: rows, Env: vars}); err != nil {
		if opened {
			t.app.mcpSources.Terminals.Close(id)
		}
		return err
	}
	return nil
}

// terminalEnv turns the frontend's variables into NAME=value pairs, sorted;
// names must be plain upper-case identifiers and values single-line
// (terminal.CheckEnvVar, which the background host applies to its wire too).
func terminalEnv(env map[string]string) ([]string, error) {
	out := make([]string, 0, len(env))
	for k, v := range env {
		if err := terminal.CheckEnvVar(k, v); err != nil {
			return nil, err
		}
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out, nil
}

// Write sends keystrokes / pasted text to the terminal.
func (t *TerminalAPI) Write(id, data string) error {
	return t.app.terminals.Write(id, []byte(data))
}

// Resize updates the terminal grid size.
func (t *TerminalAPI) Resize(id string, cols, rows int) error {
	return t.app.terminals.Resize(id, cols, rows)
}

// Cwd returns the shell's current directory (follows `cd`), "" when unknown.
func (t *TerminalAPI) Cwd(id string) string {
	cwd, err := t.app.terminals.Cwd(id)
	if err != nil {
		return ""
	}
	return cwd
}

// Resume reports the agent CLI (claude, codex) running in the terminal and its
// conversation, so the session can reopen it after a restart; the zero value
// when there is nothing to resume.
func (t *TerminalAPI) Resume(id string) terminal.Resume {
	r, err := t.app.terminals.Resume(id)
	if err != nil {
		return terminal.Resume{}
	}
	return r
}

// Close terminates the terminal's shell.
func (t *TerminalAPI) Close(id string) {
	t.app.terminals.Close(id)
	t.app.mcpSources.Terminals.Close(id)
}
