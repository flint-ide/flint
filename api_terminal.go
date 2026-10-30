package main

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"flint/internal/terminal"
)

// Events emitted by running terminals.
const (
	eventTermData = "term:data"
	eventTermExit = "term:exit"
)

// TermData carries raw PTY output; Data is base64-encoded by JSON so split UTF-8
// sequences survive until xterm.js reassembles them.
type TermData struct {
	ID   string `json:"id"`
	Data []byte `json:"data"`
}

// TermExit reports that a terminal's shell exited.
type TermExit struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

// terminalEvents forwards terminal output to the frontend.
type terminalEvents struct{ app *App }

func (e terminalEvents) Output(id string, data []byte) {
	runtime.EventsEmit(e.app.ctx, eventTermData, TermData{ID: id, Data: data})
}

func (e terminalEvents) Exited(id string, code int) {
	runtime.EventsEmit(e.app.ctx, eventTermExit, TermExit{ID: id, Code: code})
}

// TerminalAPI runs interactive shells (e.g. for claude / codex) inside Flint.
type TerminalAPI struct {
	app *App
}

// Start launches the user's login shell in cwd (home when empty). The frontend picks the id.
func (t *TerminalAPI) Start(id, cwd string, cols, rows int) error {
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	return t.app.terminals.Start(id, terminal.Options{Cwd: cwd, Cols: cols, Rows: rows})
}

// Write sends keystrokes / pasted text to the terminal.
func (t *TerminalAPI) Write(id, data string) error {
	return t.app.terminals.Write(id, []byte(data))
}

// Resize updates the terminal grid size.
func (t *TerminalAPI) Resize(id string, cols, rows int) error {
	return t.app.terminals.Resize(id, cols, rows)
}

// Close terminates the terminal's shell.
func (t *TerminalAPI) Close(id string) {
	t.app.terminals.Close(id)
}
