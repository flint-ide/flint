package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/remote"
	"flint/internal/remote/client"
	"flint/internal/settings"
)

// scriptedAgent is a fake agent driver: each prompt streams a few chunks
// (one with a credential), asks to run a command and waits for release.
type scriptedAgent struct {
	mu      sync.Mutex
	sink    as.Sink
	release chan struct{}
}

func (f *scriptedAgent) Start(_ context.Context, cwd string, sink as.Sink) (as.SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sink = sink
	return as.SessionInfo{ID: "chat-1", Cwd: cwd}, nil
}

func (f *scriptedAgent) Prompt(ctx context.Context, id, text string, _ []as.Attachment) (string, error) {
	f.mu.Lock()
	sink, release := f.sink, f.release
	f.mu.Unlock()
	sink(as.Event{SessionID: id, Kind: as.KindUserMessage, Text: text})
	sink(as.Event{SessionID: id, Kind: as.KindMessageChunk, Text: "Looking at the config. The key is AKIAABCDEFGHIJKLMNOP."})
	sink(as.Event{SessionID: id, Kind: as.KindPermission, Permission: &as.PermissionRequest{
		ID: "p1", ToolCall: as.ToolCall{ID: "t1", Kind: as.ToolExecute, Title: "go test ./..."},
		Options: []as.PermissionOption{{ID: "yes", Name: "Allow", Kind: as.AllowOnce}, {ID: "no", Name: "Reject", Kind: as.RejectOnce}},
	}})
	select {
	case <-release:
	case <-ctx.Done():
		sink(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopCancelled})
		return as.StopCancelled, nil
	}
	sink(as.Event{SessionID: id, Kind: as.KindPermissionSettled, RequestID: "p1"})
	sink(as.Event{SessionID: id, Kind: as.KindMessageChunk, Text: "All tests pass."})
	sink(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	return as.StopEndTurn, nil
}

func (f *scriptedAgent) Cancel(string) error                  { return nil }
func (f *scriptedAgent) Respond(string, string, string) error { return nil }
func (f *scriptedAgent) Stop(string) error                    { return nil }
func (f *scriptedAgent) Close() error                         { return nil }

// fakeKeychain stands in for the Keychain (tests never touch the real one).
type fakeKeychain struct {
	mu sync.Mutex
	v  map[string]string
}

func (k *fakeKeychain) Get(a string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.v[a], nil
}

func (k *fakeKeychain) Set(a, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.v[a] = v
	return nil
}

func (k *fakeKeychain) Delete(a string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.v, a)
	return nil
}

// freePort finds a free port in 34160–34169 (this test's own range).
func freePort(t *testing.T) int {
	t.Helper()
	for p := 34160; p <= 34169; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(p)))
		if err == nil {
			_ = ln.Close()
			return p
		}
	}
	t.Skip("no free port in 34160–34169")
	return 0
}

func itoa(n int) string { return strconv.Itoa(n) }

type remoteFixture struct {
	app     *App
	api     *RemoteAPI
	agent   *scriptedAgent
	keys    *fakeKeychain
	port    int
	hc      *http.Client
	project string
}

func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	a := &App{configDir: t.TempDir(), settings: settings.Default(), ctx: context.Background()}
	a.tasks = agenttask.NewTracker(a.taskByFolder, a.taskChanged)
	a.agents = as.NewManager(func(e as.Event) {
		a.emit(eventAgentEvent, e)
		a.tasks.Observe(e)
	})
	agent := &scriptedAgent{release: make(chan struct{})}
	a.agents.Register("fake", agent)
	keys := &fakeKeychain{v: map[string]string{}}
	a.remote, a.remoteDevices = newRemote(a, keys)
	t.Cleanup(func() {
		_ = a.remote.Close()
		_ = a.agents.Close()
	})
	return &remoteFixture{
		app: a, api: &RemoteAPI{app: a}, agent: agent, keys: keys, port: freePort(t),
		hc: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}, project: t.TempDir(),
	}
}

func (f *remoteFixture) enable(t *testing.T, on bool) {
	t.Helper()
	_, err := f.app.updateSettings(func(s settings.Settings) settings.Settings {
		s.Remote.Enabled, s.Remote.Port, s.Remote.ReplayEvents = on, f.port, 100
		return s
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *remoteFixture) base() string { return "http://127.0.0.1:" + itoa(f.port) }

// pair pairs a phone the way the app will: read the link, POST the secret.
func (f *remoteFixture) pair(t *testing.T) *client.Client {
	t.Helper()
	p, err := f.api.Pair("")
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(p.Link)
	if err != nil || link.Scheme != "flint-remote" || link.Host != "pair" || p.QR == "" || p.QRSize < 21 {
		t.Fatalf("pairing %+v", p)
	}
	q := link.Query()
	if q.Get("url") != f.base() || strings.ReplaceAll(p.Secret, "-", "") != q.Get("secret") {
		t.Fatalf("link %q, secret %q", p.Link, p.Secret)
	}
	token, _, err := client.Pair(context.Background(), f.hc, q.Get("url"), p.Secret, "Test iPhone")
	if err != nil {
		t.Fatal(err)
	}
	return &client.Client{Base: f.base(), Token: token, HTTP: f.hc}
}

func readUntil(t *testing.T, s *client.Stream, match func(remote.StreamMessage) bool) []remote.StreamMessage {
	t.Helper()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	var out []remote.StreamMessage
	for {
		m, err := s.Next()
		if err != nil {
			t.Fatalf("stream: %v (after %d messages)", err, len(out))
		}
		out = append(out, m)
		if match(m) {
			return out
		}
	}
}

// A paired client follows a live chat with replay; the setting closes the port.
func TestRemoteFollowsALiveChatWithReplay(t *testing.T) {
	f := newRemoteFixture(t)
	if _, err := f.api.Pair(""); err == nil {
		t.Fatal("pairing while Flint Remote is off")
	}
	f.enable(t, true)
	if st := f.api.Status(); !st.Enabled || !st.Listening || st.Error != "" {
		t.Fatalf("status %+v", st)
	}

	// The chat starts before the phone connects: its first events are replayed.
	info, err := f.app.agents.Start(context.Background(), "fake", f.project)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.agents.Prompt(info.ID, "run the tests", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { s := f.app.agents.Sessions(); return len(s) == 1 && s[0].Pending == 1 })

	c := f.pair(t)
	chats, err := c.Chats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].ID != "chat-1" || chats[0].State != remote.ChatWaiting || !chats[0].WaitingForApproval || !chats[0].ReadOnly {
		t.Fatalf("chats %+v", chats)
	}

	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	replay := readUntil(t, s, func(m remote.StreamMessage) bool { return m.Type == "replayed" })
	var kinds []string
	for _, m := range replay {
		if m.Type == "gap" {
			t.Fatalf("unexpected gap %+v", m.Gap)
		}
		if m.Type == "agent" {
			kinds = append(kinds, m.Event.Kind)
			if strings.Contains(m.Event.Text, "AKIAABCDEFGHIJKLMNOP") {
				t.Fatal("credential sent to the phone")
			}
		}
	}
	if strings.Join(kinds, ",") != "user_message,message_chunk,permission_request" {
		t.Fatalf("replayed %v", kinds)
	}

	// Live: the turn finishes; the phone sees the events and the state change.
	close(f.agent.release)
	live := readUntil(t, s, func(m remote.StreamMessage) bool { return m.Type == "agent" && m.Event.Kind == as.KindTurnEnd })
	seen := map[string]bool{}
	for _, m := range live {
		if m.Type == "agent" {
			seen[m.Event.Kind] = true
		}
	}
	if !seen[as.KindPermissionSettled] || !seen[as.KindMessageChunk] {
		t.Fatalf("live events %v", seen)
	}
	readUntil(t, s, func(m remote.StreamMessage) bool { return m.Type == "chat" && m.Chat.State == remote.ChatIdle })

	// A reconnect after the last Seq replays nothing.
	last := f.app.agents.Sessions()[0].LastSeq
	_ = s.Close()
	s2, err := c.Stream(context.Background(), map[string]uint64{"chat-1": last})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	for _, m := range readUntil(t, s2, func(m remote.StreamMessage) bool { return m.Type == "replayed" }) {
		if m.Type == "agent" || m.Type == "gap" {
			t.Fatalf("replayed after the last seq: %+v", m)
		}
	}

	// Tasks changing reaches the stream through App.emit.
	f.app.emit(eventTasksChanged)
	readUntil(t, s2, func(m remote.StreamMessage) bool { return m.Type == "tasks" })

	// Switching the setting off closes the stream and the port.
	f.enable(t, false)
	_ = s2.SetDeadline(time.Now().Add(3 * time.Second))
	var ce *client.CloseError
	for {
		if _, err := s2.Next(); err != nil {
			if !errors.As(err, &ce) || ce.Code != 1001 {
				t.Fatalf("after switching off: %v", err)
			}
			break
		}
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(f.port), time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("port open with the setting off")
	}
	if events, gap := f.app.agents.Replay("chat-1", 0); len(events) != 0 || gap == nil {
		t.Fatal("the event log is kept with Flint Remote off")
	}
}

func TestRemoteDevicesAreStoredAsHashesAndRevoked(t *testing.T) {
	f := newRemoteFixture(t)
	f.enable(t, true)
	c := f.pair(t)
	secret := strings.SplitN(c.Token, ".", 2)[1]
	st := f.api.Status()
	if len(st.Devices) != 1 || st.Devices[0].Name != "Test iPhone" {
		t.Fatalf("devices %+v", st.Devices)
	}
	// state.json has the device, not the token; the Keychain has a hash.
	f.app.stateMu.Lock()
	saved := f.app.state.RemoteDevices
	f.app.stateMu.Unlock()
	if len(saved) != 1 || saved[0].ID != st.Devices[0].ID {
		t.Fatalf("state %+v", saved)
	}
	for _, v := range f.keys.v {
		if strings.Contains(v, secret) || len(v) != 64 {
			t.Fatalf("keychain value %q is not a hash", v)
		}
	}
	s, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	readUntil(t, s, func(m remote.StreamMessage) bool { return m.Type == "replayed" })
	if err := f.api.Revoke("not-an-id"); err == nil {
		t.Fatal("invalid id accepted")
	}
	start := time.Now()
	if err := f.api.Revoke(st.Devices[0].ID); err != nil {
		t.Fatal(err)
	}
	_ = s.SetDeadline(time.Now().Add(time.Second))
	var ce *client.CloseError
	if _, err := s.Next(); !errors.As(err, &ce) || ce.Code != 1008 || time.Since(start) > time.Second {
		t.Fatalf("after revoke: %v in %v", err, time.Since(start))
	}
	if _, err := c.Chats(context.Background()); err == nil {
		t.Fatal("revoked token still works")
	}
	if len(f.keys.v) != 0 || len(f.api.Status().Devices) != 0 {
		t.Fatal("revoke left the device")
	}
}

func TestRemotePairingHostMustBeAllowed(t *testing.T) {
	f := newRemoteFixture(t)
	_, err := f.app.updateSettings(func(s settings.Settings) settings.Settings {
		s.Remote.Enabled, s.Remote.Port, s.Remote.AllowedHosts = true, f.port, []string{"laptop.tail1234.ts.net"}
		return s
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.Pair("evil.example"); err == nil {
		t.Fatal("pairing link for a host that isn't allowed")
	}
	p, err := f.api.Pair("laptop.tail1234.ts.net")
	if err != nil || p.URL != "https://laptop.tail1234.ts.net" {
		t.Fatalf("pairing %+v, %v", p, err)
	}
}

func TestRemotePortInUseIsReported(t *testing.T) {
	f := newRemoteFixture(t)
	ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(f.port))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f.enable(t, true)
	if st := f.api.Status(); st.Listening || st.Error == "" {
		t.Fatalf("status %+v", st)
	}
}

func TestRemoteTasksListTheWorkspaceTasksWithTheirChanges(t *testing.T) {
	tf := newTaskFixture(t)
	v := tf.create("Add a Retry button")
	if err := os.WriteFile(filepath.Join(v.Worktree, "b.txt"), []byte("two\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := tf.app
	a.agents = as.NewManager(func(as.Event) {})
	a.remote, a.remoteDevices = newRemote(a, &fakeKeychain{v: map[string]string{}})
	tasks, err := a.remoteTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != v.ID || tasks[0].Title != "Add a Retry button" || tasks[0].Changes == nil || tasks[0].Changes.Files != 1 || tasks[0].Changes.Added != 2 {
		t.Fatalf("tasks %+v", tasks)
	}

	// The list is reused for a while (each one runs git per task)…
	if err := os.WriteFile(filepath.Join(v.Worktree, "c.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again, _ := a.remoteTasks(context.Background()); again[0].Changes.Files != 1 {
		t.Fatalf("not reused: %+v", again[0].Changes)
	}
	// …until git or the tasks change.
	a.emit(eventGitChanged)
	if again, _ := a.remoteTasks(context.Background()); again[0].Changes.Files != 2 {
		t.Fatalf("not recomputed after git:changed: %+v", again[0].Changes)
	}
	// The request's context bounds the work.
	a.emit(eventTasksChanged)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.remoteTasks(ctx); err == nil {
		t.Fatal("a cancelled request computed the list")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
