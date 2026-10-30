package lsp

import (
	"sync"
	"testing"
)

// stalledOutbox queues without a writer, like a server that stopped reading.
func stalledOutbox() *outbox {
	o := &outbox{}
	o.cond = sync.NewCond(&o.mu)
	return o
}

func TestQueuedChangeIsReplacedByNewerOne(t *testing.T) {
	o := stalledOutbox()
	for i := range 50 {
		o.notifyLatest("change:/a.go", "textDocument/didChange", i)
	}
	o.notify("textDocument/didSave", nil)
	o.notifyLatest("change:/b.go", "textDocument/didChange", "b")
	o.notifyLatest("change:/a.go", "textDocument/didChange", "last")
	if len(o.queue) != 3 || o.queue[0].method != "textDocument/didSave" || o.queue[2].params != "last" {
		t.Fatalf("queue = %+v, want didSave, b, latest a", o.queue)
	}
}

func TestChangeBeforeBarrierIsKept(t *testing.T) {
	o := stalledOutbox()
	o.notifyLatest("change:/a.go", "textDocument/didChange", 1)
	o.push(outMsg{done: make(chan struct{})}) // a request waits for change 1
	o.notifyLatest("change:/a.go", "textDocument/didChange", 2)
	if len(o.queue) != 3 {
		t.Fatalf("queue = %+v, the change ahead of the barrier must stay", o.queue)
	}
}

func TestClosedOutboxDropsMessages(t *testing.T) {
	o := stalledOutbox()
	o.closed = true
	o.notify("textDocument/didChange", nil)
	if len(o.queue) != 0 {
		t.Fatalf("queue = %+v after close", o.queue)
	}
}
