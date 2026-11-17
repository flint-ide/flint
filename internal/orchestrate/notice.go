package orchestrate

import (
	"fmt"
	"strings"
	"sync"

	"flint/internal/agenttask"
)

// Notices keep what a parent chat has not been told yet about the agents it
// started. The rule lives here, pure and table-tested; delivering the notices
// (when the parent is idle, with no draft open) is the app's job.

// Limits: constants, not settings.
const (
	// MaxQueuedNotices is how many notices one parent may have waiting; the
	// oldest go first, so a parent that is busy for a long time is told about
	// the latest changes.
	MaxQueuedNotices = 8
	// NoticeLines is how many of a child's last lines a notice carries.
	NoticeLines = 20
	// MaxNoticeBytes bounds one notice's output text (the end is kept).
	MaxNoticeBytes = 8 << 10
)

// ShouldNotify reports whether a child's change from prev to next is worth a
// notice to its parent: only a change, and only into a state the parent acts on
// (waiting, failed, finished). noticeTurn is true when the turn that just ended
// was started by delivering a notice to that child: its end does not notify
// (waiting still does, the user may need to know). Without that rule a parent
// reacting to a notice, and a child reacting to its own, could set each other
// off for ever.
func ShouldNotify(prev, next string, noticeTurn bool) bool {
	if prev == next {
		return false
	}
	switch next {
	case agenttask.Waiting:
		return true
	case agenttask.Failed, agenttask.Finished:
		return !noticeTurn
	}
	return false
}

// Notice is one child's news for its parent.
type Notice struct {
	Child, Role, State string
	// Output is the child's last lines, already redacted.
	Output string
}

// String is the text the parent receives.
func (n Notice) String() string {
	role := n.Role
	if role == "" {
		role = "no role"
	}
	out := strings.TrimRight(n.Output, "\n")
	if out == "" {
		out = "(no output)"
	}
	if len(out) > MaxNoticeBytes {
		cut := len(out) - MaxNoticeBytes
		for cut < len(out) && out[cut]&0xC0 == 0x80 { // do not start inside a rune
			cut++
		}
		out = "…" + out[cut:]
	}
	return fmt.Sprintf("[Flint] child %s (%s) is %s:\n%s", n.Child, role, n.State, out)
}

// Compose joins notices into one message, so a parent that was busy gets one
// turn out of them instead of one per child.
func Compose(ns []Notice) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = n.String()
	}
	return strings.Join(parts, "\n\n")
}

// Notices is the set of queues, one per parent. The zero value is ready to
// use; it is safe for concurrent use.
type Notices struct {
	mu     sync.Mutex
	queues map[string][]Notice
	turns  map[string]bool // chats whose running turn a notice started
}

// Add queues n for parent. A newer notice about the same child replaces the
// older one (and takes the last place), and at most MaxQueuedNotices wait.
func (q *Notices) Add(parent string, n Notice) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.add(parent, n)
}

func (q *Notices) add(parent string, n Notice) {
	if q.queues == nil {
		q.queues = map[string][]Notice{}
	}
	list := q.queues[parent][:0:0]
	for _, old := range q.queues[parent] {
		if old.Child != n.Child {
			list = append(list, old)
		}
	}
	list = append(list, n)
	if len(list) > MaxQueuedNotices {
		list = list[len(list)-MaxQueuedNotices:]
	}
	q.queues[parent] = list
}

// Requeue puts notices that could not be delivered back, oldest first, without
// overriding a newer notice about the same child.
func (q *Notices) Requeue(parent string, ns []Notice) {
	q.mu.Lock()
	defer q.mu.Unlock()
	newer := map[string]bool{}
	for _, n := range q.queues[parent] {
		newer[n.Child] = true
	}
	current := q.queues[parent]
	delete(q.queues, parent)
	for _, n := range ns {
		if !newer[n.Child] {
			q.add(parent, n)
		}
	}
	for _, n := range current {
		q.add(parent, n)
	}
}

// Take empties and returns parent's queue, oldest first.
func (q *Notices) Take(parent string) []Notice {
	q.mu.Lock()
	defer q.mu.Unlock()
	list := q.queues[parent]
	delete(q.queues, parent)
	return list
}

// Pending reports whether parent has notices waiting.
func (q *Notices) Pending(parent string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[parent]) > 0
}

// Drop forgets everything about a chat that went away: its queue and whether
// a notice started its turn.
func (q *Notices) Drop(chat string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.queues, chat)
	delete(q.turns, chat)
}

// MarkTurn records that the turn just started in chat was started by a notice.
func (q *Notices) MarkTurn(chat string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.turns == nil {
		q.turns = map[string]bool{}
	}
	q.turns[chat] = true
}

// EndTurn reports whether the turn that just ended in chat was started by a
// notice, and forgets it.
func (q *Notices) EndTurn(chat string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	was := q.turns[chat]
	delete(q.turns, chat)
	return was
}
