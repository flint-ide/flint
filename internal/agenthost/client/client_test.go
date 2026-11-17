package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

const token = "0123456789abcdef0123456789abcdef"

// fakeHost plays the host with a script, to give the client situations a real
// host only produces by accident: gaps, resets, silence, a different protocol.
type fakeHost struct {
	conn    *jsonrpc.Conn
	end     net.Conn
	mu      sync.Mutex
	chunks  []agenthost.Chunk // what term.attach serves
	reset   bool              // make the next term.attach a reset
	attach  chan agenthost.AttachParams
	hello   agenthost.HelloResult
	silent  atomic.Bool // answer nothing but hello
	started chan struct{}
}

func newFakeHost(t *testing.T) (net.Conn, *fakeHost) {
	t.Helper()
	a, b := net.Pipe()
	h := &fakeHost{end: b, attach: make(chan agenthost.AttachParams, 64),
		hello: agenthost.HelloResult{Protocol: agenthost.Protocol, Version: "fake", Compatible: true}}
	h.conn = jsonrpc.New(b, jsonrpc.Options{OnRequest: h.handle})
	go func() { _ = h.conn.Serve(b) }()
	t.Cleanup(func() { h.conn.Close(nil); _ = a.Close(); _ = b.Close() })
	return a, h
}

func (h *fakeHost) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == agenthost.MethodHello {
		return h.hello, nil
	}
	if h.silent.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	switch method {
	case agenthost.MethodTermStart, agenthost.MethodTermWrite, agenthost.MethodHostShutdown:
		return struct{}{}, nil
	case agenthost.MethodTermAttach:
		var p agenthost.AttachParams
		_ = json.Unmarshal(params, &p)
		h.attach <- p
		h.mu.Lock()
		defer h.mu.Unlock()
		res := agenthost.TermAttachResult{Reset: h.reset}
		h.reset = false
		for _, c := range h.chunks {
			if c.Seq > p.AfterSeq || res.Reset {
				res.Chunks = append(res.Chunks, c)
			}
			res.Last = c.Seq
		}
		return res, nil
	}
	return nil, jsonrpc.Errorf(jsonrpc.CodeMethodNotFound, "unscripted %s", method)
}

func (h *fakeHost) data(seq uint64, s string) {
	h.mu.Lock()
	h.chunks = append(h.chunks, agenthost.Chunk{Seq: seq, Data: []byte(s)})
	h.mu.Unlock()
}

func (h *fakeHost) send(t *testing.T, method string, params any) {
	t.Helper()
	if err := h.conn.Notify(method, params); err != nil {
		t.Fatal(err)
	}
}

func (h *fakeHost) sendData(t *testing.T, id string, seq uint64, s string) {
	t.Helper()
	h.data(seq, s)
	h.send(t, agenthost.NotifyTermData, agenthost.TermDataParams{ID: id, Seq: seq, Data: []byte(s)})
}

func connect(t *testing.T, h client.Handlers) (*client.Client, *fakeHost) {
	t.Helper()
	end, host := newFakeHost(t)
	c, err := client.New(end, client.Config{Token: token, Handlers: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, host
}

func TestMissingOutputIsFetchedFromTheHost(t *testing.T) {
	rec := newTermRecorder()
	c, host := connect(t, client.Handlers{Terminal: rec})
	if err := c.Terminals().Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	host.sendData(t, "t1", 1, "a")
	host.data(2, "b") // the host has seq 2, the client never got the notification
	host.sendData(t, "t1", 3, "c")
	rec.waitOutput(t, "t1", "abc")
	if p := <-host.attach; p.ID != "t1" || p.AfterSeq != 1 {
		t.Errorf("attach = %+v, want after seq 1", p)
	}
	host.send(t, agenthost.NotifyTermData, agenthost.TermDataParams{ID: "t1", Seq: 3, Data: []byte("c")}) // a duplicate
	host.sendData(t, "t1", 4, "d")
	rec.waitOutput(t, "t1", "abcd")
	if got := rec.output("t1"); got != "abcd" {
		t.Errorf("output = %q, want each chunk once and in order", got)
	}
}

func TestExitWaitsForTheOutputBeforeIt(t *testing.T) {
	rec := newTermRecorder()
	c, host := connect(t, client.Handlers{Terminal: rec})
	if err := c.Terminals().Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	host.sendData(t, "t1", 1, "a")
	host.data(2, "last words") // lost on the way
	host.send(t, agenthost.NotifyTermExit, agenthost.TermExitParams{ID: "t1", Code: 4, Seq: 2})
	if code := rec.waitExit(t, "t1"); code != 4 {
		t.Errorf("code = %d", code)
	}
	if got := rec.output("t1"); got != "alast words" {
		t.Errorf("output at the exit = %q", got)
	}
}

func TestResetClearsTheScreenFirst(t *testing.T) {
	rec := newTermRecorder()
	var resets atomic.Int32
	c, host := connect(t, client.Handlers{Terminal: rec, OnReset: func(kind, id string) {
		if kind == "terminal" && id == "t1" {
			resets.Add(1)
		}
	}})
	if err := c.Terminals().Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	host.sendData(t, "t1", 1, "old")
	rec.waitOutput(t, "t1", "old")
	host.mu.Lock()
	host.chunks, host.reset = nil, true // seqs 1 to 9 fell out of the host's ring
	host.mu.Unlock()
	host.data(10, "new1")
	host.sendData(t, "t1", 11, "new2")
	rec.waitOutput(t, "t1", "new2")
	if got := rec.output("t1"); got != "old\x1bcnew1new2" {
		t.Errorf("output = %q, want a full reset between the old and the replayed text", got)
	}
	if resets.Load() != 1 {
		t.Errorf("OnReset called %d times", resets.Load())
	}
}

func TestAttachingAfterReconnectReplaysFromTheGivenSeq(t *testing.T) {
	rec := newTermRecorder()
	c, host := connect(t, client.Handlers{Terminal: rec})
	host.data(1, "a")
	host.data(2, "b")
	host.data(3, "c")
	if err := c.AttachTerminal("t1", 1); err != nil {
		t.Fatal(err)
	}
	rec.waitOutput(t, "t1", "bc")
	if got := rec.output("t1"); got != "bc" {
		t.Errorf("output = %q", got)
	}
}

func TestDroppedNotificationsAreRecoveredAfterAFlood(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	var got strings.Builder
	var mu sync.Mutex
	done := make(chan struct{})
	const total = 3000
	var count int
	ev := &funcEvents{output: func(_ string, data []byte) {
		once.Do(func() { <-gate }) // the consumer stalls on the first chunk
		mu.Lock()
		got.Write(data)
		count++
		if count == total {
			close(done)
		}
		mu.Unlock()
	}}
	c, host := connect(t, client.Handlers{Terminal: ev})
	if err := c.Terminals().Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= total; i++ {
		host.sendData(t, "t1", uint64(i), "x") // more than the client queues: the rest is dropped
	}
	close(gate)
	select {
	case <-done:
	case <-time.After(wait):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("only %d of %d chunks arrived", count, total)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.String() != strings.Repeat("x", total) {
		t.Errorf("got %d bytes", got.Len())
	}
}

type funcEvents struct{ output func(string, []byte) }

func (f *funcEvents) Output(id string, data []byte) { f.output(id, data) }
func (f *funcEvents) Exited(string, int)            {}
func (f *funcEvents) Title(string, terminal.Info)   {}

func TestCallbacksMayCallTheClient(t *testing.T) {
	var c *client.Client
	called := make(chan error, 1)
	ev := &funcEvents{output: func(id string, _ []byte) {
		called <- c.Terminals().Write(id, []byte("reply"))
	}}
	var host *fakeHost
	c, host = connect(t, client.Handlers{Terminal: ev})
	if err := c.Terminals().Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	host.sendData(t, "t1", 1, "prompt>")
	select {
	case err := <-called:
		if err != nil {
			t.Errorf("write from a callback: %v", err)
		}
	case <-time.After(wait):
		t.Fatal("a callback that calls the client hung")
	}
}

func TestClosedConnectionFailsCallsInsteadOfHanging(t *testing.T) {
	var disconnects atomic.Int32
	c, host := connect(t, client.Handlers{OnDisconnect: func(error) { disconnects.Add(1) }})
	host.silent.Store(true)

	// A call already in flight when the host goes away.
	inFlight := make(chan error, 1)
	go func() { inFlight <- c.Terminals().Write("t1", []byte("x")) }()
	time.Sleep(50 * time.Millisecond)
	host.conn.Close(nil)
	_ = host.end.Close()

	select {
	case err := <-inFlight:
		if !errors.Is(err, client.ErrDisconnected) {
			t.Errorf("in-flight call = %v, want ErrDisconnected", err)
		}
	case <-time.After(wait):
		t.Fatal("a call hung after the connection closed")
	}
	select {
	case <-c.Done():
	case <-time.After(wait):
		t.Fatal("Done is not closed")
	}
	if c.Err() == nil {
		t.Error("Err is nil after the connection closed")
	}

	terms, agents := c.Terminals(), c.Agents()
	ctx := context.Background()
	failures := map[string]error{
		"Start":  terms.Start("t2", terminal.Options{}),
		"Resize": terms.Resize("t1", 1, 1),
		"Prompt": agents.Prompt("c", "x", nil),
		"Cancel": agents.Cancel("c"),
		"Stop":   agents.Stop("c"),
	}
	_, failures["Cwd"] = terms.Cwd("t1")
	_, failures["Resume"] = terms.Resume("t1")
	_, failures["agent Start"] = agents.Start(ctx, "fake", "/w")
	_, failures["agent Resume"] = agents.Resume(ctx, "fake", "/w", "s")
	for name, err := range failures {
		if !errors.Is(err, client.ErrDisconnected) {
			t.Errorf("%s = %v, want ErrDisconnected", name, err)
		}
	}
	if agents.Busy("c") || agents.Agents() != nil {
		t.Error("Busy or Agents reported something without a host")
	}
	if got := agents.Policy(); got != as.DefaultPolicy() {
		t.Errorf("policy = %+v, want the last known", got)
	}
	terms.Close("t1")
	terms.CloseAll()
	if err := agents.Close(); !errors.Is(err, client.ErrDisconnected) && err != nil {
		t.Errorf("Close = %v", err)
	}
	waitUntil(t, "OnDisconnect", func() bool { return disconnects.Load() == 1 })
	time.Sleep(20 * time.Millisecond)
	if n := disconnects.Load(); n != 1 {
		t.Errorf("OnDisconnect called %d times", n)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	eventually(t, what, cond)
}

func TestWrongProtocolLeavesOnlyShutdown(t *testing.T) {
	end, host := newFakeHost(t)
	host.hello = agenthost.HelloResult{Protocol: 99, Version: "from the future"}
	c, err := client.New(end, client.Config{Token: token})
	var perr *client.ProtocolError
	if !errors.As(err, &perr) || perr.Host != 99 || perr.Version != "from the future" {
		t.Fatalf("err = %v", err)
	}
	if c == nil {
		t.Fatal("no client to shut the host down with")
	}
	defer c.Close()
	if err := c.Shutdown(context.Background(), true); err != nil {
		t.Errorf("shutdown = %v", err)
	}
}

func TestRealServerRefusesAWrongToken(t *testing.T) {
	srv, err := agenthost.New(agenthost.Config{Token: token})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	a, b := net.Pipe()
	go func() { _ = srv.Serve(b) }()
	c, err := client.New(a, client.Config{Token: strings.Repeat("z", 32)})
	if err == nil || c != nil {
		t.Fatalf("New = %v, %v; want an error and no client", c, err)
	}
}

func TestTitleIsForwarded(t *testing.T) {
	rec := newTermRecorder()
	_, host := connect(t, client.Handlers{Terminal: rec})
	host.send(t, agenthost.NotifyTermTitle, agenthost.TermTitleParams{ID: "t1", Command: "npm test", Cwd: "/w"})
	rec.until(t, "the title", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return rec.titles["t1"] == terminal.Info{Command: "npm test", Cwd: "/w"}
	})
}

func TestUnknownNotificationsAreIgnored(t *testing.T) {
	rec := newTermRecorder()
	c, host := connect(t, client.Handlers{Terminal: rec})
	if err := c.Terminals().Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	host.send(t, "host.settingsChanged", map[string]int{"x": 1})
	host.sendData(t, "t1", 1, "ok")
	rec.waitOutput(t, "t1", "ok")
}

func TestInterfacesAreImplemented(t *testing.T) {
	var c *client.Client
	var _ sessions.Terminals = (*client.Client).Terminals(c)
	var _ sessions.Agents = (*client.Client).Agents(c)
}
