package mcptools

import (
	"bytes"
	"path/filepath"
	"slices"
	"sync"
)

// Kinds of consoles kept by Terminals.
const (
	KindTerminal = "terminal" // an interactive shell
	KindRun      = "run"      // a run configuration's or test run's console
)

// Bounds of the kept output: the raw tail per console (twice that before
// it is trimmed) and how many consoles are remembered.
const (
	maxTailBytes = 96 << 10
	maxTerminals = 64
)

// Terminals keeps the recent raw output of every terminal and run console,
// so an agent can read what the user sees there. It is safe for concurrent
// use; Write is on the output path and only appends.
type Terminals struct {
	mu    sync.Mutex
	byID  map[string]*console
	order []string // oldest first
}

type console struct {
	kind, title, cwd string
	exited           bool
	code             int
	buf              []byte
	cut              bool // older output was dropped: buf may start mid-line
}

// TerminalInfo describes one console of a project.
type TerminalInfo struct {
	ID     string
	Kind   string
	Title  string // the running command, or the run's name
	Cwd    string
	Exited bool
	Code   int
}

// NewTerminals returns an empty store.
func NewTerminals() *Terminals { return &Terminals{byID: map[string]*console{}} }

// Open starts keeping output for id; the oldest console is forgotten when
// too many are open. Reopening an id starts it over.
func (t *Terminals) Open(id, kind, title, cwd string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeLocked(id)
	if len(t.order) >= maxTerminals {
		t.removeLocked(t.order[0])
	}
	t.byID[id] = &console{kind: kind, title: title, cwd: filepath.Clean(cwd)}
	t.order = append(t.order, id)
}

// Write appends output of id (unknown ids are ignored).
func (t *Terminals) Write(id string, data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.byID[id]
	if c == nil {
		return
	}
	c.buf = append(c.buf, data...)
	if len(c.buf) > 2*maxTailBytes {
		c.buf = slices.Clone(c.buf[len(c.buf)-maxTailBytes:])
		c.cut = true
	}
}

// SetInfo records what a terminal runs ("" at the prompt) and where its shell is.
func (t *Terminals) SetInfo(id, command, cwd string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.byID[id]; c != nil {
		c.title = command
		if filepath.IsAbs(cwd) {
			c.cwd = filepath.Clean(cwd)
		}
	}
}

// Exit records that id's process ended; its output stays readable.
func (t *Terminals) Exit(id string, code int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.byID[id]; c != nil {
		c.exited, c.code = true, code
	}
}

// Close forgets id.
func (t *Terminals) Close(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeLocked(id)
}

func (t *Terminals) removeLocked(id string) {
	if _, ok := t.byID[id]; !ok {
		return
	}
	delete(t.byID, id)
	t.order = slices.DeleteFunc(t.order, func(x string) bool { return x == id })
}

// List returns the consoles working in root (their directory is in it),
// oldest first.
func (t *Terminals) List(root string) []TerminalInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := []TerminalInfo{}
	for _, id := range t.order {
		c := t.byID[id]
		if within(c.cwd, root) {
			out = append(out, TerminalInfo{ID: id, Kind: c.kind, Title: c.title, Cwd: c.cwd, Exited: c.exited, Code: c.code})
		}
	}
	return out
}

// Tail returns the last lines of id's output as plain text.
func (t *Terminals) Tail(id string, lines int) (string, bool) {
	t.mu.Lock()
	c := t.byID[id]
	var raw []byte
	var wasCut bool
	if c != nil {
		raw, wasCut = slices.Clone(c.buf), c.cut
	}
	t.mu.Unlock()
	if c == nil {
		return "", false
	}
	if len(raw) > maxTailBytes {
		raw, wasCut = raw[len(raw)-maxTailBytes:], true
	}
	if wasCut {
		// The first line (and maybe an escape sequence) is incomplete.
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		}
	}
	return lastLines(PlainText(raw), lines), true
}
