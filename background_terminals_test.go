package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	"flint/internal/agenthost/hostmode"
	"flint/internal/sessions"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// safeTerms is a sessions.Terminals fake that may be called from any goroutine.
type safeTerms struct {
	mu       sync.Mutex
	started  []string
	closed   []string
	closeAll int
}

func (f *safeTerms) Start(id string, _ terminal.Options) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, id)
	return nil
}
func (f *safeTerms) Write(string, []byte) error             { return nil }
func (f *safeTerms) Resize(string, int, int) error          { return nil }
func (f *safeTerms) Cwd(string) (string, error)             { return "", nil }
func (f *safeTerms) Resume(string) (terminal.Resume, error) { return terminal.Resume{}, nil }
func (f *safeTerms) Close(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
}
func (f *safeTerms) CloseAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeAll++
}
func (f *safeTerms) StartStream(id string, _ terminal.Stream) error {
	return f.Start(id, terminal.Options{})
}

func (f *safeTerms) snapshot() (started, closed []string, closeAll int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.started...), append([]string{}, f.closed...), f.closeAll
}

// fakeConn is a connection to a host that exists only in the test.
type fakeConn struct {
	terms     *safeTerms
	hello     agenthost.HelloResult
	handlers  client.Handlers
	positions map[string]uint64

	mu        sync.Mutex
	list      []agenthost.TermStatus
	attached  map[string]uint64
	shutdowns []bool
	closed    bool
	settings  int
}

func newFakeConn(held ...string) *fakeConn {
	c := &fakeConn{terms: &safeTerms{}, attached: map[string]uint64{}, hello: agenthost.HelloResult{Terminals: held}}
	for _, id := range held {
		c.list = append(c.list, agenthost.TermStatus{ID: id})
	}
	return c
}

func (c *fakeConn) Terminals() sessions.Terminals { return c.terms }
func (c *fakeConn) Hello() agenthost.HelloResult  { return c.hello }
func (c *fakeConn) Positions() map[string]uint64  { return maps.Clone(c.positions) }
func (c *fakeConn) SettingsChanged(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings++
	return nil
}
func (c *fakeConn) AttachTerminal(id string, after uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attached[id] = after
	return nil
}
func (c *fakeConn) ListTerminals(context.Context) ([]agenthost.TermStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agenthost.TermStatus{}, c.list...), nil
}
func (c *fakeConn) Shutdown(_ context.Context, stop bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdowns = append(c.shutdowns, stop)
	return nil
}
func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// drop simulates the connection breaking under the window.
func (c *fakeConn) drop() { c.handlers.OnDisconnect(errors.New("gone")) }

func (c *fakeConn) state() (attached map[string]uint64, shutdowns []bool, closed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.attached), append([]bool{}, c.shutdowns...), c.closed
}

// opener answers Dial or Spawn from a script, one result per call.
type opener struct {
	mu      sync.Mutex
	results []func() (*fakeConn, error)
	calls   int
}

func (o *opener) open(_ context.Context, _ string, cfg client.Config) (hostConn, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	if len(o.results) == 0 {
		return nil, errors.New("unscripted")
	}
	next := o.results[0]
	o.results = o.results[1:]
	c, err := next()
	if c == nil {
		return nil, err
	}
	c.handlers = cfg.Handlers
	return c, err
}

func (o *opener) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

func script(c *fakeConn, err error) func() (*fakeConn, error) {
	return func() (*fakeConn, error) { return c, err }
}

var notRunning = fmt.Errorf("%w (test)", hostmode.ErrNotRunning)

// termRecorder records what reaches the frontend's event path.
type termRecorder struct {
	mu    sync.Mutex
	out   map[string]string
	exits map[string]int
	agent map[string]string
}

func newTermEventsRecorder() *termRecorder {
	return &termRecorder{out: map[string]string{}, exits: map[string]int{}, agent: map[string]string{}}
}
func (e *termRecorder) Output(id string, b []byte) {
	e.mu.Lock()
	e.out[id] += string(b)
	e.mu.Unlock()
}
func (e *termRecorder) Exited(id string, code int)  { e.mu.Lock(); e.exits[id] = code; e.mu.Unlock() }
func (e *termRecorder) Title(string, terminal.Info) {}
func (e *termRecorder) AgentState(id, state string) {
	e.mu.Lock()
	e.agent[id] = state
	e.mu.Unlock()
}
func (e *termRecorder) agentState(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agent[id]
}
func (e *termRecorder) output(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.out[id]
}
func (e *termRecorder) exit(id string) (int, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	code, ok := e.exits[id]
	return code, ok
}

type bgRig struct {
	h           *hostTerminals
	local       *safeTerms
	dial, spawn *opener
	events      *termRecorder
	choices     []string // what the dialog was shown with
	answer      string
}

func newRigWith(dial, spawn []func() (*fakeConn, error)) *bgRig {
	r := &bgRig{local: &safeTerms{}, dial: &opener{results: dial}, spawn: &opener{results: spawn}, events: newTermEventsRecorder()}
	r.h = newHostTerminals(hostTermConfig{
		ConfigDir: "/cfg", Version: "test", Events: r.events, Local: r.local,
		Dial: r.dial.open, Spawn: r.spawn.open,
		Choose: func(_, _ string, buttons []string) string { r.choices = buttons; return r.answer },
	})
	return r
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWireTerminalsFollowsTheSetting(t *testing.T) {
	for _, on := range []bool{false, true} {
		a := &App{settings: settings.Default()}
		a.settings.Sessions.Background = on
		dial := &opener{}
		a.wireTerminals(newTermEventsRecorder(), dial.open, dial.open)
		if (a.background != nil) != on {
			t.Fatalf("background %v: hostTerminals = %v", on, a.background)
		}
		if on && a.termRouter.main != sessions.Terminals(a.background) {
			t.Error("the router's main terminals are not the host's")
		}
		if _, inProcess := a.termRouter.main.(*terminal.Manager); inProcess == on {
			t.Errorf("background %v: main is %T", on, a.termRouter.main)
		}
		if dial.count() != 0 {
			t.Error("wiring dialed the host: that is startup's job, in the background")
		}
		if a.terminals != sessions.Terminals(a.termRouter) {
			t.Error("terminals are not the router")
		}
	}
}

func TestConnectUsesARunningHost(t *testing.T) {
	conn := newFakeConn()
	r := newRigWith([]func() (*fakeConn, error){script(conn, nil)}, nil)
	sawConnected := false
	r.h.cfg.OnChange = func() { sawConnected = r.h.connected() } // the menu is rebuilt from here
	r.h.connect()
	if !sawConnected {
		t.Error("OnChange ran before the connection was usable")
	}
	if err := r.h.Start("t1", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	if started, _, _ := conn.terms.snapshot(); len(started) != 1 || started[0] != "t1" {
		t.Errorf("host started %v", started)
	}
	if started, _, _ := r.local.snapshot(); len(started) != 0 {
		t.Errorf("in-process manager started %v", started)
	}
	if r.spawn.count() != 0 || !r.h.connected() || r.h.takeNotice() != "" {
		t.Errorf("spawned %d, connected %v", r.spawn.count(), r.h.connected())
	}
}

func TestConnectStartsAHostWhenNoneRuns(t *testing.T) {
	conn := newFakeConn()
	r := newRigWith([]func() (*fakeConn, error){script(nil, notRunning)}, []func() (*fakeConn, error){script(conn, nil)})
	r.h.connect()
	_ = r.h.Start("t1", terminal.Options{})
	if started, _, _ := conn.terms.snapshot(); len(started) != 1 {
		t.Errorf("the spawned host was not used: %v", started)
	}
}

// A host that cannot be reached or started leaves terminals in process for
// this run, and the UI is told once.
func TestConnectFallsBackToInProcess(t *testing.T) {
	r := newRigWith([]func() (*fakeConn, error){script(nil, notRunning)}, []func() (*fakeConn, error){script(nil, errors.New("no exec"))})
	r.h.connect()
	_ = r.h.Start("t1", terminal.Options{})
	if started, _, _ := r.local.snapshot(); len(started) != 1 {
		t.Errorf("in-process start = %v", started)
	}
	if n := r.h.takeNotice(); !strings.Contains(n, "no exec") {
		t.Errorf("notice = %q", n)
	}
	if n := r.h.takeNotice(); n != "" {
		t.Errorf("second notice = %q", n)
	}
	if r.h.connected() {
		t.Error("connected after a failure")
	}
	if list, err := r.h.live(); list != nil || err != nil {
		t.Errorf("live = %v, %v", list, err)
	}
}

// Another dial error than "not running" (a socket that refuses us) does not
// start a second host either.
func TestConnectDoesNotSpawnOverAnotherError(t *testing.T) {
	r := newRigWith([]func() (*fakeConn, error){script(nil, errors.New("permission denied"))}, nil)
	r.h.connect()
	if r.spawn.count() != 0 || r.h.connected() {
		t.Errorf("spawned %d, connected %v", r.spawn.count(), r.h.connected())
	}
}

func TestProtocolMismatch(t *testing.T) {
	skew := &client.ProtocolError{Host: 99, Version: "9.9"}
	for _, answer := range []string{keepSessionsLabel, ""} {
		old := newFakeConn("old")
		r := newRigWith([]func() (*fakeConn, error){script(old, skew)}, nil)
		r.answer = answer
		r.h.connect()
		_, shutdowns, closed := old.state()
		if len(shutdowns) != 0 || !closed {
			t.Errorf("%q: old host shut down %v, closed %v", answer, shutdowns, closed)
		}
		if len(r.choices) != 2 || r.choices[0] != restartSessionsLabel || r.choices[1] != keepSessionsLabel {
			t.Errorf("dialog buttons = %v", r.choices)
		}
		_ = r.h.Start("t1", terminal.Options{})
		if started, _, _ := r.local.snapshot(); len(started) != 1 {
			t.Errorf("%q: not in process: %v", answer, started)
		}
	}

	old, fresh := newFakeConn("old"), newFakeConn()
	r := newRigWith([]func() (*fakeConn, error){script(old, skew)}, []func() (*fakeConn, error){
		script(old, skew), // the old host has not let go of its lock yet
		script(fresh, nil),
	})
	r.answer = restartSessionsLabel
	r.h.connect()
	if _, shutdowns, closed := old.state(); len(shutdowns) != 1 || !shutdowns[0] || !closed {
		t.Errorf("old host: shutdown %v, closed %v", shutdowns, closed)
	}
	_ = r.h.Start("t1", terminal.Options{})
	if started, _, _ := fresh.terms.snapshot(); len(started) != 1 {
		t.Errorf("the restarted host was not used: %v", started)
	}
}

// Detaching (closing the window, Quit) leaves the host's terminals alone and
// ends the in-process ones and the stream sessions.
func TestDetachKeepsHostTerminals(t *testing.T) {
	conn := newFakeConn()
	r := newRigWith([]func() (*fakeConn, error){script(conn, nil)}, nil)
	r.h.connect()
	streams := &safeTerms{}
	a := &App{background: r.h, termRouter: newTerminalsRouter(r.h, streams)}
	a.terminals = a.termRouter
	_ = a.terminals.Start("shell", terminal.Options{})
	_ = a.termRouter.StartStream("docker", terminal.Stream{})

	a.closeTerminals()
	_, closed, closeAll := conn.terms.snapshot()
	if len(closed) != 0 || closeAll != 0 {
		t.Errorf("host terminals closed: %v, CloseAll %d", closed, closeAll)
	}
	if _, shutdowns, connClosed := conn.state(); len(shutdowns) != 0 || !connClosed {
		t.Errorf("shutdown %v, connection closed %v", shutdowns, connClosed)
	}
	if _, _, n := streams.snapshot(); n != 1 {
		t.Error("stream sessions were not closed")
	}
	if _, _, n := r.local.snapshot(); n != 1 {
		t.Error("in-process terminals were not closed")
	}
	conn.drop() // the host closing our connection now is expected: no reconnect
	time.Sleep(20 * time.Millisecond)
	if r.dial.count() != 1 {
		t.Errorf("dialed %d times after detaching", r.dial.count())
	}
}

func TestQuitAndStopSendsShutdown(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		conn := newFakeConn()
		r := newRigWith([]func() (*fakeConn, error){script(conn, nil)}, nil)
		r.h.connect()
		a := &App{background: r.h, termRouter: newTerminalsRouter(r.h, &safeTerms{})}
		a.terminals = a.termRouter
		a.quitAndStopSessions()
		if cancel {
			(&AppAPI{app: a}).CancelQuit()
		}
		a.closeTerminals()
		_, shutdowns, closed := conn.state()
		want := []bool{true}
		if cancel {
			want = nil
		}
		if fmt.Sprint(shutdowns) != fmt.Sprint(want) || !closed {
			t.Errorf("cancelled %v: shutdowns %v, closed %v", cancel, shutdowns, closed)
		}
	}
}

// The item sits after Quit: hidden while background terminals are off,
// enabled while the host is connected. (The macOS app menu needs a running
// Wails app for its roles, so the Linux menu is built in full here.)
func TestStopSessionsMenuItem(t *testing.T) {
	saved := isMac
	isMac = false
	t.Cleanup(func() { isMac = saved })
	off := (&App{}).buildMenu().FindByLabel(stopSessionsLabel)
	if off == nil || !off.Hidden() {
		t.Errorf("with background terminals off the item is %v", off)
	}
	r := newRigWith([]func() (*fakeConn, error){script(newFakeConn(), nil)}, nil)
	r.h.connect()
	on := (&App{background: r.h}).buildMenu().FindByLabel(stopSessionsLabel)
	if on == nil || on.Hidden() || !on.Enabled() {
		t.Errorf("with the host connected the item is %v", on)
	}
	failed := newRigWith([]func() (*fakeConn, error){script(nil, errors.New("down"))}, nil)
	failed.h.connect()
	fallback := (&App{background: failed.h}).buildMenu().FindByLabel(stopSessionsLabel)
	if fallback == nil || fallback.Hidden() || fallback.Enabled() {
		t.Errorf("with terminals in process the item is %v", fallback)
	}
}

// A dropped connection is re-established once; every terminal re-attaches
// from the last output the window showed, and the ones the host lost end.
func TestReconnectReattachesFromLastSeq(t *testing.T) {
	first := newFakeConn("t1", "t2")
	first.positions = map[string]uint64{"t1": 5, "t2": 3}
	second := newFakeConn("t1") // a host that kept t1 only
	third := newFakeConn()
	r := newRigWith([]func() (*fakeConn, error){script(first, nil), script(second, nil), script(third, nil)}, nil)
	r.h.connect()

	first.drop()
	waitUntil(t, "the re-attach", func() bool { a, _, _ := second.state(); return len(a) == 1 })
	if attached, _, _ := second.state(); attached["t1"] != 5 {
		t.Errorf("re-attached %v, want t1 after seq 5", attached)
	}
	waitUntil(t, "t2 to end", func() bool { _, ok := r.events.exit("t2"); return ok })
	if _, ok := r.events.exit("t1"); ok {
		t.Error("t1 ended although the host kept it")
	}
	_ = r.h.Start("t3", terminal.Options{})
	if started, _, _ := second.terms.snapshot(); len(started) != 1 {
		t.Errorf("new terminals do not go to the new connection: %v", started)
	}

	// A second loss is not retried: the tabs show the error.
	second.positions = map[string]uint64{"t1": 9, "t3": 0}
	second.drop()
	waitUntil(t, "t1 and t3 to end", func() bool {
		_, a := r.events.exit("t1")
		_, b := r.events.exit("t3")
		return a && b
	})
	if r.dial.count() != 2 {
		t.Errorf("dialed %d times, want 2 (start and one reconnect)", r.dial.count())
	}
	if !strings.Contains(r.events.output("t1"), "Lost this terminal") {
		t.Errorf("t1 shows %q", r.events.output("t1"))
	}
	_ = r.h.Start("t4", terminal.Options{})
	if started, _, _ := r.local.snapshot(); len(started) != 1 {
		t.Errorf("after giving up new terminals run in process: %v", started)
	}
}

func TestReconnectFailureEndsTheTabs(t *testing.T) {
	first := newFakeConn("t1")
	first.positions = map[string]uint64{"t1": 2}
	r := newRigWith([]func() (*fakeConn, error){script(first, nil), script(nil, notRunning)},
		[]func() (*fakeConn, error){script(nil, errors.New("cannot start"))})
	r.h.connect()
	first.drop()
	waitUntil(t, "t1 to end", func() bool { code, ok := r.events.exit("t1"); return ok && code == -1 })
}

func TestLiveAndAttach(t *testing.T) {
	conn := newFakeConn("kept", "done")
	conn.list[1].Exited = true
	r := newRigWith([]func() (*fakeConn, error){script(conn, nil)}, nil)
	r.h.connect()
	a := &App{settings: settings.Default(), background: r.h, terminals: r.h}
	a.mcp, a.mcpSources = newMCP(a)
	t.Cleanup(func() { _ = a.mcp.Close() })

	live := (&SessionAPI{app: a}).Live()
	if !live.Background || fmt.Sprint(live.Terminals) != "[{kept true} {done false}]" {
		t.Errorf("Live = %+v", live)
	}
	api := &TerminalAPI{app: a}
	if err := api.Attach("nope"); err == nil {
		t.Error("attached an id the host does not hold")
	}
	if err := api.Attach("kept"); err != nil {
		t.Fatal(err)
	}
	if attached, _, _ := conn.state(); attached["kept"] != 0 || len(attached) != 1 {
		t.Errorf("attached %v, want kept from the start", attached)
	}
	if err := api.Attach("kept"); err == nil {
		t.Error("attached the same terminal twice")
	}
	_ = r.h.Start("mine", terminal.Options{})
	conn.list = append(conn.list, agenthost.TermStatus{ID: "mine"})
	if err := api.Attach("mine"); err == nil {
		t.Error("attached a terminal this window started")
	}
	api.Close("kept")
	if err := api.Attach("kept"); err != nil {
		t.Errorf("a closed tab's id stays claimed: %v", err)
	}

	off := &App{settings: settings.Default()}
	if live := (&SessionAPI{app: off}).Live(); live.Background || len(live.Terminals) != 0 {
		t.Errorf("Live with the setting off = %+v", live)
	}
	if err := (&TerminalAPI{app: off}).Attach("kept"); err == nil {
		t.Error("Attach worked with the setting off")
	}
}

func TestSettingsChangeReachesTheHost(t *testing.T) {
	conn := newFakeConn()
	r := newRigWith([]func() (*fakeConn, error){script(conn, nil)}, nil)
	r.h.connect()
	r.h.settingsChanged()
	if conn.settings != 1 {
		t.Errorf("settingsChanged sent %d times", conn.settings)
	}
}
