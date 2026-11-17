package agenthost

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func helloLine(id int) string {
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": MethodHello,
		"params": HelloParams{Protocol: Protocol, Token: testToken},
	})
	return string(data) + "\n"
}

func TestUnauthenticatedConnectionIsClosedAfterTheHelloTimeout(t *testing.T) {
	r := newRig(t, func(c *Config) { c.HelloTimeout = 100 * time.Millisecond })
	p := r.dial(t, nil)
	closedWithin(t, p)
	waitFor(t, "the connection to be forgotten", func() bool { return r.srv.Connections() == 0 })
}

func TestHelloInTimeKeepsTheConnection(t *testing.T) {
	r := newRig(t, func(c *Config) { c.HelloTimeout = 200 * time.Millisecond })
	p := r.connect(t)
	time.Sleep(400 * time.Millisecond) // well past the timeout
	if err := p.call(t, MethodAgentAgents, nil, nil); err != nil {
		t.Fatalf("an authenticated connection was closed by the hello timeout: %v", err)
	}
}

func TestUnauthenticatedConnectionsAreCappedSeparately(t *testing.T) {
	r := newRig(t, func(c *Config) { c.HelloTimeout = time.Hour })
	var open []*peer
	for range MaxUnauthenticated {
		open = append(open, r.dial(t, nil))
	}
	waitFor(t, "the connections to be served", func() bool { return r.srv.Connections() == MaxUnauthenticated })
	closedWithin(t, r.dial(t, nil)) // one over the cap

	// The cap is not shared with authenticated connections: a window can
	// still say hello when the unauthenticated slots are all taken ...
	if err := open[0].call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
		t.Fatal(err)
	}
	// ... and the slot it held is free again.
	late := r.dial(t, nil)
	if err := late.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
		t.Fatalf("the freed slot was not usable: %v", err)
	}
	for range 20 { // authenticated ones are not limited by that cap
		r.connect(t)
	}
}

func TestSlotOfAFailedHelloIsFreed(t *testing.T) {
	r := newRig(t, nil)
	for range 3 * MaxUnauthenticated {
		p := r.dial(t, nil)
		_ = p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: strings.Repeat("x", 32)}, nil)
		closedWithin(t, p)
		waitFor(t, "the slot to be freed", func() bool { return r.srv.Connections() == 0 })
	}
	r.connect(t)
}

func TestUnauthenticatedConnectionsAreNotClients(t *testing.T) {
	r := newRig(t, func(c *Config) { c.HelloTimeout = time.Hour })
	p := r.dial(t, nil)
	waitFor(t, "the connection to be served", func() bool { return r.srv.Connections() == 1 })
	if l := r.srv.Load(); l.Clients != 0 || !l.Idle() {
		t.Errorf("load with only an unauthenticated connection = %+v, want idle", l)
	}
	if err := p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
		t.Fatal(err)
	}
	if l := r.srv.Load(); l.Clients != 1 {
		t.Errorf("load after hello = %+v", l)
	}
}

func TestOnChangeIsReportedWhenAConnectionAuthenticates(t *testing.T) {
	changed := make(chan struct{}, 16)
	r := newRig(t, func(c *Config) { c.OnChange = func() { changed <- struct{}{} } })
	p := r.dial(t, nil)
	if err := p.call(t, MethodHello, HelloParams{Protocol: Protocol, Token: testToken}, nil); err != nil {
		t.Fatal(err)
	}
	waitChange(t, changed, "a client authenticated") // the idle timer must learn of it
}

// Until hello is done a peer may send very little: a line without an end must
// not be buffered up to the 4 MiB an authenticated peer may send.
func TestFirstMessageIsReadWithASmallLimit(t *testing.T) {
	r := newRig(t, nil)
	p := r.dial(t, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.raw.Write([]byte(strings.Repeat("a", 2*maxFirstMessage)))
	}()
	closedWithin(t, p)
	_ = p.raw.Close()
	<-done
}

func TestHelloWithinTheSmallLimitWorksAndLaterMessagesMayBeBig(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	if err := p.call(t, MethodTermStart, TermStartParams{ID: "t1"}, nil); err != nil {
		t.Fatal(err)
	}
	// Far over the first-message limit, well under the normal one.
	if err := p.call(t, MethodTermWrite, TermWriteParams{ID: "t1", Data: make([]byte, 512<<10)}, nil); err != nil {
		t.Fatalf("a big message after hello: %v", err)
	}
}

// The protocol: nothing may be sent before the hello reply arrived. A request
// that comes in the same write as hello cannot have waited for it, and the
// connection is closed, whatever the timing.
func TestRequestSentWithHelloIsRefused(t *testing.T) {
	for range 20 {
		r := newRig(t, nil)
		p := r.dial(t, nil)
		line := helloLine(1) + `{"jsonrpc":"2.0","id":2,"method":"term.cwd","params":{"id":"t1"}}` + "\n"
		go func() { _, _ = p.raw.Write([]byte(line)) }()
		closedWithin(t, p)
	}
}

// compatible is stored before authed, so whoever sees authed sees the right
// compatibility.
func TestAuthenticatedConnectionAlwaysKnowsItsCompatibility(t *testing.T) {
	r := newRig(t, nil)
	raw, _ := json.Marshal(HelloParams{Protocol: Protocol, Token: testToken})
	for range 3000 {
		cl := &client{}
		var wg sync.WaitGroup
		var bad atomic.Bool
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !cl.authed.Load() {
			}
			if !cl.compatible.Load() {
				bad.Store(true)
			}
		}()
		if _, err := r.srv.hello(cl, raw); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if bad.Load() {
			t.Fatal("a connection was authenticated before its compatibility was known")
		}
	}
}
