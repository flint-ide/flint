package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

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
}

func (e *terminalEvents) Exited(id string, code int) {
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
	e.app.emit(eventTermTitle, TermTitle{ID: id, Info: info})
}

// TerminalAPI runs interactive shells (e.g. for claude / codex) inside Flint.
type TerminalAPI struct {
	app *App
}

// Start launches the user's login shell in cwd (home when empty) with extra
// environment variables (e.g. AWS_PROFILE). The frontend picks the id.
func (t *TerminalAPI) Start(id, cwd string, cols, rows int, env map[string]string) error {
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	vars, err := terminalEnv(env)
	if err != nil {
		return err
	}
	return t.app.terminals.Start(id, terminal.Options{Cwd: cwd, Cols: cols, Rows: rows, Env: vars})
}

var envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// terminalEnv turns the frontend's variables into NAME=value pairs, sorted;
// names must be plain upper-case identifiers and values single-line.
func terminalEnv(env map[string]string) ([]string, error) {
	out := make([]string, 0, len(env))
	for k, v := range env {
		if !envNameRe.MatchString(k) || strings.ContainsAny(v, "\x00\n\r") || len(v) > 1024 {
			return nil, fmt.Errorf("invalid terminal variable %q", k)
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

// Close terminates the terminal's shell.
func (t *TerminalAPI) Close(id string) {
	t.app.terminals.Close(id)
}
