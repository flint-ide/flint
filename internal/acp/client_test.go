package acp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordedUpdate struct {
	SessionID string
	Kind      string
	Text      string
}

// testHandler records everything the agent asks of the client.
type testHandler struct {
	mu      sync.Mutex
	updates []recordedUpdate
	files   map[string]string
	writes  []WriteTextFileRequest
	perms   []PermissionRequest

	decide    func(ctx context.Context, req PermissionRequest) (PermissionOutcome, error)
	permAsked chan struct{}
	onUpdate  func(recordedUpdate)
}

func newTestHandler() *testHandler {
	return &testHandler{files: map[string]string{}, permAsked: make(chan struct{}, 8)}
}

func (h *testHandler) SessionUpdate(sessionID string, update json.RawMessage) {
	var u struct {
		SessionUpdate string `json:"sessionUpdate"`
		Content       struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(update, &u)
	rec := recordedUpdate{SessionID: sessionID, Kind: u.SessionUpdate, Text: u.Content.Text}
	h.mu.Lock()
	h.updates = append(h.updates, rec)
	onUpdate := h.onUpdate
	h.mu.Unlock()
	if onUpdate != nil {
		onUpdate(rec)
	}
}

func (h *testHandler) RequestPermission(ctx context.Context, req PermissionRequest) (PermissionOutcome, error) {
	h.mu.Lock()
	h.perms = append(h.perms, req)
	decide := h.decide
	h.mu.Unlock()
	h.permAsked <- struct{}{}
	if decide == nil {
		return Selected(req.Options[0].OptionID), nil
	}
	return decide(ctx, req)
}

func (h *testHandler) ReadTextFile(_ context.Context, req ReadTextFileRequest) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	content, ok := h.files[req.Path]
	if !ok {
		return "", os.ErrNotExist
	}
	return ApplyLineWindow(content, req), nil
}

func (h *testHandler) WriteTextFile(_ context.Context, req WriteTextFileRequest) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.writes = append(h.writes, req)
	h.files[req.Path] = req.Content
	return nil
}

// messages concatenates agent_message_chunk texts for a session.
func (h *testHandler) messages(sessionID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b strings.Builder
	for _, u := range h.updates {
		if u.SessionID == sessionID && u.Kind == "agent_message_chunk" {
			b.WriteString(u.Text)
		}
	}
	return b.String()
}

func (h *testHandler) kinds(sessionID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, u := range h.updates {
		if u.SessionID == sessionID {
			out = append(out, u.Kind)
		}
	}
	return out
}

func fakeOptions(t *testing.T, mode string) Options {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return Options{
		Command:       exe,
		Env:           []string{fakeAgentEnv + "=1", fakeModeEnv + "=" + mode},
		ClientName:    "flint-test",
		ClientVersion: "0.1.0",
	}
}

// startFake starts the fake agent and registers cleanup that closes it and
// checks that no goroutines leaked.
func startFake(t *testing.T, mode string, h Handler) *Client {
	t.Helper()
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, fakeOptions(t, mode), h)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		assertNoLeaks(t, baseline)
	})
	return c
}

func assertNoLeaks(t *testing.T, baseline int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Fatalf("goroutine leak: %d running, baseline %d\n%s", runtime.NumGoroutine(), baseline, buf[:n])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newSession(t *testing.T, c *Client) string {
	t.Helper()
	res, err := c.NewSession(testCtx(t), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return res.SessionID
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestInitializeAndSessionSetup(t *testing.T) {
	h := newTestHandler()
	c := startFake(t, "", h)
	ctx := testCtx(t)

	info := c.Info()
	if info.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d", info.ProtocolVersion)
	}
	caps := info.AgentCapabilities
	if !caps.LoadSession || !caps.PromptCapabilities.EmbeddedContext || !caps.PromptCapabilities.Image ||
		caps.PromptCapabilities.Audio || !caps.MCPCapabilities.HTTP {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
	if len(info.AuthMethods) != 1 || info.AuthMethods[0].ID != "agent-login" ||
		info.AuthMethods[0].Description != "Use your account" || len(info.AuthMethods[0].Raw) == 0 {
		t.Errorf("unexpected auth methods: %+v", info.AuthMethods)
	}
	if info.AgentInfo == nil || info.AgentInfo.Name != "fake-agent" || info.AgentInfo.Version != "1.2.3" {
		t.Errorf("unexpected agentInfo: %+v", info.AgentInfo)
	}
	if len(info.Raw) == 0 {
		t.Error("Raw initialize response missing")
	}

	if err := c.Authenticate(ctx, "agent-login"); err != nil {
		t.Errorf("Authenticate: %v", err)
	}
	var rpcErr *RPCError
	if err := c.Authenticate(ctx, "nope"); !errors.As(err, &rpcErr) || rpcErr.Code != CodeInvalidParams {
		t.Errorf("Authenticate(bad) = %v, want RPC -32602", err)
	}

	res, err := c.NewSession(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if res.SessionID != "sess_1" || res.Modes == nil || res.Modes.CurrentModeID != "default" ||
		len(res.Modes.AvailableModes) != 2 || res.Modes.AvailableModes[1].Description != "Plan only" {
		t.Errorf("unexpected session: %+v modes=%+v", res, res.Modes)
	}
	if _, err := c.NewSession(ctx, "relative/dir", nil); err == nil {
		t.Error("NewSession with relative cwd should fail")
	}

	if err := c.SetMode(ctx, res.SessionID, "plan"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if kinds := h.kinds(res.SessionID); len(kinds) != 1 || kinds[0] != "current_mode_update" {
		t.Errorf("updates after SetMode = %v, want [current_mode_update]", kinds)
	}
	if err := c.SetMode(ctx, res.SessionID, "bogus"); !errors.As(err, &rpcErr) {
		t.Errorf("SetMode(bogus) = %v, want RPC error", err)
	}
	if c.Err() != nil {
		t.Errorf("Err() while running = %v", c.Err())
	}
}

func TestPromptStreamsUpdatesInOrder(t *testing.T) {
	h := newTestHandler()
	c := startFake(t, "", h)
	sid := newSession(t, c)

	stop, err := c.Prompt(testCtx(t), sid, []ContentBlock{TextBlock("stream")})
	if err != nil || stop != StopEndTurn {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	want := []string{"agent_thought_chunk", "agent_message_chunk", "agent_message_chunk", "plan"}
	if got := h.kinds(sid); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("update kinds = %v, want %v", got, want)
	}
	if got := h.messages(sid); got != "Hello world" {
		t.Errorf("message = %q", got)
	}
}

func TestPromptFullTurn(t *testing.T) {
	cases := []struct {
		name       string
		decide     func(context.Context, PermissionRequest) (PermissionOutcome, error)
		wantMsg    string
		wantWrites int
	}{
		{
			name: "allow",
			decide: func(context.Context, PermissionRequest) (PermissionOutcome, error) {
				return Selected("allow-once"), nil
			},
			wantMsg:    "read:line2\nwrote",
			wantWrites: 1,
		},
		{
			name: "reject",
			decide: func(context.Context, PermissionRequest) (PermissionOutcome, error) {
				return Selected("reject-once"), nil
			},
			wantMsg: "read:line2\nrejected:selected:reject-once",
		},
		{
			name:    "option not offered",
			decide:  func(context.Context, PermissionRequest) (PermissionOutcome, error) { return Selected("bogus"), nil },
			wantMsg: "read:line2\nperm-error:-32603",
		},
		{
			name: "handler error",
			decide: func(context.Context, PermissionRequest) (PermissionOutcome, error) {
				return PermissionOutcome{}, errors.New("ui gone")
			},
			wantMsg: "read:line2\nperm-error:-32603",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler()
			h.decide = tc.decide
			path := filepath.Join(t.TempDir(), "main.go")
			h.files[path] = "line1\nline2\nline3\n"
			c := startFake(t, "", h)
			sid := newSession(t, c)

			prompt := []ContentBlock{TextBlock("full"), ResourceLinkBlock("file://"+path, "main.go")}
			stop, err := c.Prompt(testCtx(t), sid, prompt)
			if err != nil || stop != StopEndTurn {
				t.Fatalf("Prompt = %q, %v", stop, err)
			}
			if got := h.messages(sid); got != tc.wantMsg {
				t.Errorf("messages = %q, want %q", got, tc.wantMsg)
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.writes) != tc.wantWrites {
				t.Fatalf("writes = %+v", h.writes)
			}
			if tc.wantWrites > 0 && (h.writes[0].Content != "patched\n" || h.writes[0].Path != path || h.writes[0].SessionID != sid) {
				t.Errorf("write = %+v", h.writes[0])
			}
			if len(h.perms) != 1 {
				t.Fatalf("perms = %+v", h.perms)
			}
			perm := h.perms[0]
			if perm.SessionID != sid || perm.ToolCall.ToolCallID != "call_1" || perm.ToolCall.Title != "Edit file" ||
				perm.ToolCall.Kind != "edit" || len(perm.ToolCall.Raw) == 0 || len(perm.Options) != 2 ||
				perm.Options[0].Kind != PermissionAllowOnce || perm.Options[1].Kind != PermissionRejectOnce {
				t.Errorf("permission request = %+v", perm)
			}
		})
	}
}

func TestPromptCancellation(t *testing.T) {
	h := newTestHandler()
	h.decide = func(ctx context.Context, _ PermissionRequest) (PermissionOutcome, error) {
		<-ctx.Done()
		return PermissionOutcome{}, ctx.Err()
	}
	c := startFake(t, "", h)
	sid := newSession(t, c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		stop string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		stop, err := c.Prompt(ctx, sid, []ContentBlock{TextBlock("cancel")})
		done <- result{stop, err}
	}()
	select {
	case <-h.permAsked:
	case <-time.After(10 * time.Second):
		t.Fatal("permission was never requested")
	}
	cancel()
	select {
	case r := <-done:
		if r.err != nil || r.stop != StopCancelled {
			t.Fatalf("Prompt = %q, %v; want cancelled", r.stop, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Prompt did not return after cancel")
	}
	if got := h.messages(sid); got != "perm:cancelled late:cancelled" {
		t.Errorf("messages = %q", got)
	}
	h.mu.Lock()
	asked := len(h.perms)
	h.mu.Unlock()
	if asked != 1 {
		t.Errorf("handler asked %d times; the post-cancel request should be auto-cancelled", asked)
	}

	// The session is usable for another turn afterwards.
	if stop, err := c.Prompt(testCtx(t), sid, []ContentBlock{TextBlock("stream")}); err != nil || stop != StopEndTurn {
		t.Errorf("follow-up Prompt = %q, %v", stop, err)
	}
}

func TestAgentCancelRequest(t *testing.T) {
	h := newTestHandler()
	h.decide = func(ctx context.Context, _ PermissionRequest) (PermissionOutcome, error) {
		<-ctx.Done()
		return PermissionOutcome{}, ctx.Err()
	}
	c := startFake(t, "", h)
	sid := newSession(t, c)
	if _, err := c.Prompt(testCtx(t), sid, []ContentBlock{TextBlock("cancelreq")}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got := h.messages(sid); got != "cancelreq:-32800" {
		t.Errorf("messages = %q", got)
	}
}

func TestUnsupportedAgentRequests(t *testing.T) {
	h := newTestHandler()
	c := startFake(t, "", h)
	sid := newSession(t, c)
	if _, err := c.Prompt(testCtx(t), sid, []ContentBlock{TextBlock("unknown")}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got, want := h.messages(sid), "codes:-32601,-32601,-32602,-32602 parse:-32700"; got != want {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

func TestLargeMessages(t *testing.T) {
	h := newTestHandler()
	c := startFake(t, "", h)
	sid := newSession(t, c)

	big := strings.Repeat("0123456789abcdef\n", 5<<20/17+1) // ~5 MB with newlines
	prompt := []ContentBlock{TextBlock("big"), TextResourceBlock("file:///big.txt", "text/plain", big)}
	stop, err := c.Prompt(testCtx(t), sid, prompt)
	if err != nil || stop != StopEndTurn {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	if got := h.messages(sid); got != big {
		t.Errorf("echoed %d bytes, want %d", len(got), len(big))
	}
}

func TestConcurrentSessions(t *testing.T) {
	h := newTestHandler()
	c := startFake(t, "", h)
	ids := []string{newSession(t, c), newSession(t, c), newSession(t, c)}

	var wg sync.WaitGroup
	errs := make(chan error, len(ids))
	for _, sid := range ids {
		wg.Go(func() {
			if _, err := c.Prompt(testCtx(t), sid, []ContentBlock{TextBlock("stream")}); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Prompt: %v", err)
	}
	for _, sid := range ids {
		if got := h.messages(sid); got != "Hello world" {
			t.Errorf("session %s messages = %q", sid, got)
		}
	}
}

func TestAgentCrashMidPrompt(t *testing.T) {
	h := newTestHandler()
	c := startFake(t, "", h)
	sid := newSession(t, c)

	_, err := c.Prompt(testCtx(t), sid, []ContentBlock{TextBlock("crash")})
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("Prompt error = %v, want one quoting stderr", err)
	}
	var exitErr *AgentExitedError
	if !errors.As(err, &exitErr) || exitErr.Err == nil {
		t.Fatalf("error %v is not an AgentExitedError with exit status", err)
	}
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("Done not closed after crash")
	}
	if !errors.As(c.Err(), &exitErr) {
		t.Errorf("Err() = %v", c.Err())
	}
	if !strings.Contains(c.Stderr(), "kaboom") {
		t.Errorf("Stderr() = %q", c.Stderr())
	}
	if got := h.messages(sid); got != "about to crash" {
		t.Errorf("updates before crash lost: %q", got)
	}
	if _, err := c.NewSession(testCtx(t), t.TempDir(), nil); !errors.As(err, &exitErr) {
		t.Errorf("call after crash = %v", err)
	}
}

func TestCloseWithPendingPrompt(t *testing.T) {
	baseline := runtime.NumGoroutine()
	h := newTestHandler()
	hanging := make(chan struct{}, 1)
	h.onUpdate = func(u recordedUpdate) {
		if u.Text == "hanging" {
			hanging <- struct{}{}
		}
	}
	c, err := Start(testCtx(t), fakeOptions(t, ""), h)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	sid := newSession(t, c)
	done := make(chan error, 1)
	go func() {
		_, err := c.Prompt(context.Background(), sid, []ContentBlock{TextBlock("hang")})
		done <- err
	}()
	select {
	case <-hanging:
	case <-time.After(10 * time.Second):
		t.Fatal("agent never started the turn")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("pending Prompt error = %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending Prompt not unblocked by Close")
	}
	if !errors.Is(c.Err(), ErrClosed) {
		t.Errorf("Err() = %v", c.Err())
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := c.NewSession(testCtx(t), t.TempDir(), nil); !errors.Is(err, ErrClosed) {
		t.Errorf("call after Close = %v", err)
	}
	if err := c.Cancel(sid); !errors.Is(err, ErrClosed) {
		t.Errorf("Cancel after Close = %v", err)
	}
	assertNoLeaks(t, baseline)
}

func TestStartFailures(t *testing.T) {
	t.Run("nil handler", func(t *testing.T) {
		if _, err := Start(context.Background(), fakeOptions(t, ""), nil); err == nil {
			t.Error("want error")
		}
	})
	t.Run("missing command", func(t *testing.T) {
		_, err := Start(context.Background(), Options{Command: "flint-no-such-agent-xyz"}, newTestHandler())
		if err == nil {
			t.Error("want error")
		}
	})
	t.Run("protocol version mismatch", func(t *testing.T) {
		baseline := runtime.NumGoroutine()
		_, err := Start(testCtx(t), fakeOptions(t, "badversion"), newTestHandler())
		if err == nil || !strings.Contains(err.Error(), "unsupported protocol version 99") {
			t.Errorf("err = %v", err)
		}
		assertNoLeaks(t, baseline)
	})
	t.Run("initialize timeout", func(t *testing.T) {
		baseline := runtime.NumGoroutine()
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err := Start(ctx, fakeOptions(t, "noinit"), newTestHandler())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want deadline exceeded", err)
		}
		assertNoLeaks(t, baseline)
	})
}

func TestCallWithCancelledContext(t *testing.T) {
	c := startFake(t, "", newTestHandler())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prompt(ctx, "sess_x", []ContentBlock{TextBlock("stream")}); !errors.Is(err, context.Canceled) {
		t.Errorf("Prompt = %v", err)
	}
	if err := c.SetMode(ctx, "sess_x", "plan"); !errors.Is(err, context.Canceled) {
		t.Errorf("SetMode = %v", err)
	}
}
