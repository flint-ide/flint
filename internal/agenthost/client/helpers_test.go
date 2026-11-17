package client_test

import (
	"context"
	"fmt"
	"hash/crc32"
	"strings"
	"sync"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/terminal"
)

const wait = 10 * time.Second

// termRecorder is the terminal.Events of a test: it keeps what arrives.
type termRecorder struct {
	mu      sync.Mutex
	out     map[string]*strings.Builder
	exits   map[string]int
	titles  map[string]terminal.Info
	agents  map[string]string
	changed chan struct{}
}

func newTermRecorder() *termRecorder {
	return &termRecorder{
		out: map[string]*strings.Builder{}, exits: map[string]int{}, titles: map[string]terminal.Info{}, agents: map[string]string{},
		changed: make(chan struct{}, 1),
	}
}

func (r *termRecorder) poke() {
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *termRecorder) Output(id string, data []byte) {
	r.mu.Lock()
	if r.out[id] == nil {
		r.out[id] = &strings.Builder{}
	}
	r.out[id].Write(data)
	r.mu.Unlock()
	r.poke()
}

func (r *termRecorder) Exited(id string, code int) {
	r.mu.Lock()
	r.exits[id] = code
	r.mu.Unlock()
	r.poke()
}

func (r *termRecorder) Title(id string, info terminal.Info) {
	r.mu.Lock()
	r.titles[id] = info
	r.mu.Unlock()
	r.poke()
}

func (r *termRecorder) AgentState(id, state string) {
	r.mu.Lock()
	r.agents[id] = state
	r.mu.Unlock()
	r.poke()
}

func (r *termRecorder) output(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b := r.out[id]; b != nil {
		return b.String()
	}
	return ""
}

// until waits for cond, re-checking whenever something arrives.
func (r *termRecorder) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(wait)
	for !cond() {
		select {
		case <-r.changed:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (r *termRecorder) waitOutput(t *testing.T, id, substr string) {
	t.Helper()
	r.until(t, fmt.Sprintf("%q in the output of %s (have %q)", substr, id, r.output(id)), func() bool {
		return strings.Contains(r.output(id), substr)
	})
}

func (r *termRecorder) waitExit(t *testing.T, id string) int {
	t.Helper()
	var code int
	r.until(t, "the exit of "+id, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		c, ok := r.exits[id]
		code = c
		return ok
	})
	return code
}

// fakeDriver is an agent backend that answers prompts without any process.
// A prompt starting with "ask" requests a permission and waits for the answer;
// one starting with "block" waits until cancelled or released.
type fakeDriver struct {
	mu       sync.Mutex
	sinks    map[string]as.Sink
	n        int
	answered chan string // the options Respond received
	unblock  chan struct{}
	stopped  chan string
	release  chan struct{}
	cancel   chan struct{}
}

func newFakeDriver() *fakeDriver {
	return &fakeDriver{
		sinks: map[string]as.Sink{}, answered: make(chan string, 16), unblock: make(chan struct{}, 16), stopped: make(chan string, 16),
		release: make(chan struct{}), cancel: make(chan struct{}, 1),
	}
}

func (d *fakeDriver) open(cwd, id string, sink as.Sink) (as.SessionInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if id == "" {
		d.n++
		id = fmt.Sprint("chat-", d.n)
	}
	d.sinks[id] = sink
	return as.SessionInfo{ID: id, Agent: "fake", Model: "m", Cwd: cwd}, nil
}

func (d *fakeDriver) Start(_ context.Context, cwd string, sink as.Sink) (as.SessionInfo, error) {
	return d.open(cwd, "", sink)
}

func (d *fakeDriver) Resume(_ context.Context, cwd, id string, sink as.Sink) (as.SessionInfo, error) {
	if id == "gone" {
		return as.SessionInfo{}, fmt.Errorf("no conversation %q", id)
	}
	return d.open(cwd, id, sink)
}

func (d *fakeDriver) sink(id string) as.Sink {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sinks[id]
}

func (d *fakeDriver) Prompt(ctx context.Context, id, text string, _ []as.Attachment) (string, error) {
	sink := d.sink(id)
	if strings.HasPrefix(text, "size") {
		// What arrived, in a form that survives the trip back: a big prompt
		// cannot be echoed (an event has a size limit too).
		sink(as.Event{SessionID: id, Kind: as.KindMessageChunk, Text: fmt.Sprintf("len:%d crc:%08x", len(text), crc32.ChecksumIEEE([]byte(text)))})
		sink(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
		return as.StopEndTurn, nil
	}
	sink(as.Event{SessionID: id, Kind: as.KindMessageChunk, Text: "echo:" + text})
	switch {
	case strings.HasPrefix(text, "ask"):
		sink(as.Event{SessionID: id, Kind: as.KindPermission, Permission: &as.PermissionRequest{
			ID: "req1", ToolCall: as.ToolCall{ID: "tc1", Kind: as.ToolExecute, Title: "go test"},
			Options: []as.PermissionOption{{ID: "yes", Name: "Allow", Kind: as.AllowOnce}, {ID: "no", Name: "Deny", Kind: as.RejectOnce}},
		}})
		select {
		case <-d.unblock:
		case <-ctx.Done():
		case <-time.After(wait):
		}
	case strings.HasPrefix(text, "block"):
		select {
		case <-d.release:
		case <-d.cancel:
			sink(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopCancelled})
			return as.StopCancelled, nil
		case <-ctx.Done():
			return as.StopCancelled, nil
		}
	}
	sink(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	return as.StopEndTurn, nil
}

func (d *fakeDriver) Cancel(string) error {
	select {
	case d.cancel <- struct{}{}:
	default:
	}
	return nil
}

// Respond reports the chosen option to the test and unblocks the prompt.
func (d *fakeDriver) Respond(_, _, option string) error {
	d.answered <- option
	d.unblock <- struct{}{}
	return nil
}

func (d *fakeDriver) Stop(id string) error {
	d.stopped <- id
	return nil
}

// Close stops every session, as a driver's Close does.
func (d *fakeDriver) Close() error {
	d.mu.Lock()
	ids := make([]string, 0, len(d.sinks))
	for id := range d.sinks {
		ids = append(ids, id)
	}
	d.mu.Unlock()
	for _, id := range ids {
		d.stopped <- id
	}
	return nil
}
