package agenthost

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/jsonrpc"
	"flint/internal/terminal"
)

func closedWithin(t *testing.T, p *peer) {
	t.Helper()
	select {
	case <-p.conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the host did not close the connection")
	}
}

func TestNewValidatesTheConfig(t *testing.T) {
	if _, err := New(Config{Token: "short"}); err == nil {
		t.Error("a short token was accepted")
	}
	s, err := New(Config{Token: testToken, ReplayKiB: 1 << 20})
	if err != nil || s.replayKiB.Load() != MaxReplayKiB {
		t.Errorf("replay = %d, %v; want it capped at %d", s.replayKiB.Load(), err, MaxReplayKiB)
	}
	s, _ = New(Config{Token: testToken})
	if s.replayKiB.Load() != DefaultReplayKiB {
		t.Errorf("default replay = %d", s.replayKiB.Load())
	}
}

// host.settingsChanged reaches the host mode's hook; a changed replay size
// applies to terminals started afterwards only.
func TestSettingsChangedAndReplaySize(t *testing.T) {
	var calls atomic.Int32
	r := newRig(t, func(c *Config) { c.OnSettingsChanged = func() { calls.Add(1) } })
	p := r.connect(t)
	if err := p.call(t, MethodHostSettingsChanged, struct{}{}, nil); err != nil || calls.Load() != 1 {
		t.Fatalf("settingsChanged = %v, hook calls %d", err, calls.Load())
	}
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "old"}, nil); err != nil {
		t.Fatal(err)
	}
	r.srv.SetReplayKiB(1)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "new"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.srv.term("old").ring.maxBytes; got != DefaultReplayKiB<<10 {
		t.Errorf("old ring = %d bytes", got)
	}
	if got := r.srv.term("new").ring.maxBytes; got != 1<<10 {
		t.Errorf("new ring = %d bytes", got)
	}
	r.srv.SetReplayKiB(0)
	if r.srv.replayKiB.Load() != DefaultReplayKiB {
		t.Errorf("SetReplayKiB(0) = %d", r.srv.replayKiB.Load())
	}
}

// term.list reports what the host holds now, exited shells included and
// closed terminals left out.
func TestTermList(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for _, id := range []string{"b", "a", "c"} {
		if err := p.call(t, MethodTermStart, TermStartParams{ID: id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.terms.events.Exited("b", 0)
	if err := p.call(t, MethodTermClose, IDParams{ID: "c"}, nil); err != nil {
		t.Fatal(err)
	}
	var res TermListResult
	if err := p.call(t, MethodTermList, struct{}{}, &res); err != nil {
		t.Fatal(err)
	}
	want := []TermStatus{{ID: "a"}, {ID: "b", Exited: true}}
	if !slices.Equal(res.Terminals, want) {
		t.Errorf("term.list = %+v, want %+v", res.Terminals, want)
	}
}

func TestHandshakeIsEnforced(t *testing.T) {
	tests := []struct {
		name string
		do   func(t *testing.T, p *peer) error
	}{
		{"a call before hello", func(t *testing.T, p *peer) error {
			return p.call(t, MethodTermCwd, IDParams{ID: "t1"}, nil)
		}},
		{"hello with a wrong token", func(t *testing.T, p *peer) error {
			return p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: strings.Repeat("x", 32)}, nil)
		}},
		{"hello with no token", func(t *testing.T, p *peer) error {
			return p.call(t, MethodHello, HelloParams{Protocol: Protocol}, nil)
		}},
		{"hello without params", func(t *testing.T, p *peer) error {
			return p.call(t, MethodHello, nil, nil)
		}},
		{"hello twice", func(t *testing.T, p *peer) error {
			if err := p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
				t.Fatal(err)
			}
			return p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, nil)
			p := r.dial(t, nil)
			if err := tt.do(t, p); err == nil {
				t.Error("the call was answered")
			}
			closedWithin(t, p)
			waitFor(t, "the connection to be forgotten", func() bool { return r.srv.Clients() == 0 })
			if len(r.terms.started) != 0 {
				t.Error("an unauthenticated call reached the terminal manager")
			}
		})
	}
}

func TestIncompatibleProtocolMayOnlyShutDown(t *testing.T) {
	r := newRig(t, nil)
	p := r.dial(t, nil)
	var res HelloResult
	if err := p.call(t, MethodHello, HelloParams{Protocol: Protocol + 1, Token: testToken}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Compatible || res.Protocol != Protocol || res.Version != "test" {
		t.Errorf("hello result = %+v", res)
	}
	var rpcErr *jsonrpc.Error
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); !errors.As(err, &rpcErr) {
		t.Errorf("term.start on an incompatible connection = %v", err)
	}
	if err := p.call(t, MethodHostShutdown, ShutdownParams{StopSessions: true}, nil); err != nil {
		t.Fatal(err)
	}
	if stop := <-r.down; !stop {
		t.Error("OnShutdown lost stopSessions")
	}
	r.terms.mu.Lock()
	defer r.terms.mu.Unlock()
	if r.terms.all != 1 || !r.agents.closed {
		t.Errorf("sessions were not stopped: closeAll=%d agentsClosed=%v", r.terms.all, r.agents.closed)
	}
}

func TestShutdownWithoutStoppingKeepsSessions(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodHostShutdown, ShutdownParams{}, nil); err != nil {
		t.Fatal(err)
	}
	if stop := <-r.down; stop {
		t.Error("stopSessions was set")
	}
	if r.terms.all != 0 || r.agents.closed {
		t.Error("sessions were stopped")
	}
}

func TestUnknownMethodAndMissingParts(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	var rpcErr *jsonrpc.Error
	if err := p.call(t, "nope", nil, nil); !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("unknown method = %v", err)
	}
	bare := newRig(t, func(c *Config) { c.Terminals, c.Agents = nil, nil })
	q := bare.connect(t)
	if err := q.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); !errors.As(err, &rpcErr) || !strings.Contains(err.Error(), "not available") {
		t.Errorf("term.start without terminals = %v", err)
	}
	if err := q.call(t, MethodAgentAgents, nil, nil); err == nil {
		t.Error("agent.agents without agents succeeded")
	}
}

func TestInputFromTheWireIsValidated(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	many := make([]string, MaxArgs+1)
	tests := []struct {
		name   string
		method string
		params any
	}{
		{"empty terminal id", MethodTermStart, TermStartParams{}},
		{"terminal id too long", MethodTermStart, TermStartParams{ID: strings.Repeat("a", MaxID+1)}},
		{"terminal id with a control character", MethodTermStart, TermStartParams{ID: "a\nb"}},
		{"columns too large", MethodTermStart, TermStartParams{ID: "t2", Cols: MaxSize + 1}},
		{"negative rows", MethodTermStart, TermStartParams{ID: "t2", Rows: -1}},
		{"too many arguments", MethodTermStart, TermStartParams{ID: "t2", Args: many}},
		{"too many environment entries", MethodTermStart, TermStartParams{ID: "t2", Env: many}},
		{"cwd too long", MethodTermStart, TermStartParams{ID: "t2", Cwd: strings.Repeat("a", MaxPath+1)}},
		{"write too large", MethodTermWrite, TermWriteParams{ID: "t1", Data: make([]byte, MaxWrite+1)}},
		{"resize too large", MethodTermResize, TermResizeParams{ID: "t1", Cols: 1, Rows: MaxSize + 1}},
		{"write with empty id", MethodTermWrite, TermWriteParams{}},
		{"attach with empty id", MethodTermAttach, AttachParams{}},
		{"prompt too large", MethodAgentPrompt, AgentPromptParams{ID: "c", Text: strings.Repeat("a", MaxText+1)}},
		{"prompt with a bad id", MethodAgentPrompt, AgentPromptParams{ID: "", Text: "x"}},
		{"agent name too long", MethodAgentStart, AgentStartParams{Agent: strings.Repeat("a", MaxID+1)}},
		{"params of the wrong type", MethodTermClose, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rpcErr *jsonrpc.Error
			if err := p.call(t, tt.method, tt.params, nil); !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams {
				t.Errorf("%s = %v, want invalid params", tt.method, err)
			}
		})
	}
	r.terms.mu.Lock()
	defer r.terms.mu.Unlock()
	if len(r.terms.started) != 1 {
		t.Errorf("rejected requests reached the manager: %v", r.terms.started)
	}
	if len(r.terms.written["t1"]) != 0 {
		t.Error("a rejected write reached the shell")
	}
}

func TestMessageOverTheLimitClosesTheConnection(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	done := make(chan error, 1)
	go func() {
		_, err := p.raw.Write([]byte(strings.Repeat("a", MaxMessage+1<<10) + "\n"))
		done <- err
	}()
	closedWithin(t, p)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer was not released")
	}
	waitFor(t, "the connection to be forgotten", func() bool { return r.srv.Clients() == 0 })
}

func TestTerminalCountIsBounded(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for i := range MaxSessions {
		if err := p.call(t, MethodTermStart, TermStartParams{ID: fmt.Sprint("t", i)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "one-more"}, nil); err == nil {
		t.Error("the terminal over the limit was started")
	}
}

type termData struct {
	ID   string `json:"id"`
	Seq  uint64 `json:"seq"`
	Data []byte `json:"data"`
}

func nextData(t *testing.T, p *peer) termData {
	t.Helper()
	n := p.next(t)
	if n.method != NotifyTermData {
		t.Fatalf("notification %s, want term.data", n.method)
	}
	var d termData
	if err := json.Unmarshal(n.params, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTerminalOutputIsNumberedAndReplayable(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1", Cols: 100, Rows: 30}, nil); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"one", "two", "three"} {
		r.terms.events.Output("t1", []byte(s))
	}
	for i, want := range []string{"one", "two", "three"} {
		if d := nextData(t, p); d.ID != "t1" || d.Seq != uint64(i+1) || string(d.Data) != want {
			t.Errorf("notification %d = %+v", i, d)
		}
	}
	var res TermAttachResult
	if err := p.call(t, MethodTermAttach, AttachParams{ID: "t1", AfterSeq: 1}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Reset || res.Last != 3 || len(res.Chunks) != 2 || string(res.Chunks[0].Data) != "two" || res.Chunks[1].Seq != 3 {
		t.Errorf("attach after 1 = %+v", res)
	}
	if err := p.call(t, MethodTermAttach, AttachParams{ID: "nope"}, nil); err == nil {
		t.Error("attach to an unknown terminal succeeded")
	}
}

func TestTerminalAttachAfterTheRingMovedOnResets(t *testing.T) {
	r := newRig(t, func(c *Config) { c.ReplayKiB = 1 })
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1", Cols: 100, Rows: 30}, nil); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		r.terms.events.Output("t1", []byte(strings.Repeat(fmt.Sprint(i), 400)))
	}
	var res TermAttachResult
	if err := p.call(t, MethodTermAttach, AttachParams{ID: "t1", AfterSeq: 1}, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Reset || len(res.Chunks) != 2 || res.Chunks[0].Seq != 4 || res.Chunks[1].Seq != 5 {
		t.Errorf("attach = reset %v, chunks %v", res.Reset, res.Chunks)
	}
	r.terms.mu.Lock()
	defer r.terms.mu.Unlock()
	if got := r.terms.sizes["t1"]; len(got) != 2 || got[0] != [2]int{100, 31} || got[1] != [2]int{100, 30} {
		t.Errorf("redraw nudge = %v, want a size change and back", got)
	}
}

func TestTerminalAttachPagesALargeRing(t *testing.T) {
	r := newRig(t, func(c *Config) { c.ReplayKiB = 4096 })
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	const total = 3 << 20
	r.terms.events.Output("t1", make([]byte, total))

	var after uint64
	var got, pages int
	for {
		var res TermAttachResult
		if err := p.call(t, MethodTermAttach, AttachParams{ID: "t1", AfterSeq: after}, &res); err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		if res.Reset {
			t.Fatal("reset while paging a ring that did not move")
		}
		for _, c := range res.Chunks {
			got += len(c.Data)
			after = c.Seq
		}
		if !res.More {
			break
		}
	}
	if got != total || pages < 3 {
		t.Errorf("got %d bytes in %d pages, want %d in at least 3", got, pages, total)
	}
}

func TestTerminalExitIsRememberedUntilTheTabCloses(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	r.terms.events.Output("t1", []byte("bye"))
	r.terms.events.Exited("t1", 3)
	nextData(t, p)
	n := p.next(t)
	var exit TermExitParams
	if n.method != NotifyTermExit || json.Unmarshal(n.params, &exit) != nil || exit.Code != 3 || exit.Seq != 1 {
		t.Fatalf("notification = %s %s", n.method, n.params)
	}
	// A window that comes later still learns how the shell ended.
	late := r.connect(t)
	var res TermAttachResult
	if err := late.call(t, MethodTermAttach, AttachParams{ID: "t1"}, &res); err != nil || !res.Exited || res.Code != 3 || len(res.Chunks) != 1 {
		t.Errorf("attach after exit = %+v, %v", res, err)
	}
	if err := late.call(t, MethodTermClose, IDParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := late.call(t, MethodTermAttach, AttachParams{ID: "t1"}, nil); err == nil {
		t.Error("the terminal outlived its close")
	}
}

func TestClosingARunningTerminalStillReportsItsExit(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.call(t, MethodTermClose, IDParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	n := p.next(t)
	if n.method != NotifyTermExit {
		t.Fatalf("notification = %s", n.method)
	}
	waitFor(t, "the entry to be released", func() bool { return r.srv.term("t1") == nil })
}

func TestTerminalTitleIsForwarded(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	r.terms.events.Title("t1", terminal.Info{Command: "npm run dev", Cwd: "/w"})
	n := p.next(t)
	var title TermTitleParams
	if n.method != NotifyTermTitle || json.Unmarshal(n.params, &title) != nil || title.Command != "npm run dev" || title.Cwd != "/w" {
		t.Errorf("notification = %s %s", n.method, n.params)
	}
}

func TestAgentStateIsForwarded(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	r.terms.events.AgentState("t1", "waiting")
	n := p.next(t)
	var st TermAgentParams
	if n.method != NotifyTermAgent || json.Unmarshal(n.params, &st) != nil || st.ID != "t1" || st.State != "waiting" {
		t.Errorf("notification = %s %s", n.method, n.params)
	}
}

func TestEveryClientGetsTheOutputOfItsTerminals(t *testing.T) {
	r := newRig(t, nil)
	a, b := r.connect(t), r.connect(t)
	if err := a.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.call(t, MethodTermAttach, AttachParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	r.terms.events.Output("t1", []byte("x"))
	for _, p := range []*peer{a, b} {
		if d := nextData(t, p); d.Seq != 1 {
			t.Errorf("seq = %d", d.Seq)
		}
	}
	a.conn.Close(nil)
	_ = a.raw.Close()
	waitFor(t, "the first client to be forgotten", func() bool { return r.srv.Clients() == 1 })
	r.terms.events.Output("t1", []byte("y"))
	if d := nextData(t, b); d.Seq != 2 || string(d.Data) != "y" {
		t.Errorf("the remaining client got %+v", d)
	}
}

func TestSlowClientIsDroppedWithoutHoldingUpOthers(t *testing.T) {
	r := newRig(t, nil)
	good := r.connect(t)
	var slowGate *gated
	slow := r.dial(t, func(c net.Conn) net.Conn {
		slowGate = &gated{Conn: c, hold: make(chan struct{})}
		return slowGate
	})
	if err := slow.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
		t.Fatal(err)
	}
	if err := good.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := slow.call(t, MethodTermAttach, AttachParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	slowGate.block.Store(true)

	const outputs, batch = 3000, 100
	chunk := make([]byte, 4<<10)
	start := time.Now()
	for i := range outputs {
		r.terms.events.Output("t1", chunk) // the PTY pump: it must never wait for a client
		if (i+1)%batch == 0 {
			// Pace the producer for the good client's sake only.
			waitFor(t, "the good client to keep up", func() bool { return len(good.notes) >= i+1 })
		}
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("producing took %v", d)
	}
	for i := 1; i <= outputs; i++ {
		if d := nextData(t, good); d.Seq != uint64(i) {
			t.Fatalf("good client: seq %d at position %d", d.Seq, i)
		}
	}
	waitFor(t, "only the good client to stay", func() bool { return r.srv.Clients() == 1 })
	close(slowGate.hold) // let the slow client notice that it was dropped
	closedWithin(t, slow)
}
