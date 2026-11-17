// Package orchestrate keeps what agent-to-agent orchestration needs to know
// about every chat: a tree of who started whom, each chat's state (derived
// from its session events by agenttask.Next, never re-derived here), a bounded
// ring of its latest output, and waiters that are woken when a chat reaches a
// state. It is pure: no I/O, no Wails, safe for concurrent use.
package orchestrate

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/redact"
)

// Limits: constants, not settings.
const (
	maxNodes   = 1024 // registered chats
	maxWaiters = 32   // blocked Wait calls per chat
)

var (
	// ErrUnknown is returned for an id that is not registered.
	ErrUnknown = errors.New("orchestrate: unknown agent")
	// ErrRemoved is returned to a Wait whose agent was removed meanwhile.
	ErrRemoved = errors.New("orchestrate: agent was removed")
	// ErrTooManyWaiters is returned when a chat already has maxWaiters waiters.
	ErrTooManyWaiters = errors.New("orchestrate: too many waiters on one agent")
	// ErrFull is returned by Register when maxNodes chats are registered.
	ErrFull = errors.New("orchestrate: too many agents")
)

// Node is one chat in the tree.
type Node struct {
	ID    string
	Agent string
	Role  string
	// Parent is the id of the chat that started this one, "" for a chat the
	// user opened.
	Parent string
	Cwd    string
	// Depth is 0 for a root and the parent's depth plus one below it; Register
	// computes it when the parent is registered.
	Depth int
	// Live is the chat's state, folded from its events.
	Live agenttask.Live
	// Draft reports that the user has an unfinished message in the chat's
	// input (set by the UI through SetDraft).
	Draft bool
}

type entry struct {
	node    Node
	seq     int // registration order, for stable Children
	out     ring
	waiters map[*waiter]struct{}
}

// waiter is one blocked Wait; res is buffered so a wake-up never blocks.
type waiter struct {
	until []string
	res   chan waitResult
}

type waitResult struct {
	live agenttask.Live
	err  error
}

// Registry is the set of known chats. The zero value is not usable; call New.
type Registry struct {
	mu    sync.Mutex
	nodes map[string]*entry
	seq   int
}

// New returns an empty registry.
func New() *Registry { return &Registry{nodes: map[string]*entry{}} }

// Register adds a chat. Its state starts as idle. When n.Parent is registered
// the depth is the parent's plus one, otherwise n.Depth is kept.
func (r *Registry) Register(n Node) error {
	if n.ID == "" {
		return errors.New("orchestrate: empty id")
	}
	if n.Parent == n.ID {
		return fmt.Errorf("orchestrate: %s cannot be its own parent", n.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nodes[n.ID] != nil {
		return fmt.Errorf("orchestrate: %s is already registered", n.ID)
	}
	if len(r.nodes) >= maxNodes {
		return ErrFull
	}
	if p := r.nodes[n.Parent]; p != nil {
		n.Depth = p.node.Depth + 1
	}
	n.Live = agenttask.Live{State: agenttask.Idle}
	r.seq++
	r.nodes[n.ID] = &entry{node: n, seq: r.seq, waiters: map[*waiter]struct{}{}}
	return nil
}

// Remove forgets a chat and fails the Waits on it with ErrRemoved. Its
// children keep their Parent link but Descendant no longer sees through it.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.nodes[id]
	if e == nil {
		return
	}
	delete(r.nodes, id)
	for w := range e.waiters {
		w.res <- waitResult{err: ErrRemoved}
	}
	e.waiters = nil
}

// Get returns a copy of a node.
func (r *Registry) Get(id string) (Node, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.nodes[id]; e != nil {
		return e.node, true
	}
	return Node{}, false
}

// Children returns the chats whose Parent is id, in registration order.
func (r *Registry) Children(id string) []Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []*entry
	for _, e := range r.nodes {
		if e.node.Parent == id && id != "" {
			found = append(found, e)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].seq < found[j].seq })
	out := make([]Node, len(found))
	for i, e := range found {
		out[i] = e.node
	}
	return out
}

// All returns a copy of every node, in registration order.
func (r *Registry) All() []Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := make([]*entry, 0, len(r.nodes))
	for _, e := range r.nodes {
		found = append(found, e)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].seq < found[j].seq })
	out := make([]Node, len(found))
	for i, e := range found {
		out[i] = e.node
	}
	return out
}

// Descendant reports whether id is below ancestor in the tree (a chat is not
// its own descendant).
func (r *Registry) Descendant(ancestor, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Bounded by the registry's size, so even a corrupt cycle ends.
	for i := 0; i <= len(r.nodes); i++ {
		e := r.nodes[id]
		if e == nil || e.node.Parent == "" {
			return false
		}
		if e.node.Parent == ancestor {
			return true
		}
		id = e.node.Parent
	}
	return false
}

// SetDraft records whether the user has an unfinished message in the chat.
func (r *Registry) SetDraft(id string, open bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.nodes[id]; e != nil {
		e.node.Draft = open
	}
}

// Prompted marks the start of a turn the user (or an agent) began: the state
// machine only counts work done inside a turn.
func (r *Registry) Prompted(id string) {
	r.update(id, agenttask.Prompted)
}

// Observe folds one session event into its chat. Events of chats that are not
// registered are ignored, so the registry never grows from a stream it was not
// told about.
func (r *Registry) Observe(e as.Event) {
	r.ObserveChange(e)
}

// ObserveChange is Observe that also returns the chat as it is now and the
// state it was in before the event, so a caller can act on a change without
// reading the registry twice (and racing another event). ok is false for a chat
// that is not registered.
func (r *Registry) ObserveChange(e as.Event) (n Node, prev string, ok bool) {
	return r.update(e.SessionID, func(l agenttask.Live) agenttask.Live { return agenttask.Next(l, e) }, e)
}

// update applies f to a chat's state and wakes the waiters that now match.
// events, if any, also feed the output ring. It returns the node after the
// change and the state before it.
func (r *Registry) update(id string, f func(agenttask.Live) agenttask.Live, events ...as.Event) (Node, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.nodes[id]
	if e == nil {
		return Node{}, "", false
	}
	prev := e.node.Live.State
	for _, ev := range events {
		if e.node.Live.Turn { // like Next: replayed history is not output
			e.record(ev)
		}
	}
	e.node.Live = f(e.node.Live)
	for w := range e.waiters {
		if contains(w.until, e.node.Live.State) {
			w.res <- waitResult{live: e.node.Live}
			delete(e.waiters, w)
		}
	}
	return e.node, prev, true
}

// record puts what an event says into the output ring.
func (e *entry) record(ev as.Event) {
	switch ev.Kind {
	case as.KindMessageChunk:
		e.out.addText(ev.Text)
	case as.KindToolCall:
		if ev.ToolCall == nil {
			return
		}
		title := ev.ToolCall.Title
		if title == "" {
			title = ev.ToolCall.Kind
		}
		if title != "" {
			e.out.addLine("[tool] " + title)
		}
	}
}

// Output returns the last lines of what the chat said and did (assistant text
// and tool-call titles), oldest first; lines <= 0 means everything kept. The
// text is redacted: an agent's output may contain what it read.
func (r *Registry) Output(id string, lines int) string {
	r.mu.Lock()
	e := r.nodes[id]
	var s string
	if e != nil {
		s = e.out.last(lines)
	}
	r.mu.Unlock()
	return redact.Secrets(s)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
