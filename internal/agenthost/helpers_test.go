package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

const testToken = "0123456789abcdef0123456789abcdef"

// fakeTerms is a terminal manager that runs nothing: tests push output and
// exits through events.
type fakeTerms struct {
	events terminal.Events

	mu      sync.Mutex
	started map[string]terminal.Options
	written map[string][]byte
	sizes   map[string][][2]int
	closed  []string
	all     int
}

func newFakeTerms(ev terminal.Events) *fakeTerms {
	return &fakeTerms{events: ev, started: map[string]terminal.Options{}, written: map[string][]byte{}, sizes: map[string][][2]int{}}
}

func (f *fakeTerms) Start(id string, o terminal.Options) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.started[id]; ok {
		return fmt.Errorf("terminal %q already running", id)
	}
	f.started[id] = o
	return nil
}

func (f *fakeTerms) Write(id string, b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.started[id]; !ok {
		return fmt.Errorf("terminal %q not running", id)
	}
	f.written[id] = append(f.written[id], b...)
	return nil
}

func (f *fakeTerms) Resize(id string, cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes[id] = append(f.sizes[id], [2]int{cols, rows})
	return nil
}

func (f *fakeTerms) Cwd(id string) (string, error) { return "/cwd/" + id, nil }
func (f *fakeTerms) Resume(string) (terminal.Resume, error) {
	return terminal.Resume{Agent: "claude"}, nil
}
func (f *fakeTerms) Close(id string) {
	f.mu.Lock()
	f.closed = append(f.closed, id)
	delete(f.started, id)
	f.mu.Unlock()
	f.events.Exited(id, 129) // a hang-up, as the real manager reports it
}
func (f *fakeTerms) CloseAll() {
	f.mu.Lock()
	f.all++
	f.mu.Unlock()
}

// fakeAgents is an agent manager whose events the test emits.
type fakeAgents struct {
	emit   func(as.Event)
	mu     sync.Mutex
	policy as.Policy
	closed bool
	stops  []string
}

func (f *fakeAgents) Agents() []string { return []string{"fake"} }
func (f *fakeAgents) Start(_ context.Context, agent, cwd string) (as.SessionInfo, error) {
	return as.SessionInfo{ID: "chat-1", Agent: agent, Cwd: cwd}, nil
}
func (f *fakeAgents) Resume(_ context.Context, agent, cwd, id string) (as.SessionInfo, error) {
	return as.SessionInfo{ID: id, Agent: agent, Cwd: cwd}, nil
}
func (f *fakeAgents) Prompt(string, string, []as.Attachment) error { return nil }
func (f *fakeAgents) Busy(string) bool                             { return true }
func (f *fakeAgents) Cancel(string) error                          { return nil }
func (f *fakeAgents) Respond(string, string, string) error         { return nil }
func (f *fakeAgents) Stop(id string) error {
	f.mu.Lock()
	f.stops = append(f.stops, id)
	f.mu.Unlock()
	return nil
}
func (f *fakeAgents) Policy() as.Policy { f.mu.Lock(); defer f.mu.Unlock(); return f.policy }
func (f *fakeAgents) SetPolicy(p as.Policy) {
	f.mu.Lock()
	f.policy = p
	f.mu.Unlock()
}
func (f *fakeAgents) Close() error { f.mu.Lock(); f.closed = true; f.mu.Unlock(); return nil }

// rig is a server with fakes.
type rig struct {
	srv    *Server
	terms  *fakeTerms
	agents *fakeAgents
	down   chan bool // host.shutdown requests
}

func newRig(t *testing.T, mod func(*Config)) *rig {
	t.Helper()
	r := &rig{down: make(chan bool, 4)}
	cfg := Config{
		Token: testToken, Version: "test",
		Terminals:  func(ev terminal.Events) sessions.Terminals { r.terms = newFakeTerms(ev); return r.terms },
		Agents:     func(emit func(as.Event)) sessions.Agents { r.agents = &fakeAgents{emit: emit}; return r.agents },
		OnShutdown: func(stop bool) { r.down <- stop },
	}
	if mod != nil {
		mod(&cfg)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r.srv = srv
	t.Cleanup(srv.Close)
	return r
}

// peer is a raw JSON-RPC client of the server.
type peer struct {
	conn  *jsonrpc.Conn
	notes chan note
	raw   net.Conn
}

type note struct {
	method string
	params json.RawMessage
}

// dial connects without saying hello; wrap may decorate the client's end.
func (r *rig) dial(t *testing.T, wrap func(net.Conn) net.Conn) *peer {
	t.Helper()
	a, b := net.Pipe()
	go func() { _ = r.srv.Serve(b) }()
	var end net.Conn = a
	if wrap != nil {
		end = wrap(a)
	}
	p := &peer{notes: make(chan note, 1<<16), raw: a}
	p.conn = jsonrpc.New(end, jsonrpc.Options{
		MaxMessage: MaxMessage,
		OnNotify: func(m string, params json.RawMessage) {
			select {
			case p.notes <- note{m, params}:
			default:
			}
		},
	})
	go func() { _ = p.conn.Serve(end) }()
	t.Cleanup(func() { p.conn.Close(nil); _ = a.Close() })
	return p
}

// connect dials and says hello.
func (r *rig) connect(t *testing.T) *peer {
	t.Helper()
	p := r.dial(t, nil)
	var res HelloResult
	if err := p.conn.Call(ctx(t), MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, &res); err != nil || !res.Compatible {
		t.Fatalf("hello = %+v, %v", res, err)
	}
	return p
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func (p *peer) call(t *testing.T, method string, params, result any) error {
	t.Helper()
	return p.conn.Call(ctx(t), method, params, result)
}

// next waits for the next notification.
func (p *peer) next(t *testing.T) note {
	t.Helper()
	select {
	case n := <-p.notes:
		return n
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
		return note{}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// gated passes reads through until blocked, then holds them: a client that
// stopped reading.
type gated struct {
	net.Conn
	block atomic.Bool
	hold  chan struct{}
}

func (g *gated) Read(b []byte) (int, error) {
	if g.block.Load() {
		<-g.hold
	}
	return g.Conn.Read(b)
}
