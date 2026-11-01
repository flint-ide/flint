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
	maxSegments  = 64 // directory changes remembered per console
)

// Terminals keeps the recent raw output of every terminal and run console,
// so an agent can read what the user sees there. Output is kept in segments
// by the directory the shell was in when it was printed, and an agent only
// gets the segments printed inside its project: output from before a `cd`
// into the project stays out. It is safe for concurrent use; Write is on the
// output path and only appends.
type Terminals struct {
	mu    sync.Mutex
	byID  map[string]*console
	order []string // oldest first
}

type console struct {
	kind, title, cwd string
	exited           bool
	code             int
	segs             []segment // oldest first; the last one is being written
	size             int       // bytes in segs
}

// segment is output printed while the shell was in cwd.
type segment struct {
	cwd  string
	data []byte
	cut  bool // its beginning was dropped: it may start mid-line
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

// Open starts keeping output for id; when too many are kept, the oldest
// finished console (else the oldest) is forgotten. Reopening an id starts it
// over.
func (t *Terminals) Open(id, kind, title, cwd string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.openLocked(id, kind, title, cwd)
}

func (t *Terminals) openLocked(id, kind, title, cwd string) {
	cwd = filepath.Clean(cwd)
	t.removeLocked(id)
	if len(t.order) >= maxTerminals {
		victim := t.order[0]
		if i := slices.IndexFunc(t.order, func(x string) bool { return t.byID[x].exited }); i >= 0 {
			victim = t.order[i]
		}
		t.removeLocked(victim)
	}
	t.byID[id] = &console{kind: kind, title: title, cwd: cwd, segs: []segment{{cwd: cwd}}}
	t.order = append(t.order, id)
}

// OpenNew is Open for a process about to start: it refuses (false) when id
// is kept and still running, so a start that fails because id is taken
// doesn't wipe the running console. A finished console is started over.
func (t *Terminals) OpenNew(id, kind, title, cwd string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.byID[id]; c != nil && !c.exited {
		return false
	}
	t.openLocked(id, kind, title, cwd)
	return true
}

// Write appends output of id (unknown ids are ignored).
func (t *Terminals) Write(id string, data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.byID[id]
	if c == nil || len(data) == 0 {
		return
	}
	last := &c.segs[len(c.segs)-1]
	last.data = append(last.data, data...)
	c.size += len(data)
	if c.size > 2*maxTailBytes {
		c.trim(maxTailBytes)
	}
}

// trim drops the oldest output until at most n bytes are kept.
func (c *console) trim(n int) {
	for c.size > n {
		first := &c.segs[0]
		excess := c.size - n
		if len(c.segs) > 1 && len(first.data) <= excess {
			c.size -= len(first.data)
			c.segs = slices.Delete(c.segs, 0, 1)
			continue
		}
		drop := min(excess, len(first.data))
		first.data = slices.Clone(first.data[drop:])
		first.cut = true
		c.size -= drop
	}
}

// SetInfo records what a terminal runs ("" at the prompt) and where its
// shell is; a new directory starts a new segment of output.
func (t *Terminals) SetInfo(id, command, cwd string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.byID[id]
	if c == nil {
		return
	}
	c.title = command
	if !filepath.IsAbs(cwd) {
		return
	}
	cwd = filepath.Clean(cwd)
	if cwd == c.cwd {
		return
	}
	c.cwd = cwd
	if last := c.segs[len(c.segs)-1]; len(last.data) == 0 {
		c.segs[len(c.segs)-1].cwd = cwd
		return
	}
	c.segs = append(c.segs, segment{cwd: cwd})
	if len(c.segs) > maxSegments {
		c.size -= len(c.segs[0].data)
		c.segs = slices.Delete(c.segs, 0, 1)
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

// List returns the consoles that printed something inside root, or whose
// shell is in it now, oldest first.
func (t *Terminals) List(root string) []TerminalInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := []TerminalInfo{}
	for _, id := range t.order {
		c := t.byID[id]
		in := within(c.cwd, root) || slices.ContainsFunc(c.segs, func(s segment) bool { return len(s.data) > 0 && within(s.cwd, root) })
		if in {
			out = append(out, TerminalInfo{ID: id, Kind: c.kind, Title: c.title, Cwd: c.cwd, Exited: c.exited, Code: c.code})
		}
	}
	return out
}

// Tail returns the last lines id printed inside root, as plain text.
func (t *Terminals) Tail(id, root string, lines int) (string, bool) {
	t.mu.Lock()
	c := t.byID[id]
	var parts [][]byte
	if c != nil {
		for _, s := range c.segs {
			if !within(s.cwd, root) || len(s.data) == 0 {
				continue
			}
			data := s.data
			if s.cut {
				// The first line (and maybe an escape sequence) is incomplete.
				if i := bytes.IndexByte(data, '\n'); i >= 0 {
					data = data[i+1:]
				}
			}
			parts = append(parts, slices.Clone(data))
		}
	}
	t.mu.Unlock()
	if c == nil {
		return "", false
	}
	var texts []string
	for _, p := range parts {
		if text := PlainText(p); text != "" {
			texts = append(texts, text)
		}
	}
	return lastLines(joinLines(texts), lines), true
}

func joinLines(texts []string) string {
	var b bytes.Buffer
	for i, s := range texts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}
	return b.String()
}
