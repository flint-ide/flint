package remote_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/remote"
	"flint/internal/remote/client"
)

// fakeSecrets stands in for the Keychain.
type fakeSecrets struct {
	mu sync.Mutex
	v  map[string]string
}

func (f *fakeSecrets) Get(a string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.v[a], nil
}

func (f *fakeSecrets) Set(a, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.v[a] = v
	return nil
}

func (f *fakeSecrets) Delete(a string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.v, a)
	return nil
}

// world is what the server reads: open chats and their kept events.
type world struct {
	mu       sync.Mutex
	chats    []remote.Chat
	events   map[string][]as.Event
	keepFrom map[string]uint64 // first kept Seq per chat
	tasks    []remote.Task
}

func (w *world) Chats() []remote.Chat {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]remote.Chat{}, w.chats...)
}

func (w *world) Tasks(context.Context) ([]remote.Task, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]remote.Task{}, w.tasks...), nil
}

func (w *world) Replay(id string, after uint64) ([]as.Event, *as.Gap) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []as.Event
	for _, e := range w.events[id] {
		if e.Seq > after && e.Seq >= w.keepFrom[id] {
			out = append(out, e)
		}
	}
	if from := w.keepFrom[id]; from > 1 && after+1 < from {
		return out, &as.Gap{SessionID: id, From: after + 1, To: from - 1}
	}
	return out, nil
}

// add records an event and returns it (the caller publishes it).
func (w *world) add(id, text string) as.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	e := as.Event{SessionID: id, Seq: uint64(len(w.events[id]) + 1), Kind: as.KindMessageChunk, Text: text}
	w.events[id] = append(w.events[id], e)
	return e
}

type logBuf struct {
	mu    sync.Mutex
	lines []string
}

func (l *logBuf) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logBuf) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type env struct {
	srv     *remote.Server
	devices *remote.Devices
	world   *world
	logs    *logBuf
	base    string
	port    string
	hc      *http.Client
}

// leakCheck fails the test if goroutines started during it are still running
// after its cleanups.
func leakCheck(t *testing.T) {
	t.Helper()
	base := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(3 * time.Second)
		for runtime.NumGoroutine() > base {
			if time.Now().After(deadline) {
				buf := make([]byte, 1<<16)
				t.Fatalf("goroutine leak: %d running, %d before\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}

func newEnv(t *testing.T, perMin int, hosts ...string) *env {
	t.Helper()
	leakCheck(t)
	w := &world{events: map[string][]as.Event{}, keepFrom: map[string]uint64{}}
	logs := &logBuf{}
	devices := remote.NewDevices(nil, &fakeSecrets{v: map[string]string{}}, nil)
	srv := remote.New(remote.Options{Devices: devices, Chats: w.Chats, Tasks: w.Tasks, Replay: w.Replay, Logf: logs.logf})
	if err := srv.Start(remote.Config{Host: "127.0.0.1", Port: 0, AllowedHosts: hosts, RequestsPerMinute: perMin}); err != nil {
		t.Fatal(err)
	}
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	t.Cleanup(func() { _ = srv.Close() })
	_, port, _ := net.SplitHostPort(srv.Addr())
	return &env{srv: srv, devices: devices, world: w, logs: logs, base: "http://" + srv.Addr(), port: port, hc: hc}
}

func (e *env) pair(t *testing.T, name string) *client.Client {
	t.Helper()
	p, err := e.devices.BeginPairing()
	if err != nil {
		t.Fatal(err)
	}
	token, dev, err := client.Pair(context.Background(), e.hc, e.base, p.Secret, name)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Name != name || dev.Scopes[0] != remote.ScopeRead {
		t.Fatalf("device %+v", dev)
	}
	return &client.Client{Base: e.base, Token: token, HTTP: e.hc}
}

// request sends a raw request and returns the status.
func (e *env) request(t *testing.T, method, path string, header map[string]string, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, e.base+path, strings.NewReader(body))
	for k, v := range header {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func statusOf(err error) int {
	var se *client.StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

func TestNonLoopbackBindIsRefused(t *testing.T) {
	srv := remote.New(remote.Options{Devices: remote.NewDevices(nil, &fakeSecrets{v: map[string]string{}}, nil)})
	for _, host := range []string{"0.0.0.0", "", "::", "192.168.1.10", "localhost", "example.com"} {
		if err := srv.Start(remote.Config{Host: host, Port: 0}); !errors.Is(err, remote.ErrNotLoopback) {
			t.Errorf("bind %q: %v", host, err)
		}
	}
	if srv.Addr() != "" {
		t.Fatal("listening after refused binds")
	}
}

func TestStopClosesThePortAndStartOpensItAgain(t *testing.T) {
	e := newEnv(t, 120)
	addr := e.srv.Addr()
	e.srv.Stop()
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("port still open after Stop")
	}
	_, port, _ := net.SplitHostPort(addr)
	if err := e.srv.Start(remote.Config{Host: "127.0.0.1", Port: atoi(port), RequestsPerMinute: 120}); err != nil {
		t.Fatal(err)
	}
	if e.srv.Addr() != addr {
		t.Fatalf("restarted on %s, want %s", e.srv.Addr(), addr)
	}
	_ = e.srv.Close()
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("port open after Close")
	}
	if err := e.srv.Start(remote.Config{Host: "127.0.0.1", Port: 0}); err == nil {
		t.Fatal("Start after Close")
	}
}

func atoi(s string) int {
	n := 0
	fmt.Sscan(s, &n)
	return n
}

func TestUnpairedRequestsGet401(t *testing.T) {
	e := newEnv(t, 120)
	for _, path := range []string{"/api/chats", "/api/tasks", "/api/stream", "/api/nope", "/"} {
		if got := e.request(t, http.MethodGet, path, nil, ""); got != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d", path, got)
		}
		if got := e.request(t, http.MethodGet, path, map[string]string{"Authorization": "Bearer 0123456789abcdef.nope"}, ""); got != http.StatusUnauthorized {
			t.Errorf("%s with a bad token: %d", path, got)
		}
	}
	c := e.pair(t, "phone")
	if _, err := c.Chats(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.request(t, http.MethodGet, "/api/nope", map[string]string{"Authorization": "Bearer " + c.Token}, ""); got != http.StatusNotFound {
		t.Errorf("unknown path with a token: %d", got)
	}
	if got := e.request(t, http.MethodPost, "/api/chats", map[string]string{"Authorization": "Bearer " + c.Token}, ""); got != http.StatusMethodNotAllowed {
		t.Errorf("POST chats: %d", got)
	}
}

func TestForeignHostOrOriginGets403(t *testing.T) {
	e := newEnv(t, 600, "laptop.tail1234.ts.net")
	c := e.pair(t, "phone")
	auth := "Bearer " + c.Token
	for name, tc := range map[string]struct {
		h    map[string]string
		want int
	}{
		"loopback":               {map[string]string{}, 200},
		"localhost, other port":  {map[string]string{"Host": "localhost:8000"}, 200}, // ssh -L 8000:127.0.0.1:7419
		"ipv6 loopback":          {map[string]string{"Host": "[::1]:8000"}, 200},
		"allowed host":           {map[string]string{"Host": "Laptop.tail1234.ts.net"}, 200},
		"allowed host origin":    {map[string]string{"Host": "laptop.tail1234.ts.net", "Origin": "https://laptop.tail1234.ts.net"}, 403}, // any Origin: a browser
		"rebinding host":         {map[string]string{"Host": "evil.example:" + e.port}, 403},
		"suffix trick":           {map[string]string{"Host": "laptop.tail1234.ts.net.evil.example"}, 403},
		"foreign origin":         {map[string]string{"Origin": "https://evil.example"}, 403},
		"null origin":            {map[string]string{"Origin": "null"}, 403},
		"origin with userinfo":   {map[string]string{"Origin": "http://127.0.0.1@evil.example"}, 403},
		"non-http origin":        {map[string]string{"Origin": "file://127.0.0.1"}, 403},
		"loopback origin":        {map[string]string{"Origin": "http://127.0.0.1:" + e.port}, 403},
		"rebinding without auth": {map[string]string{"Host": "evil.example", "Authorization": ""}, 403},
	} {
		h := map[string]string{"Authorization": auth}
		for k, v := range tc.h {
			h[k] = v
		}
		if h["Authorization"] == "" {
			delete(h, "Authorization")
		}
		if got := e.request(t, http.MethodGet, "/api/chats", h, ""); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
	// Pairing is checked the same way.
	if got := e.request(t, http.MethodPost, "/api/pair", map[string]string{"Host": "evil.example", "Content-Type": "application/json"}, `{}`); got != 403 {
		t.Errorf("pair from a foreign host: %d", got)
	}
	// The allow-list applies without a restart.
	if err := e.srv.Start(remote.Config{Host: "127.0.0.1", Port: atoi(e.port), RequestsPerMinute: 600}); err != nil {
		t.Fatal(err)
	}
	if got := e.request(t, http.MethodGet, "/api/chats", map[string]string{"Authorization": auth, "Host": "laptop.tail1234.ts.net"}, ""); got != 403 {
		t.Errorf("removed host: %d", got)
	}
}

func TestPairingOverHTTP(t *testing.T) {
	e := newEnv(t, 120)
	ctx := context.Background()
	p, _ := e.devices.BeginPairing()
	if _, _, err := client.Pair(ctx, e.hc, e.base, "WRONGWRONGWRONG1", "phone"); statusOf(err) != 401 {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, _, err := client.Pair(ctx, e.hc, e.base, p.Secret, "bad\nname"); statusOf(err) != 400 {
		t.Fatalf("bad name: %v", err)
	}
	token, _, err := client.Pair(ctx, e.hc, e.base, p.Secret, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Pair(ctx, e.hc, e.base, p.Secret, "again"); statusOf(err) != 401 {
		t.Fatalf("reused secret: %v", err)
	}
	if got := e.request(t, http.MethodGet, "/api/pair", nil, ""); got != 405 {
		t.Errorf("GET pair: %d", got)
	}
	if got := e.request(t, http.MethodPost, "/api/pair", map[string]string{"Content-Type": "text/plain"}, "x"); got != 415 {
		t.Errorf("text body: %d", got)
	}
	big := `{"secret":"` + strings.Repeat("A", 8<<10) + `"}`
	if got := e.request(t, http.MethodPost, "/api/pair", map[string]string{"Content-Type": "application/json"}, big); got != 413 {
		t.Errorf("big body: %d", got)
	}
	// The audit log names the device and operation, never the secret or token.
	logs := e.logs.all()
	if strings.Contains(logs, p.Secret) || strings.Contains(logs, token) || strings.Contains(logs, strings.SplitN(token, ".", 2)[1]) {
		t.Fatalf("secret in the log:\n%s", logs)
	}
	if !strings.Contains(logs, `op=pair status=401`) || !strings.Contains(logs, `("phone") op=pair status=200`) {
		t.Fatalf("audit lines missing:\n%s", logs)
	}
}

func TestRateLimitPerDeviceAndForBadTokens(t *testing.T) {
	e := newEnv(t, 10) // burst 5
	c := e.pair(t, "phone")
	other := e.pair(t, "tablet")
	ctx := context.Background()
	limited := 0
	for range 8 {
		if _, err := c.Chats(ctx); statusOf(err) == 429 {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("no 429 after 8 requests at 10/min")
	}
	if _, err := other.Chats(ctx); err != nil {
		t.Fatalf("another device was limited too: %v", err)
	}
	got := map[int]int{}
	for range 30 {
		got[e.request(t, http.MethodGet, "/api/chats", map[string]string{"Authorization": "Bearer nope"}, "")]++
	}
	if got[429] == 0 || got[401] == 0 {
		t.Fatalf("bad tokens: %v", got)
	}
}

func TestChatsAndTasksAreRedacted(t *testing.T) {
	e := newEnv(t, 120)
	home, _ := os.UserHomeDir()
	e.world.chats = []remote.Chat{{ID: "c1", Agent: "claude", Project: home + "/src/app", State: remote.ChatWaiting, WaitingForApproval: true, Pending: 1}, {ID: "c2", Agent: "codex", Project: "/tmp/x", ReadOnly: true}}
	e.world.tasks = []remote.Task{{ID: "t1", Title: "use key AKIAABCDEFGHIJKLMNOP", State: "working", Changes: &remote.DiffSummary{Files: 2, Added: 3}}}
	c := e.pair(t, "phone")
	chats, err := c.Chats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 || chats[0].Project != "~/src/app" || !chats[0].WaitingForApproval || !chats[1].ReadOnly {
		t.Fatalf("chats %+v", chats)
	}
	tasks, err := c.Tasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || strings.Contains(tasks[0].Title, "AKIAABCDEFGHIJKLMNOP") || tasks[0].Changes.Files != 2 {
		t.Fatalf("tasks %+v", tasks)
	}
}

// next reads stream messages until one of type typ arrives.
func next(t *testing.T, s *client.Stream, typ string) remote.StreamMessage {
	t.Helper()
	_ = s.SetDeadline(time.Now().Add(3 * time.Second))
	for {
		m, err := s.Next()
		if err != nil {
			t.Fatalf("waiting for %s: %v", typ, err)
		}
		if m.Type == typ {
			return m
		}
	}
}

// collect reads messages up to and including "replayed".
func collect(t *testing.T, s *client.Stream) []remote.StreamMessage {
	t.Helper()
	_ = s.SetDeadline(time.Now().Add(3 * time.Second))
	var out []remote.StreamMessage
	for {
		m, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
		if m.Type == "replayed" {
			return out
		}
	}
}

func TestStreamReplaysThenFollowsLive(t *testing.T) {
	e := newEnv(t, 600)
	e.world.chats = []remote.Chat{{ID: "c1", Agent: "claude", Project: "/p", State: remote.ChatWorking}}
	for i := range 5 {
		e.world.add("c1", fmt.Sprintf("part %d", i+1))
	}
	e.world.keepFrom["c1"] = 3 // 1–2 were dropped from the log
	c := e.pair(t, "phone")
	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msgs := collect(t, s)
	if msgs[0].Type != "hello" || len(msgs[0].Chats) != 1 {
		t.Fatalf("first message %+v", msgs[0])
	}
	if msgs[1].Type != "gap" || msgs[1].Gap.From != 1 || msgs[1].Gap.To != 2 {
		t.Fatalf("gap %+v", msgs[1])
	}
	var seqs []uint64
	for _, m := range msgs {
		if m.Type == "agent" {
			seqs = append(seqs, m.Event.Seq)
		}
	}
	if fmt.Sprint(seqs) != "[3 4 5]" {
		t.Fatalf("replayed %v", seqs)
	}
	// Live: an event already replayed is not sent twice; a new one is, redacted.
	e.srv.AgentEvent(as.Event{SessionID: "c1", Seq: 5, Kind: as.KindMessageChunk, Text: "dup"})
	live := e.world.add("c1", "token ghp_abcdefghijklmnopqrstuvwxyz0123456789")
	e.srv.AgentEvent(live)
	m := next(t, s, "agent")
	if m.Event.Seq != 6 || strings.Contains(m.Event.Text, "ghp_") {
		t.Fatalf("live event %+v", m.Event)
	}
	// Chat state and tasks.
	e.world.mu.Lock()
	e.world.chats[0].State, e.world.chats[0].WaitingForApproval = remote.ChatWaiting, true
	e.world.mu.Unlock()
	e.srv.ChatChanged("c1")
	if m := next(t, s, "chat"); m.Chat.State != remote.ChatWaiting || !m.Chat.WaitingForApproval {
		t.Fatalf("chat %+v", m.Chat)
	}
	e.srv.TasksChanged()
	next(t, s, "tasks")
	e.world.mu.Lock()
	e.world.chats = nil
	e.world.mu.Unlock()
	e.srv.ChatChanged("c1")
	if m := next(t, s, "chatClosed"); m.ID != "c1" {
		t.Fatalf("closed %+v", m)
	}
	_ = s.Close()

	// Reconnecting with since continues without a gap or duplicates.
	e.world.mu.Lock()
	e.world.chats = []remote.Chat{{ID: "c1", Agent: "claude", Project: "/p"}}
	e.world.mu.Unlock()
	s2, err := c.Stream(context.Background(), map[string]uint64{"c1": 4})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	seqs = nil
	for _, m := range collect(t, s2) {
		if m.Type == "gap" {
			t.Fatalf("unexpected gap %+v", m.Gap)
		}
		if m.Type == "agent" {
			seqs = append(seqs, m.Event.Seq)
		}
	}
	if fmt.Sprint(seqs) != "[5 6]" {
		t.Fatalf("replay after 4: %v", seqs)
	}
	if !strings.Contains(e.logs.all(), "op=stream.open") {
		t.Fatal("no audit line for the stream")
	}
	if strings.Contains(e.logs.all(), "part 3") {
		t.Fatal("event content in the audit log")
	}
}

func TestStreamRedactsDiffsOfSecretFiles(t *testing.T) {
	e := newEnv(t, 600)
	e.world.chats = []remote.Chat{{ID: "c1"}}
	c := e.pair(t, "phone")
	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	collect(t, s)
	e.srv.AgentEvent(as.Event{SessionID: "c1", Seq: 1, Kind: as.KindToolCall, ToolCall: &as.ToolCall{
		ID: "t", Title: "edit", Diffs: []as.Diff{
			{Path: "/p/.env", OldText: "A=1", NewText: "PASSWORD=hunter2"},
			{Path: "/p/main.go", OldText: "", NewText: `key := "AKIAABCDEFGHIJKLMNOP"`},
		},
	}})
	m := next(t, s, "agent")
	d := m.Event.ToolCall.Diffs
	if strings.Contains(d[0].NewText, "hunter2") || d[0].OldText != "" || strings.Contains(d[1].NewText, "AKIAABCDEFGHIJKLMNOP") {
		t.Fatalf("diffs not redacted: %+v", d)
	}
}

func TestStreamHandshakeIsChecked(t *testing.T) {
	e := newEnv(t, 600)
	c := e.pair(t, "phone")
	// A plain GET with a token is refused with 426 (no WebSocket upgrade).
	if got := e.request(t, http.MethodGet, "/api/stream", map[string]string{"Authorization": "Bearer " + c.Token}, ""); got != http.StatusUpgradeRequired {
		t.Fatalf("plain GET: %d", got)
	}
	if got := e.request(t, http.MethodGet, "/api/stream?since=nocolon", map[string]string{"Authorization": "Bearer " + c.Token}, ""); got != 400 {
		t.Fatalf("bad since: %d", got)
	}
	// Unpaired upgrade.
	bad := &client.Client{Base: e.base, Token: "0123456789abcdef.nope"}
	if _, err := bad.Stream(context.Background(), nil); statusOf(err) != 401 {
		t.Fatalf("unpaired stream: %v", err)
	}
}

func TestRevokeCutsAnOpenStreamWithinASecond(t *testing.T) {
	e := newEnv(t, 600)
	c := e.pair(t, "phone")
	keep := e.pair(t, "tablet")
	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s2, err := keep.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	collect(t, s)
	collect(t, s2)
	var dev remote.Device
	for _, d := range e.devices.List() {
		if d.Name == "phone" {
			dev = d
		}
	}
	start := time.Now()
	if err := e.srv.Revoke(dev.ID); err != nil {
		t.Fatal(err)
	}
	_ = s.SetDeadline(time.Now().Add(time.Second))
	var ce *client.CloseError
	if _, err := s.Next(); !errors.As(err, &ce) || ce.Code != 1008 {
		t.Fatalf("after revoke: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("revoke took %v", time.Since(start))
	}
	if _, err := c.Chats(context.Background()); statusOf(err) != 401 {
		t.Fatalf("revoked token: %v", err)
	}
	// The other device keeps its stream.
	e.srv.TasksChanged()
	next(t, s2, "tasks")
}

func TestStopEndsStreamsWithGoingAway(t *testing.T) {
	e := newEnv(t, 600)
	c := e.pair(t, "phone")
	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	collect(t, s)
	done := make(chan struct{})
	go func() { e.srv.Stop(); close(done) }()
	_ = s.SetDeadline(time.Now().Add(2 * time.Second))
	var ce *client.CloseError
	if _, err := s.Next(); !errors.As(err, &ce) || ce.Code != 1001 {
		t.Fatalf("after stop: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return")
	}
	if e.srv.Streams() != 0 {
		t.Fatal("streams left after Stop")
	}
}

func TestSlowStreamIsClosedNotTheDesktop(t *testing.T) {
	e := newEnv(t, 600)
	e.world.chats = []remote.Chat{{ID: "c1"}}
	c := e.pair(t, "phone")
	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	collect(t, s)
	// The client stops reading; publishing must never block.
	big := strings.Repeat("x", 32<<10)
	start := time.Now()
	for i := range 5000 {
		e.srv.AgentEvent(as.Event{SessionID: "c1", Seq: uint64(i + 1), Kind: as.KindMessageChunk, Text: big})
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("publishing 5000 events took %v", d)
	}
	// Eventually the stream is closed with "try again later".
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	for {
		_, err := s.Next()
		var ce *client.CloseError
		if errors.As(err, &ce) {
			if ce.Code != 1013 {
				t.Fatalf("close code %d", ce.Code)
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStreamsPerDeviceAreBounded(t *testing.T) {
	e := newEnv(t, 600)
	c := e.pair(t, "phone")
	var open []*client.Stream
	defer func() {
		for _, s := range open {
			_ = s.Close()
		}
	}()
	for range 4 {
		s, err := c.Stream(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		open = append(open, s)
	}
	if _, err := c.Stream(context.Background(), nil); statusOf(err) != 429 {
		t.Fatalf("fifth stream: %v", err)
	}
}

func TestMalformedClientFrameEndsTheStream(t *testing.T) {
	e := newEnv(t, 600)
	c := e.pair(t, "phone")
	conn, err := net.Dial("tcp", e.srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "GET /api/stream HTTP/1.1\r\nHost: 127.0.0.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nAuthorization: Bearer " + c.Token + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// An unmasked text frame: the server answers 1002 and closes.
	if _, err := conn.Write([]byte{0x81, 0x02, 'h', 'i'}); err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(conn)
	i := bytes.LastIndexByte(data, 0x88)
	if !bytes.Contains(data, []byte("101 Switching Protocols")) || i < 0 || len(data) < i+4 || !bytes.Equal(data[i+2:i+4], []byte{0x03, 0xea}) {
		t.Fatalf("server answer %q", data)
	}
	waitFor(t, func() bool { return e.srv.Streams() == 0 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
