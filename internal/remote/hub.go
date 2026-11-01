package remote

import (
	"sync"
	"sync/atomic"

	"flint/internal/agentsession"
)

// subBuffer bounds the messages waiting for one stream.
const subBuffer = 1024

type msgKind int

const (
	msgAgent msgKind = iota
	msgTasks
	msgChat
)

type hubMsg struct {
	kind  msgKind
	event agentsession.Event // msgAgent
	id    string             // msgChat
}

// subscriber is one stream's inbox. lost is closed when the inbox overflowed:
// the stream ends and the client replays from its last Seq.
type subscriber struct {
	ch       chan hubMsg
	lost     chan struct{}
	lostOnce sync.Once
}

// hub fans events out to the open streams. publish is called on the desktop
// path (App.emit, under the agent manager's lock), so it never blocks and
// costs one atomic load when no stream is open.
type hub struct {
	n    atomic.Int32
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func newHub() *hub { return &hub{subs: map[*subscriber]struct{}{}} }

func (h *hub) subscribe() *subscriber {
	sub := &subscriber{ch: make(chan hubMsg, subBuffer), lost: make(chan struct{})}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.n.Store(int32(len(h.subs)))
	h.mu.Unlock()
	return sub
}

func (h *hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.n.Store(int32(len(h.subs)))
	h.mu.Unlock()
}

func (h *hub) publish(m hubMsg) {
	if h.n.Load() == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- m:
		default:
			sub.lostOnce.Do(func() { close(sub.lost) })
		}
	}
}
