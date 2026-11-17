package agenthost

import (
	"context"
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

// closeAndKeep closes t1 while its shell stays alive (it ignores SIGHUP).
func closeAndKeep(t *testing.T, r *rig, p *peer, id string) {
	t.Helper()
	r.terms.setHoldExit(true)
	defer r.terms.setHoldExit(false)
	if err := p.call(t, MethodTermClose, IDParams{ID: id}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestClosedTerminalsIdIsFreeBeforeItsShellEnds(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	oldKey := r.srv.term("t1").key
	closeAndKeep(t, r, p, "t1")

	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1", Cols: 100}, nil); err != nil {
		t.Fatalf("starting under the id of a closed terminal: %v", err)
	}
	if r.srv.term("t1").key == oldKey {
		t.Fatal("the new shell shares the old one's identity, so their events could mix")
	}
	if l := r.srv.Load(); l.Terminals != 1 {
		t.Errorf("load = %+v, want only the new terminal", l)
	}

	// The old shell finally leaves: that is not the new terminal's exit.
	r.terms.events.Output(oldKey, []byte("old"))
	r.terms.events.Exited(oldKey, 129)
	r.terms.events.Output(r.srv.term("t1").key, []byte("new"))
	if d := nextData(t, p); string(d.Data) != "new" || d.Seq != 1 {
		t.Errorf("the first notification = %+v, want the new terminal's output", d)
	}
	if r.srv.term("t1").isExited() {
		t.Error("the old shell's exit marked the new terminal as ended")
	}
	if l := r.srv.Load(); l.Terminals != 1 {
		t.Errorf("load = %+v after the old exit", l)
	}
	r.srv.termsMu.Lock()
	n := len(r.srv.termByKey)
	r.srv.termsMu.Unlock()
	if n != 1 {
		t.Errorf("%d entries tracked, want only the new terminal's", n)
	}
}

func TestClosedTerminalsDoNotCountTowardTheCapOrTheLoad(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for i := range MaxSessions {
		if err := p.call(t, MethodTermStart, TermStartParams{ID: fmt.Sprint("t", i)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	closeAndKeep(t, r, p, "t0")
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "extra"}, nil); err != nil {
		t.Fatalf("a closed terminal still holds a place: %v", err)
	}
	if l := r.srv.Load(); l.Terminals != MaxSessions {
		t.Errorf("load = %+v, want %d", l, MaxSessions)
	}
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "one-more"}, nil); err == nil {
		t.Error("the cap on running terminals was exceeded")
	}
}

func TestClosedTerminalAloneLetsTheHostGoIdle(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	closeAndKeep(t, r, p, "t1")
	p.conn.Close(nil)
	_ = p.raw.Close()
	waitFor(t, "the host to be idle", func() bool { return r.srv.Load().Idle() })
}

func TestClosedTerminalIsNotListedInHello(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for _, id := range []string{"a", "b"} {
		if err := p.call(t, MethodTermStart, TermStartParams{ID: id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	closeAndKeep(t, r, p, "a")
	terms, _ := r.srv.held()
	if !slices.Equal(terms, []string{"b"}) {
		t.Errorf("held = %v", terms)
	}
}

func TestClosedTerminalRefusesInputButStillReportsItsExit(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	key := r.srv.term("t1").key
	closeAndKeep(t, r, p, "t1")
	for name, call := range map[string]func() error{
		"write":  func() error { return p.call(t, MethodTermWrite, TermWriteParams{ID: "t1", Data: []byte("x")}, nil) },
		"resize": func() error { return p.call(t, MethodTermResize, TermResizeParams{ID: "t1", Cols: 1, Rows: 1}, nil) },
		"cwd":    func() error { return p.call(t, MethodTermCwd, IDParams{ID: "t1"}, nil) },
		"resume": func() error { return p.call(t, MethodTermResume, IDParams{ID: "t1"}, nil) },
	} {
		var rpcErr *jsonrpc.Error
		if err := call(); err == nil || errors.As(err, &rpcErr) && rpcErr.Code == jsonrpc.CodeMethodNotFound {
			t.Errorf("%s on a closed terminal = %v", name, err)
		}
	}
	r.terms.events.Exited(key, 129)
	if n := p.next(t); n.method != NotifyTermExit {
		t.Errorf("notification = %s", n.method)
	}
	waitFor(t, "the entry to be released", func() bool { return r.srv.term("t1") == nil })
}

func TestClosingTerminalsAreBounded(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	r.terms.setHoldExit(true)
	for i := range MaxSessions {
		if err := p.call(t, MethodTermStart, TermStartParams{ID: "x"}, nil); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		if err := p.call(t, MethodTermClose, IDParams{ID: "x"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	err := p.call(t, MethodTermStart, TermStartParams{ID: "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Errorf("start with %d shells still leaving = %v, want a refusal", MaxSessions, err)
	}
}

func TestTermCloseOfAnUnknownIDIsHarmless(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermClose, IDParams{ID: "nope"}, nil); err != nil {
		t.Error(err)
	}
}

// term.attach carries the latest title, because title notifications have no
// sequence number and are lost when a client resyncs.
func TestAttachReplyCarriesTheLatestTitle(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	var res TermAttachResult
	if err := p.call(t, MethodTermAttach, AttachParams{ID: "t1"}, &res); err != nil || res.Title != nil {
		t.Fatalf("attach before any title = %+v, %v", res, err)
	}
	r.terms.events.Title("t1", terminal.Info{Command: "vim", Cwd: "/a"})
	r.terms.events.Title("t1", terminal.Info{Command: "npm run dev", Cwd: "/w"})
	late := r.connect(t)
	if err := late.call(t, MethodTermAttach, AttachParams{ID: "t1"}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Title == nil || res.Title.Command != "npm run dev" || res.Title.Cwd != "/w" {
		t.Errorf("title = %+v", res.Title)
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), `"title"`) {
		t.Errorf("reply = %s", raw)
	}
}

func TestShellAndArgumentsAreRefusedOnTheWire(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for name, params := range map[string]TermStartParams{
		"a shell":   {ID: "a", Shell: "/bin/sh"},
		"arguments": {ID: "b", Args: []string{"-c", "id"}},
	} {
		var rpcErr *jsonrpc.Error
		if err := p.call(t, MethodTermStart, params, nil); !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams {
			t.Errorf("%s = %v, want invalid params", name, err)
		}
	}
	if len(r.terms.startedIDs()) != 0 {
		t.Error("a refused request reached the terminal manager")
	}
}

func TestShellOverTheWireIsAllowedWhenConfigured(t *testing.T) {
	r := newRig(t, func(c *Config) { c.AllowShell = true })
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "a", Shell: "/bin/sh", Args: []string{"-c", "true"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.terms.started["a"]; got.Shell != "/bin/sh" || len(got.Args) != 2 {
		t.Errorf("options = %+v", got)
	}
}

func TestEnvironmentFromTheWireFollowsTheWebViewsRules(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for _, bad := range []string{"lower=x", "NO_EQUALS", "=x", "A=b\nc", "A=b\x00c", "A=" + strings.Repeat("x", 1025), "PATH=/x\r"} {
		var rpcErr *jsonrpc.Error
		err := p.call(t, MethodTermStart, TermStartParams{ID: "e", Env: []string{bad}}, nil)
		if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams {
			t.Errorf("env %.20q = %v, want invalid params", bad, err)
		}
	}
	if len(r.terms.startedIDs()) != 0 {
		t.Error("a refused request reached the terminal manager")
	}
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "e", Env: []string{"AWS_PROFILE=prod", "X="}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.terms.started["e"].Env; !slices.Equal(got, []string{"AWS_PROFILE=prod", "X="}) {
		t.Errorf("env = %v", got)
	}
}

func TestShutdownRunsTheHookOnceForConcurrentRequests(t *testing.T) {
	calls := make(chan bool, 16)
	r := newRig(t, func(c *Config) { c.OnShutdown = func(stop bool) { calls <- stop } })
	const n = 6
	peers := make([]*peer, n)
	for i := range peers {
		peers[i] = r.connect(t)
	}
	done := make(chan error, n)
	for _, p := range peers {
		go func() { done <- p.call(t, MethodHostShutdown, ShutdownParams{StopSessions: true}, nil) }()
	}
	for range n {
		if err := <-done; err != nil {
			t.Errorf("shutdown = %v", err)
		}
	}
	<-calls
	select {
	case <-calls:
		t.Error("the shutdown hook ran twice")
	case <-time.After(300 * time.Millisecond):
	}
}

// stalledWrites holds the host's writes once block is set: a client that
// stopped reading.
type stalledWrites struct {
	net.Conn
	block atomic.Bool
	hold  chan struct{}
}

func (s *stalledWrites) Write(b []byte) (int, error) {
	if s.block.Load() {
		<-s.hold
	}
	return s.Conn.Write(b)
}

func TestShutdownHookWaitsForTheReplyToBeWritten(t *testing.T) {
	called := make(chan struct{})
	r := newRig(t, func(c *Config) { c.OnShutdown = func(bool) { close(called) } })
	a, b := net.Pipe()
	host := &stalledWrites{Conn: b, hold: make(chan struct{})}
	go func() { _ = r.srv.Serve(host) }()
	conn := jsonrpc.New(a, jsonrpc.Options{MaxMessage: MaxMessage})
	go func() { _ = conn.Serve(a) }()
	t.Cleanup(func() { conn.Close(nil); _ = a.Close() })
	if err := conn.Call(ctx(t), MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
		t.Fatal(err)
	}
	host.block.Store(true)
	go func() { _ = conn.Call(context.Background(), MethodHostShutdown, ShutdownParams{}, nil) }()
	select {
	case <-called:
		t.Fatal("the host started to exit before the reply was written")
	case <-time.After(300 * time.Millisecond):
	}
	close(host.hold)
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("the host did not exit after the reply")
	}
}
