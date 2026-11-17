package agenthost

import (
	"os"
	"slices"
	"testing"
	"time"
)

// hello tells a window that attaches later what the host holds, so it can
// find its sessions.
func TestHelloListsWhatTheHostHolds(t *testing.T) {
	r := newRig(t, nil)
	a := r.connect(t)
	if err := a.call(t, MethodTermStart, TermStartParams{ID: "t2"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.call(t, MethodAgentStart, AgentStartParams{Agent: "fake", Cwd: "/w"}, nil); err != nil {
		t.Fatal(err)
	}

	b := r.dial(t, nil)
	var res HelloResult
	if err := b.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, &res); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Terminals, []string{"t1", "t2"}) || !slices.Equal(res.Chats, []string{"chat-1"}) {
		t.Errorf("hello = terminals %v chats %v", res.Terminals, res.Chats)
	}
	if res.Pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", res.Pid, os.Getpid())
	}
}

// An empty host answers with empty lists, not null: clients range over them.
func TestHelloListsAreNeverNull(t *testing.T) {
	r := newRig(t, nil)
	p := r.dial(t, nil)
	var raw map[string]any
	if err := p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"terminals", "chats"} {
		if l, ok := raw[k].([]any); !ok || len(l) != 0 {
			t.Errorf("%s = %#v, want []", k, raw[k])
		}
	}
}

// waitChange waits for the OnChange hook, which the host's idle timer relies on.
func waitChange(t *testing.T, ch <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("no change reported: %s", why)
	}
}

func TestOnChangeReportsClientsAndSessions(t *testing.T) {
	changed := make(chan struct{}, 64)
	r := newRig(t, func(c *Config) { c.OnChange = func() { changed <- struct{}{} } })
	drain := func() {
		for {
			select {
			case <-changed:
			default:
				return
			}
		}
	}

	p := r.connect(t)
	waitChange(t, changed, "a client connected")
	if l := r.srv.Load(); l.Clients != 1 || l.Terminals != 0 || l.Chats != 0 || l.Idle() {
		t.Errorf("load = %+v", l)
	}

	drain()
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	waitChange(t, changed, "a terminal started")
	if l := r.srv.Load(); l.Terminals != 1 {
		t.Errorf("load = %+v", l)
	}
	drain()
	r.terms.events.Exited("t1", 0)
	waitChange(t, changed, "a terminal exited")
	if l := r.srv.Load(); l.Terminals != 0 {
		t.Errorf("an exited terminal still counts as a session: %+v", l)
	}

	drain()
	if err := p.call(t, MethodAgentStart, AgentStartParams{Agent: "fake", Cwd: "/w"}, nil); err != nil {
		t.Fatal(err)
	}
	waitChange(t, changed, "a chat started")
	if l := r.srv.Load(); l.Chats != 1 {
		t.Errorf("load = %+v", l)
	}
	drain()
	if err := p.call(t, MethodAgentStop, IDParams{ID: "chat-1"}, nil); err != nil {
		t.Fatal(err)
	}
	waitChange(t, changed, "a chat stopped")
	if l := r.srv.Load(); l.Chats != 0 {
		t.Errorf("load = %+v", l)
	}

	drain()
	p.conn.Close(nil)
	_ = p.raw.Close()
	waitChange(t, changed, "a client left")
	if l := r.srv.Load(); !l.Idle() {
		t.Errorf("load = %+v, want idle", l)
	}
}

func TestOnChangeIsOptional(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
}
