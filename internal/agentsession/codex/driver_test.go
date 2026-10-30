package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/agentsession"
	"flint/internal/shellenv"
)

func TestMain(m *testing.M) {
	if script := os.Getenv(envFakeScript); script != "" {
		os.Exit(runFakeServer(script))
	}
	shellenv.Wait() // settle the PATH import so goroutine counts are stable
	os.Exit(m.Run())
}

// recorder is a Sink that keeps every event and can react to them.
type recorder struct {
	mu     sync.Mutex
	events []agentsession.Event
	notify chan struct{}
	react  func(agentsession.Event)
}

func newRecorder() *recorder { return &recorder{notify: make(chan struct{}, 1)} }

func (r *recorder) sink(ev agentsession.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	react := r.react
	r.mu.Unlock()
	if react != nil {
		react(ev)
	}
	wake(r.notify)
}

func (r *recorder) setReact(fn func(agentsession.Event)) {
	r.mu.Lock()
	r.react = fn
	r.mu.Unlock()
}

func (r *recorder) all() []agentsession.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agentsession.Event(nil), r.events...)
}

func (r *recorder) ofKind(kind string) []agentsession.Event {
	var out []agentsession.Event
	for _, ev := range r.all() {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func (r *recorder) text(kind string) string {
	var b strings.Builder
	for _, ev := range r.ofKind(kind) {
		b.WriteString(ev.Text)
	}
	return b.String()
}

// waitFor blocks until an event of kind arrives.
func (r *recorder) waitFor(t *testing.T, kind string) agentsession.Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if evs := r.ofKind(kind); len(evs) > 0 {
			return evs[0]
		}
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("timed out waiting for %s; got %v", kind, kinds(r.all()))
		}
	}
}

func kinds(evs []agentsession.Event) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Kind
	}
	return out
}

// toolCalls folds tool_call and tool_call_update events into final states.
func (r *recorder) toolCalls() map[string]agentsession.ToolCall {
	out := map[string]agentsession.ToolCall{}
	for _, ev := range r.all() {
		if ev.ToolCall == nil {
			continue
		}
		tc := out[ev.ToolCall.ID]
		u := ev.ToolCall
		tc.ID = u.ID
		for dst, src := range map[*string]string{&tc.Title: u.Title, &tc.Kind: u.Kind, &tc.Status: u.Status, &tc.Input: u.Input, &tc.Output: u.Output} {
			if src != "" {
				*dst = src
			}
		}
		if u.Diffs != nil {
			tc.Diffs = u.Diffs
		}
		if u.Paths != nil {
			tc.Paths = u.Paths
		}
		out[u.ID] = tc
	}
	return out
}

type harness struct {
	d   *Driver
	rec *recorder
	cwd string
	log string
}

// newHarness starts a Driver whose app-server is this test binary replaying
// testdata/<fixture>.
func newHarness(t *testing.T, fixture string, files map[string]string) *harness {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(t.TempDir(), "client.jsonl")
	d := New(Options{
		Binary: exe,
		Config: []string{`model_reasoning_effort="low"`},
		Env:    []string{envFakeScript + "=" + script, envFakeLog + "=" + logPath},
	})
	t.Cleanup(func() { _ = d.Close() })
	return &harness{d: d, rec: newRecorder(), cwd: cwd, log: logPath}
}

func (h *harness) start(t *testing.T) agentsession.SessionInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := h.d.Start(ctx, h.cwd, h.rec.sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return info
}

func (h *harness) prompt(t *testing.T, sid, text string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return h.d.Prompt(ctx, sid, text, nil)
}

// clientLog returns what the driver sent to the fake server.
func (h *harness) clientLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(h.log)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertLastIsTurnEnd(t *testing.T, evs []agentsession.Event, stop string) {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	last := evs[len(evs)-1]
	if last.Kind != agentsession.KindTurnEnd || last.StopReason != stop {
		t.Fatalf("last event = %s/%s, want turn_end/%s; all: %v", last.Kind, last.StopReason, stop, kinds(evs))
	}
}

func TestTextTurnThenEditAndCommand(t *testing.T) {
	h := newHarness(t, "text_two_turns.jsonl", map[string]string{"a.txt": "hello\n"})
	info := h.start(t)
	if info.Agent != "codex" || info.Model != "gpt-5.6-luna" || info.ID == "" || info.Cwd != h.cwd {
		t.Fatalf("unexpected session info %+v", info)
	}

	stop, err := h.prompt(t, info.ID, "Reply with exactly the word: pong")
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("turn 1: stop=%q err=%v", stop, err)
	}
	if got := h.rec.text(agentsession.KindMessageChunk); got != "pong" {
		t.Fatalf("turn 1 text = %q", got)
	}
	usage := h.rec.ofKind(agentsession.KindUsage)
	if len(usage) != 1 || usage[0].Usage.InputTokens != 21885 || usage[0].Usage.OutputTokens != 5 {
		t.Fatalf("turn 1 usage = %+v", usage)
	}
	if u := usage[0].Usage; u.ContextTokens != 21885 || u.ContextWindow != 258400 {
		t.Fatalf("turn 1 context usage = %+v", u)
	}
	assertLastIsTurnEnd(t, h.rec.all(), agentsession.StopEndTurn)

	atts := []agentsession.Attachment{
		{Path: filepath.Join(h.cwd, "a.txt"), Text: "hello", StartLine: 1, EndLine: 1},
		{Path: filepath.Join(h.cwd, "b.go")},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop, err = h.d.Prompt(ctx, info.ID, "edit and run", atts)
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("turn 2: stop=%q err=%v", stop, err)
	}
	calls := h.rec.toolCalls()
	edit := calls["exec-d384c8fe-39b6-4e9f-ab8b-2f5afe2e83f0"]
	wantPath := filepath.Join(h.cwd, "a.txt")
	if edit.Kind != agentsession.ToolEdit || edit.Status != agentsession.StatusCompleted || edit.Title != "Edit a.txt" {
		t.Fatalf("edit call = %+v", edit)
	}
	if len(edit.Diffs) != 1 || edit.Diffs[0].Path != wantPath || edit.Diffs[0].OldText != "hello\n" || edit.Diffs[0].NewText != "hello\nworld\n" {
		t.Fatalf("edit diffs = %+v", edit.Diffs)
	}
	run := calls["exec-443d4f00-a017-4d79-81b9-d72eeb1f1cdc"]
	if run.Kind != agentsession.ToolExecute || run.Title != "Run: cat a.txt" || run.Input != "cat a.txt" ||
		run.Status != agentsession.StatusCompleted || run.Output != "hello\nworld\n" {
		t.Fatalf("command call = %+v", run)
	}
	usage = h.rec.ofKind(agentsession.KindUsage)
	if len(usage) != 2 || usage[1].Usage.InputTokens != 91077-21885 || usage[1].Usage.OutputTokens != 258-5 {
		t.Fatalf("turn 2 usage = %+v", usage[len(usage)-1].Usage)
	}
	if u := usage[1].Usage; u.ContextTokens != 23205 || u.ContextWindow != 258400 {
		t.Fatalf("turn 2 context usage = %+v", u)
	}
	if got := h.rec.text(agentsession.KindMessageChunk); got != "pongdone" {
		t.Fatalf("message text = %q", got)
	}
	if ends := h.rec.ofKind(agentsession.KindTurnEnd); len(ends) != 2 {
		t.Fatalf("turn ends = %d", len(ends))
	}
	for _, ev := range h.rec.all() {
		if ev.SessionID != info.ID {
			t.Fatalf("event %s has session %q", ev.Kind, ev.SessionID)
		}
	}
	log := h.clientLog(t)
	for _, want := range []string{`"approvalPolicy":"on-request"`, `"sandbox":"workspace-write"`, `"optOutNotificationMethods"`, `"text":"edit and run"`, `a.txt:1-1\n` + "```" + `\nhello\n` + "```", `"text":"Relevant file: `} {
		if !strings.Contains(log, want) {
			t.Errorf("client log lacks %s", want)
		}
	}
}

func TestApprovalAllow(t *testing.T) {
	h := newHarness(t, "approval_allow.jsonl", nil)
	info := h.start(t)
	var perms []agentsession.PermissionRequest
	h.rec.setReact(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			perms = append(perms, *ev.Permission)
			if err := h.d.Respond(ev.SessionID, ev.Permission.ID, decisionAccept); err != nil {
				t.Errorf("Respond: %v", err)
			}
		}
	})
	stop, err := h.prompt(t, info.ID, "plan, create, touch")
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	if len(perms) != 2 {
		t.Fatalf("permissions = %d", len(perms))
	}
	file, cmd := perms[0], perms[1]
	if file.ToolCall.Kind != agentsession.ToolEdit || len(file.ToolCall.Diffs) != 1 ||
		file.ToolCall.Diffs[0].NewText != "x\n" || file.ToolCall.Diffs[0].OldText != "" || file.ToolCall.Title != "Create new.txt" {
		t.Fatalf("file permission = %+v", file)
	}
	if cmd.ToolCall.Title != "Run: touch b.txt" || cmd.ID != "1" {
		t.Fatalf("command permission = %+v", cmd)
	}
	wantKinds := []string{agentsession.AllowOnce, agentsession.AllowAlways, agentsession.RejectOnce}
	for i, o := range cmd.Options {
		if o.Kind != wantKinds[i] {
			t.Fatalf("options = %+v", cmd.Options)
		}
	}
	if n := len(h.rec.ofKind(agentsession.KindPermissionSettled)); n != 2 {
		t.Fatalf("settled = %d", n)
	}
	if calls := h.rec.toolCalls(); calls["exec-3230fb1f-bf3c-41bd-bef2-aced7a07c49d"].Status != agentsession.StatusCompleted {
		t.Fatalf("command not completed: %+v", calls)
	}
	if got := h.rec.text(agentsession.KindMessageChunk); !strings.HasSuffix(got, "\n\nok") {
		t.Fatalf("message text = %q", got)
	}
}

func TestApprovalDeny(t *testing.T) {
	h := newHarness(t, "approval_deny.jsonl", nil)
	info := h.start(t)
	h.rec.setReact(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			if err := h.d.Respond(ev.SessionID, ev.Permission.ID, "bogus"); err == nil {
				t.Error("Respond accepted an unknown option")
			}
			if err := h.d.Respond(ev.SessionID, ev.Permission.ID, decisionDecline); err != nil {
				t.Errorf("Respond: %v", err)
			}
			if err := h.d.Respond(ev.SessionID, ev.Permission.ID, decisionDecline); err == nil {
				t.Error("second Respond succeeded")
			}
		}
	})
	stop, err := h.prompt(t, info.ID, "touch c.txt")
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	call := h.rec.toolCalls()["exec-aee1b288-ff4f-46a8-87eb-bb5bd3f4ddb4"]
	if call.Status != agentsession.StatusFailed {
		t.Fatalf("declined command = %+v", call)
	}
	if !strings.Contains(h.clientLog(t), `"decision":"decline"`) {
		t.Fatal("decline not sent")
	}
}

func TestCancelDuringApproval(t *testing.T) {
	h := newHarness(t, "interrupt.jsonl", nil)
	info := h.start(t)
	h.rec.setReact(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			if err := h.d.Cancel(ev.SessionID); err != nil {
				t.Errorf("Cancel: %v", err)
			}
		}
	})
	stop, err := h.prompt(t, info.ID, "sleep 5")
	if err != nil || stop != agentsession.StopCancelled {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	evs := h.rec.all()
	assertLastIsTurnEnd(t, evs, agentsession.StopCancelled)
	if n := len(h.rec.ofKind(agentsession.KindPermissionSettled)); n != 1 {
		t.Fatalf("settled = %d", n)
	}
	call := h.rec.toolCalls()["exec-5bb80264-00a7-46c8-853d-156014fcda6c"]
	if call.Status != agentsession.StatusFailed {
		t.Fatalf("interrupted command = %+v", call)
	}
	log := h.clientLog(t)
	if !strings.Contains(log, `"method":"turn/interrupt"`) || !strings.Contains(log, `"decision":"cancel"`) {
		t.Fatalf("client log lacks interrupt/cancel:\n%s", log)
	}
	if err := h.d.Cancel(info.ID); err != nil {
		t.Fatalf("idle Cancel: %v", err)
	}
}

func TestPromptContextCancel(t *testing.T) {
	h := newHarness(t, "interrupt.jsonl", nil)
	info := h.start(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.rec.setReact(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			cancel()
		}
	})
	stop, err := h.d.Prompt(ctx, info.ID, "sleep 5", nil)
	if err != nil || stop != agentsession.StopCancelled {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
}

func TestPlanReasoningAndTools(t *testing.T) {
	h := newHarness(t, "plan_reasoning.jsonl", nil)
	info := h.start(t)
	stop, err := h.prompt(t, info.ID, "plan")
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	if got := h.rec.text(agentsession.KindThoughtChunk); got != "Planning\n\nthe steps" {
		t.Fatalf("thoughts = %q", got)
	}
	plans := h.rec.ofKind(agentsession.KindPlan)
	if len(plans) != 1 || len(plans[0].Plan) != 3 ||
		plans[0].Plan[0].Status != agentsession.StatusCompleted ||
		plans[0].Plan[1].Status != agentsession.StatusInProgress ||
		plans[0].Plan[2].Status != agentsession.StatusPending || plans[0].Plan[1].Content != "Write fix" {
		t.Fatalf("plan = %+v", plans)
	}
	if got := h.rec.text(agentsession.KindMessageChunk); got != "Plan ready." {
		t.Fatalf("message = %q (sub-agent text must be ignored)", got)
	}
	calls := h.rec.toolCalls()
	if mcp := calls["mcp-1"]; mcp.Title != "docs.search" || mcp.Output != "found 3" || mcp.Kind != agentsession.ToolOther ||
		mcp.Status != agentsession.StatusCompleted || !strings.Contains(mcp.Input, `"q": "go"`) {
		t.Fatalf("mcp call = %+v", mcp)
	}
	if ws := calls["ws-1"]; ws.Kind != agentsession.ToolFetch || ws.Status != agentsession.StatusCompleted || ws.Title != "Search: golang release" {
		t.Fatalf("web search = %+v", ws)
	}
	errs := h.rec.ofKind(agentsession.KindError)
	if len(errs) != 1 || !strings.Contains(errs[0].Text, "retrying") {
		t.Fatalf("errors = %+v", errs)
	}
	if u := h.rec.ofKind(agentsession.KindUsage); len(u) != 1 || u[0].Usage.InputTokens != 250 || u[0].Usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestFailedTurn(t *testing.T) {
	h := newHarness(t, "failed_turn.jsonl", nil)
	info := h.start(t)
	stop, err := h.prompt(t, info.ID, "hi")
	var te *TurnError
	if stop != agentsession.StopError || !errors.As(err, &te) || te.Message != "Quota exceeded" {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	if errs := h.rec.ofKind(agentsession.KindError); len(errs) != 1 || errs[0].Text != "Quota exceeded" {
		t.Fatalf("errors = %+v", errs)
	}
	assertLastIsTurnEnd(t, h.rec.all(), agentsession.StopError)
}

func TestMiscRequests(t *testing.T) {
	h := newHarness(t, "misc_requests.jsonl", map[string]string{"a.txt": "hello\n"})
	info := h.start(t)
	h.rec.setReact(func(ev agentsession.Event) {
		if ev.Kind != agentsession.KindPermission {
			return
		}
		option := decisionAcceptForSession
		if ev.Permission.ToolCall.ID == "perm-1" {
			option = "session"
		}
		if err := h.d.Respond(ev.SessionID, ev.Permission.ID, option); err != nil {
			t.Errorf("Respond: %v", err)
		}
	})
	stop, err := h.prompt(t, info.ID, "misc")
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	perms := h.rec.ofKind(agentsession.KindPermission)
	if len(perms) != 2 {
		t.Fatalf("permissions = %d", len(perms))
	}
	file := perms[0].Permission.ToolCall
	if len(file.Diffs) != 1 || file.Diffs[0].NewText != "howdy\n" || !strings.Contains(file.Input, "write outside workspace") {
		t.Fatalf("file permission = %+v", file)
	}
	if p := perms[1].Permission; p.ToolCall.Title != "Need network" || !strings.Contains(p.ToolCall.Input, "network") {
		t.Fatalf("permissions request = %+v", p)
	}
	calls := h.rec.toolCalls()
	if run := calls["exec-9"]; run.Status != agentsession.StatusFailed || run.Title != "Run: go test ./... 2>&1 | tail -n 5" || run.Output != "FAIL\tflint/x\n" {
		t.Fatalf("failing command = %+v", run)
	}
	if edit := calls["patch-1"]; edit.Status != agentsession.StatusCompleted || len(edit.Diffs) != 1 || edit.Diffs[0].OldText != "hello\n" {
		t.Fatalf("edit = %+v", edit)
	}
}

func TestCrashReportsStderr(t *testing.T) {
	h := newHarness(t, "crash.jsonl", nil)
	info := h.start(t)
	stop, err := h.prompt(t, info.ID, "go")
	var exitErr *ExitError
	if stop != agentsession.StopError || !errors.As(err, &exitErr) || !strings.Contains(exitErr.Stderr, "panicked at app-server: boom") {
		t.Fatalf("stop=%q err=%v", stop, err)
	}
	errs := h.rec.ofKind(agentsession.KindError)
	if len(errs) != 1 || !strings.Contains(errs[0].Text, "boom") {
		t.Fatalf("errors = %+v", errs)
	}
	assertLastIsTurnEnd(t, h.rec.all(), agentsession.StopError)
	if got := h.rec.text(agentsession.KindMessageChunk); got != "partial" {
		t.Fatalf("text before crash = %q", got)
	}
	if _, err := h.prompt(t, info.ID, "again"); !errors.As(err, &exitErr) {
		t.Fatalf("prompt after crash: %v", err)
	}
}

func TestStopDuringTurn(t *testing.T) {
	h := newHarness(t, "hang.jsonl", nil)
	info := h.start(t)
	type result struct {
		stop string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		stop, err := h.d.Prompt(context.Background(), info.ID, "hang", nil)
		done <- result{stop, err}
	}()
	h.rec.waitFor(t, agentsession.KindToolCall)
	if _, err := h.d.Prompt(context.Background(), info.ID, "second", nil); !errors.Is(err, errTurnActive) {
		t.Fatalf("concurrent prompt: %v", err)
	}
	if err := h.d.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.stop != agentsession.StopCancelled {
		t.Fatalf("stop=%q err=%v", r.stop, r.err)
	}
	if call := h.rec.toolCalls()["exec-1"]; call.Status != agentsession.StatusFailed || call.Title != "Run: sleep 100" {
		t.Fatalf("open call = %+v", call)
	}
	assertLastIsTurnEnd(t, h.rec.all(), agentsession.StopCancelled)
	if err := h.d.Stop(info.ID); err == nil {
		t.Fatal("second Stop succeeded")
	}
	if _, err := h.d.Prompt(context.Background(), info.ID, "x", nil); err == nil {
		t.Fatal("Prompt on stopped session succeeded")
	}
}

func TestCloseWithoutLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	h := newHarness(t, "hang.jsonl", nil)
	info := h.start(t)
	done := make(chan string, 1)
	go func() {
		stop, _ := h.d.Prompt(context.Background(), info.ID, "hang", nil)
		done <- stop
	}()
	h.rec.waitFor(t, agentsession.KindToolCall)
	if err := h.d.Close(); err != nil {
		t.Fatal(err)
	}
	if stop := <-done; stop != agentsession.StopCancelled {
		t.Fatalf("stop = %q", stop)
	}
	if _, err := h.d.Start(context.Background(), h.cwd, h.rec.sink); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("goroutines: before=%d after=%d\n%s", before, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestResumeOpensStoredThread(t *testing.T) {
	h := newHarness(t, "resume.jsonl", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := h.d.Resume(ctx, h.cwd, "thr-old", h.rec.sink)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if info.ID != "thr-old" || info.Model != "gpt-5.6-luna" {
		t.Fatalf("info = %+v", info)
	}
	log := h.clientLog(t)
	if !strings.Contains(log, `"method":"thread/resume"`) || !strings.Contains(log, `"threadId":"thr-old"`) ||
		!strings.Contains(log, `"approvalPolicy":"on-request"`) || !strings.Contains(log, `"excludeTurns":true`) || strings.Contains(log, "thread/start") {
		t.Fatalf("client log:\n%s", log)
	}
	if _, err := h.d.Resume(ctx, h.cwd, "thr-old", h.rec.sink); err == nil {
		t.Fatal("resuming an open thread should fail")
	}
	if _, err := h.d.Resume(ctx, h.cwd, "", h.rec.sink); err == nil {
		t.Fatal("empty thread id should fail")
	}
}
