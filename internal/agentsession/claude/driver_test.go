package claude

import (
	"bufio"
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

	"flint/internal/agentsession"
)

// recorder collects events; hook (optional) runs inside the sink, so it may
// call Respond/Cancel synchronously.
type recorder struct {
	mu     sync.Mutex
	events []agentsession.Event
	hook   func(agentsession.Event)
}

func (r *recorder) sink(ev agentsession.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	hook := r.hook
	r.mu.Unlock()
	if hook != nil {
		hook(ev)
	}
}

func (r *recorder) setHook(h func(agentsession.Event)) {
	r.mu.Lock()
	r.hook = h
	r.mu.Unlock()
}

func (r *recorder) all() []agentsession.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agentsession.Event(nil), r.events...)
}

// reset returns and clears the collected events.
func (r *recorder) reset() []agentsession.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func ofKind(evs []agentsession.Event, kind string) []agentsession.Event {
	var out []agentsession.Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func text(evs []agentsession.Event, kind string) string {
	var b strings.Builder
	for _, e := range ofKind(evs, kind) {
		b.WriteString(e.Text)
	}
	return b.String()
}

func kinds(evs []agentsession.Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

type harness struct {
	d    *Driver
	info agentsession.SessionInfo
	rec  *recorder
	cwd  string
	log  string
}

func fakeOptions(t *testing.T, scenario string) (Options, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "fake.log")
	return Options{
		Binary: exe,
		Model:  "haiku",
		Env: []string{
			fakeEnv + "=1",
			fakeScenarioEnv + "=" + scenario,
			fakeDirEnv + "=" + testdata,
			fakeLogEnv + "=" + logPath,
		},
	}, logPath
}

// newWorkspace creates the files the captured transcripts operated on.
func newWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc greet() string {\n\treturn \"hello\"\n}\n")
	write("notes.txt", "line one\nline two\n")
	return dir
}

func startFake(t *testing.T, scenario string) *harness {
	t.Helper()
	opts, logPath := fakeOptions(t, scenario)
	d := New(opts)
	t.Cleanup(func() { _ = d.Close() })
	rec := &recorder{}
	cwd := newWorkspace(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := d.Start(ctx, cwd, rec.sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return &harness{d: d, info: info, rec: rec, cwd: cwd, log: logPath}
}

func (h *harness) prompt(t *testing.T, text string) (string, []agentsession.Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop, err := h.d.Prompt(ctx, h.info.ID, text, nil)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	evs := h.rec.reset()
	if len(evs) == 0 || evs[len(evs)-1].Kind != agentsession.KindTurnEnd || evs[len(evs)-1].StopReason != stop {
		t.Fatalf("turn did not end with turn_end(%s): %v", stop, kinds(evs))
	}
	for _, e := range evs {
		if e.SessionID != h.info.ID {
			t.Fatalf("event %s has session %q", e.Kind, e.SessionID)
		}
	}
	return stop, evs
}

// fakeLog returns what the fake logged (args line and stdin lines).
func (h *harness) fakeLog(t *testing.T) []map[string]json.RawMessage {
	t.Helper()
	f, err := os.Open(h.log)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad log line %s: %v", sc.Bytes(), err)
		}
		out = append(out, m)
	}
	return out
}

func TestTextTurn(t *testing.T) {
	h := startFake(t, "text")
	if h.info.Agent != "claude" || h.info.Model != "claude-haiku-5-5" || h.info.Cwd != h.cwd || len(h.info.ID) != 36 {
		t.Fatalf("SessionInfo = %+v", h.info)
	}
	stop, evs := h.prompt(t, "Reply with exactly: Hi there. Do not use tools.")
	if stop != agentsession.StopEndTurn {
		t.Fatalf("stop = %q", stop)
	}
	if got := text(evs, agentsession.KindMessageChunk); got != "Hi there." {
		t.Fatalf("message = %q (must not be doubled by the final assistant message)", got)
	}
	usage := ofKind(evs, agentsession.KindUsage)
	if len(usage) != 1 || usage[0].Usage.OutputTokens != 7 || usage[0].Usage.InputTokens != 2+32922 || usage[0].Usage.CostUSD <= 0 {
		t.Fatalf("usage = %+v", usage)
	}
	if u := usage[0].Usage; u.ContextTokens != 2+32922 || u.ContextWindow != 1000000 {
		t.Fatalf("context usage = %+v", u)
	}
	if model, _ := h.d.Model(h.info.ID); model != "claude-haiku-5-5" {
		t.Fatalf("Model = %q", model)
	}

	if err := h.d.Stop(h.info.ID); err != nil {
		t.Fatal(err)
	}
	log := h.fakeLog(t)
	var args []string
	_ = json.Unmarshal(log[0]["args"], &args)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--model haiku", "--permission-mode default", "--session-id " + h.info.ID} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q lack %q", joined, want)
		}
	}
	if string(log[0]["todoEnv"]) != `"1"` || string(log[0]["pwd"]) != `"`+h.cwd+`"` || string(log[0]["claudecode"]) != `""` {
		t.Errorf("env = %s", log[0])
	}
	var user struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct{ Type, Text string }
		} `json:"message"`
	}
	for _, l := range log[1:] {
		if raw, ok := l["stdin"]; ok && strings.Contains(string(raw), `"type":"user"`) {
			_ = json.Unmarshal(raw, &user)
		}
	}
	if user.Message.Role != "user" || len(user.Message.Content) != 1 || user.Message.Content[0].Text != "Reply with exactly: Hi there. Do not use tools." {
		t.Errorf("user message = %+v", user)
	}
	if _, err := h.d.Prompt(context.Background(), h.info.ID, "again", nil); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("Prompt after Stop: %v", err)
	}
}

func TestReadThenEditTwoTurns(t *testing.T) {
	h := startFake(t, "readedit")
	var perms []agentsession.PermissionRequest
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			perms = append(perms, *ev.Permission)
			if err := h.d.Respond(h.info.ID, ev.Permission.ID, agentsession.AllowOnce); err != nil {
				t.Errorf("Respond: %v", err)
			}
		}
	})

	// Turn 1: Read.
	stop, evs := h.prompt(t, "Use the Read tool on notes.txt and reply with only its first line.")
	if stop != agentsession.StopEndTurn || text(evs, agentsession.KindMessageChunk) != "line one" {
		t.Fatalf("turn 1: stop %q, text %q", stop, text(evs, agentsession.KindMessageChunk))
	}
	calls := ofKind(evs, agentsession.KindToolCall)
	if len(calls) != 1 {
		t.Fatalf("turn 1 tool calls: %v", kinds(evs))
	}
	read := calls[0].ToolCall
	if read.Kind != agentsession.ToolRead || read.Title != "Read notes.txt" || read.Status != agentsession.StatusPending ||
		len(read.Paths) != 1 || read.Paths[0] != filepath.Join(h.cwd, "notes.txt") {
		t.Fatalf("read call = %+v", read)
	}
	upd := ofKind(evs, agentsession.KindToolCallUpdate)
	if len(upd) != 1 || upd[0].ToolCall.ID != read.ID || upd[0].ToolCall.Status != agentsession.StatusCompleted ||
		!strings.Contains(upd[0].ToolCall.Output, "line one") {
		t.Fatalf("read update = %+v", upd)
	}

	// Turn 2: Read, Edit (permission) and Write (permission) in one session.
	stop, evs = h.prompt(t, "Use the Edit tool to change \"hello\" to \"hi\" in main.go, then use the Write tool to create new.txt containing exactly: abc. Be brief.")
	if stop != agentsession.StopEndTurn {
		t.Fatalf("turn 2 stop %q", stop)
	}
	byTitle := map[string]*agentsession.ToolCall{}
	for _, e := range ofKind(evs, agentsession.KindToolCall) {
		byTitle[e.ToolCall.Title] = e.ToolCall
	}
	edit, write := byTitle["Edit main.go"], byTitle["Write new.txt"]
	if edit == nil || write == nil || byTitle["Read main.go"] == nil {
		t.Fatalf("turn 2 calls: %v", byTitle)
	}
	mainGo := filepath.Join(h.cwd, "main.go")
	if edit.Kind != agentsession.ToolEdit || len(edit.Diffs) != 1 ||
		edit.Diffs[0] != (agentsession.Diff{Path: mainGo, OldText: `"hello"`, NewText: `"hi"`}) {
		t.Fatalf("edit = %+v", edit)
	}
	newTxt := filepath.Join(h.cwd, "new.txt")
	if write.Kind != agentsession.ToolEdit || len(write.Diffs) != 1 ||
		write.Diffs[0] != (agentsession.Diff{Path: newTxt, OldText: "", NewText: "abc"}) {
		t.Fatalf("write = %+v", write)
	}
	if len(perms) != 2 || perms[0].ToolCall.ID != edit.ID || perms[1].ToolCall.ID != write.ID {
		t.Fatalf("permissions = %+v", perms)
	}
	if opts := perms[0].Options; len(opts) != 3 || opts[1].Kind != agentsession.AllowAlways {
		t.Fatalf("options = %+v", opts)
	}
	if perms[0].ToolCall.Diffs[0].NewText != `"hi"` {
		t.Fatalf("permission tool call lacks diff: %+v", perms[0].ToolCall)
	}
	settled := ofKind(evs, agentsession.KindPermissionSettled)
	if len(settled) != 2 || settled[0].RequestID != perms[0].ID || settled[1].RequestID != perms[1].ID {
		t.Fatalf("settled = %+v", settled)
	}
	// Each allowed call goes in_progress then completed.
	status := map[string][]string{}
	for _, e := range ofKind(evs, agentsession.KindToolCallUpdate) {
		status[e.ToolCall.ID] = append(status[e.ToolCall.ID], e.ToolCall.Status)
	}
	for _, id := range []string{edit.ID, write.ID} {
		if strings.Join(status[id], ",") != "in_progress,completed" {
			t.Fatalf("statuses of %s = %v", id, status[id])
		}
	}
	if !strings.Contains(text(evs, agentsession.KindMessageChunk), "new.txt") {
		t.Fatalf("final text = %q", text(evs, agentsession.KindMessageChunk))
	}
	if len(ofKind(evs, agentsession.KindUsage)) != 1 {
		t.Fatal("missing usage")
	}
}

func TestBashAllowAlways(t *testing.T) {
	h := startFake(t, "bash_allow")
	var perm *agentsession.PermissionRequest
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			perm = ev.Permission
			if err := h.d.Respond(h.info.ID, ev.Permission.ID, agentsession.AllowAlways); err != nil {
				t.Errorf("Respond: %v", err)
			}
		}
	})
	stop, evs := h.prompt(t, "Run this exact shell command with the Bash tool: touch allowed.txt && echo created. Then reply done.")
	if stop != agentsession.StopEndTurn || text(evs, agentsession.KindMessageChunk) != "done" {
		t.Fatalf("stop %q text %q", stop, text(evs, agentsession.KindMessageChunk))
	}
	call := ofKind(evs, agentsession.KindToolCall)[0].ToolCall
	if call.Kind != agentsession.ToolExecute || call.Title != "Run: touch allowed.txt && echo created" ||
		call.Input != "touch allowed.txt && echo created" {
		t.Fatalf("call = %+v", call)
	}
	if perm == nil || perm.ToolCall.Title != call.Title || len(perm.Options) != 3 {
		t.Fatalf("permission = %+v", perm)
	}
	last := ofKind(evs, agentsession.KindToolCallUpdate)
	if got := last[len(last)-1].ToolCall; got.Status != agentsession.StatusCompleted || got.Output != "created" {
		t.Fatalf("result = %+v", got)
	}
	// Repeated Respond fails: the request is gone.
	if err := h.d.Respond(h.info.ID, perm.ID, agentsession.AllowOnce); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("second Respond: %v", err)
	}
	_ = h.d.Stop(h.info.ID)

	// Only the rule suggestion is persisted, not setMode/addDirectories.
	var answer struct {
		Behavior           string `json:"behavior"`
		UpdatedPermissions []struct {
			Type  string `json:"type"`
			Rules []struct{ ToolName, RuleContent string }
		} `json:"updatedPermissions"`
		UpdatedInput map[string]any `json:"updatedInput"`
	}
	for _, l := range h.fakeLog(t) {
		if raw, ok := l["permissionAnswer"]; ok {
			_ = json.Unmarshal(raw, &answer)
		}
	}
	if answer.Behavior != "allow" || len(answer.UpdatedPermissions) != 1 || answer.UpdatedPermissions[0].Type != "addRules" ||
		answer.UpdatedPermissions[0].Rules[0].RuleContent != "touch allowed.txt && echo created" ||
		answer.UpdatedInput["command"] != "touch allowed.txt && echo created" {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestBashReject(t *testing.T) {
	h := startFake(t, "bash_deny")
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			if err := h.d.Respond(h.info.ID, ev.Permission.ID, agentsession.RejectOnce); err != nil {
				t.Errorf("Respond: %v", err)
			}
		}
	})
	stop, evs := h.prompt(t, "Run this exact shell command with the Bash tool: touch denied.txt. If refused, reply: refused.")
	if stop != agentsession.StopEndTurn || text(evs, agentsession.KindMessageChunk) != "refused." {
		t.Fatalf("stop %q text %q", stop, text(evs, agentsession.KindMessageChunk))
	}
	upd := ofKind(evs, agentsession.KindToolCallUpdate)
	if len(upd) != 1 || upd[0].ToolCall.Status != agentsession.StatusFailed || upd[0].ToolCall.Output != "User rejected this action" {
		t.Fatalf("updates = %+v", upd)
	}
	if len(ofKind(evs, agentsession.KindPermissionSettled)) != 1 {
		t.Fatalf("events = %v", kinds(evs))
	}
}

func TestTodoWritePlan(t *testing.T) {
	h := startFake(t, "todo")
	_, evs := h.prompt(t, "Use the TodoWrite tool to record a plan with exactly two todos: 'Inspect main.go' (in_progress) and 'Write tests' (pending). Do nothing else, then reply ok.")
	plans := ofKind(evs, agentsession.KindPlan)
	want := []agentsession.PlanEntry{{Content: "Inspect main.go", Status: "in_progress"}, {Content: "Write tests", Status: "pending"}}
	if len(plans) != 1 || len(plans[0].Plan) != 2 || plans[0].Plan[0] != want[0] || plans[0].Plan[1] != want[1] {
		t.Fatalf("plans = %+v", plans)
	}
	for _, e := range ofKind(evs, agentsession.KindToolCall) {
		if e.ToolCall.Title == "TodoWrite" {
			t.Fatal("TodoWrite must be shown as a plan, not a tool call")
		}
	}
	if calls := ofKind(evs, agentsession.KindToolCall); len(calls) != 1 || calls[0].ToolCall.Title != "Load tools: select:TodoWrite" {
		t.Fatalf("calls = %+v", calls)
	}
	// No orphan update for the hidden TodoWrite call.
	if upd := ofKind(evs, agentsession.KindToolCallUpdate); len(upd) != 1 || upd[0].ToolCall.Output != "Loaded tool TodoWrite" {
		t.Fatalf("updates = %+v", upd)
	}
}

func TestTaskToolsPlan(t *testing.T) {
	h := startFake(t, "tasks")
	_, evs := h.prompt(t, "Use the TodoWrite tool (or, if absent, TaskCreate) to record a plan with exactly two items: 'Inspect main.go' (in_progress) and 'Write tests' (pending). Do nothing else, then reply ok.")
	plans := ofKind(evs, agentsession.KindPlan)
	if len(plans) != 3 {
		t.Fatalf("plans = %+v", plans)
	}
	final := plans[2].Plan
	if len(final) != 2 || final[0] != (agentsession.PlanEntry{Content: "Inspect main.go", Status: "in_progress"}) ||
		final[1] != (agentsession.PlanEntry{Content: "Write tests", Status: "pending"}) {
		t.Fatalf("final plan = %+v", final)
	}
	if len(plans[0].Plan) != 1 || plans[0].Plan[0].Status != "pending" {
		t.Fatalf("first plan = %+v", plans[0].Plan)
	}
}

func TestCancelWhileStreaming(t *testing.T) {
	h := startFake(t, "interrupt")
	var once sync.Once
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindMessageChunk {
			once.Do(func() {
				if err := h.d.Cancel(h.info.ID); err != nil {
					t.Errorf("Cancel: %v", err)
				}
			})
		}
	})
	stop, evs := h.prompt(t, "Write a 400-word story about a lighthouse keeper. No tools.")
	if stop != agentsession.StopCancelled {
		t.Fatalf("stop = %q", stop)
	}
	if len(ofKind(evs, agentsession.KindError)) != 0 {
		t.Fatalf("a cancelled turn is not an error: %v", ofKind(evs, agentsession.KindError))
	}
	if got := text(evs, agentsession.KindMessageChunk); !strings.HasPrefix(got, "**The Keeper") {
		t.Fatalf("partial text = %q", got)
	}
	// Cancel with no turn running is a no-op.
	h.rec.setHook(nil)
	if err := h.d.Cancel(h.info.ID); err != nil {
		t.Fatal(err)
	}
	// The session survives the interrupt.
	stop, evs = h.prompt(t, "Reply with exactly: still here")
	if stop != agentsession.StopEndTurn || text(evs, agentsession.KindMessageChunk) != "still here" {
		t.Fatalf("follow-up: %q %q", stop, text(evs, agentsession.KindMessageChunk))
	}
}

func TestCancelPendingPermissionViaContext(t *testing.T) {
	h := startFake(t, "interrupt_perm")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var permID string
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			permID = ev.Permission.ID
			cancel()
		}
	})
	stop, err := h.d.Prompt(ctx, h.info.ID, "Run this exact shell command with the Bash tool: touch never.txt", nil)
	if err != nil || stop != agentsession.StopCancelled {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	evs := h.rec.reset()
	settled := ofKind(evs, agentsession.KindPermissionSettled)
	if permID == "" || len(settled) != 1 || settled[0].RequestID != permID {
		t.Fatalf("settled = %+v (perm %q), events %v", settled, permID, kinds(evs))
	}
	if evs[len(evs)-1].Kind != agentsession.KindTurnEnd || evs[len(evs)-1].StopReason != agentsession.StopCancelled {
		t.Fatalf("events = %v", kinds(evs))
	}
	if err := h.d.Respond(h.info.ID, permID, agentsession.AllowOnce); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("Respond after cancel: %v", err)
	}
}

func TestCrashMidTurn(t *testing.T) {
	h := startFake(t, "crash")
	stop, err := h.d.Prompt(context.Background(), h.info.ID, "hi", nil)
	if stop != agentsession.StopError || err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Err == nil {
		t.Fatalf("err = %#v", err)
	}
	evs := h.rec.reset()
	n := len(evs)
	if n < 3 || evs[n-2].Kind != agentsession.KindError || !strings.Contains(evs[n-2].Text, "boom") ||
		evs[n-1].Kind != agentsession.KindTurnEnd || evs[n-1].StopReason != agentsession.StopError {
		t.Fatalf("events = %+v", evs)
	}
	if text(evs, agentsession.KindMessageChunk) != "partial" {
		t.Fatalf("partial text lost: %v", kinds(evs))
	}
	if _, err := h.d.Prompt(context.Background(), h.info.ID, "again", nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Prompt on dead session: %v", err)
	}
	if err := h.d.Stop(h.info.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStartFailureReportsStderr(t *testing.T) {
	opts, _ := fakeOptions(t, "badinit")
	d := New(opts)
	defer d.Close()
	_, err := d.Start(context.Background(), newWorkspace(t), (&recorder{}).sink)
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("Start = %v", err)
	}
}

func TestStartValidation(t *testing.T) {
	d := New(Options{Binary: "/nonexistent/claude"})
	defer d.Close()
	ctx := context.Background()
	if _, err := d.Start(ctx, t.TempDir(), nil); err == nil {
		t.Error("nil sink accepted")
	}
	if _, err := d.Start(ctx, "relative", (&recorder{}).sink); err == nil {
		t.Error("relative cwd accepted")
	}
	if _, err := d.Start(ctx, filepath.Join(t.TempDir(), "missing"), (&recorder{}).sink); err == nil {
		t.Error("missing cwd accepted")
	}
	if _, err := d.Start(ctx, t.TempDir(), (&recorder{}).sink); err == nil || !strings.Contains(err.Error(), "not an executable") {
		t.Errorf("bad binary: %v", err)
	}
	for _, call := range []func() error{
		func() error { _, err := d.Prompt(ctx, "nope", "x", nil); return err },
		func() error { return d.Cancel("nope") },
		func() error { return d.Respond("nope", "r", agentsession.AllowOnce) },
		func() error { return d.Stop("nope") },
		func() error { _, err := d.Model("nope"); return err },
	} {
		if err := call(); !errors.Is(err, ErrUnknownSession) {
			t.Errorf("unknown session: %v", err)
		}
	}
	_ = d.Close()
	if _, err := d.Start(ctx, t.TempDir(), (&recorder{}).sink); !errors.Is(err, ErrClosed) {
		t.Errorf("Start after Close: %v", err)
	}
	if err := d.Cancel("nope"); !errors.Is(err, ErrClosed) {
		t.Errorf("Cancel after Close: %v", err)
	}
}

func TestPromptValidationAndBusy(t *testing.T) {
	h := startFake(t, "hang")
	if _, err := h.d.Prompt(context.Background(), h.info.ID, "  ", nil); err == nil {
		t.Fatal("empty prompt accepted")
	}
	started := make(chan struct{})
	var once sync.Once
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindMessageChunk {
			once.Do(func() { close(started) })
		}
	})
	result := make(chan string, 1)
	go func() {
		stop, _ := h.d.Prompt(context.Background(), h.info.ID, "work", nil)
		result <- stop
	}()
	<-started
	if _, err := h.d.Prompt(context.Background(), h.info.ID, "more", nil); !errors.Is(err, ErrTurnInProgress) {
		t.Fatalf("concurrent Prompt: %v", err)
	}
	if err := h.d.Respond(h.info.ID, "nope", "bogus"); err == nil {
		t.Fatal("unknown option accepted")
	}
	// Stop ends the running turn as cancelled.
	if err := h.d.Stop(h.info.ID); err != nil {
		t.Fatal(err)
	}
	if stop := <-result; stop != agentsession.StopCancelled {
		t.Fatalf("stop = %q", stop)
	}
	evs := h.rec.all()
	if evs[len(evs)-1].Kind != agentsession.KindTurnEnd || len(ofKind(evs, agentsession.KindError)) != 0 {
		t.Fatalf("events = %v", kinds(evs))
	}
}

func TestCancelGraceStopsUnresponsiveCLI(t *testing.T) {
	old := cancelGrace
	cancelGrace = 200 * time.Millisecond
	defer func() { cancelGrace = old }()
	h := startFake(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindMessageChunk {
			cancel()
		}
	})
	start := time.Now()
	stop, err := h.d.Prompt(ctx, h.info.ID, "work", nil)
	if stop != agentsession.StopCancelled || err != nil {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if _, err := h.d.Prompt(context.Background(), h.info.ID, "again", nil); !errors.Is(err, ErrSessionStopped) {
		t.Fatalf("Prompt after forced stop: %v", err)
	}
}

func TestCloseStopsEverythingWithoutLeaks(t *testing.T) {
	waitGoroutines := func(limit int) int {
		deadline := time.Now().Add(5 * time.Second)
		for {
			n := runtime.NumGoroutine()
			if n <= limit || time.Now().After(deadline) {
				return n
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	baseline := runtime.NumGoroutine()

	opts, _ := fakeOptions(t, "text")
	d := New(opts)
	rec := &recorder{}
	var ids []string
	for range 3 {
		info, err := d.Start(context.Background(), newWorkspace(t), rec.sink)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.ID)
	}
	if _, err := d.Prompt(context.Background(), ids[0], "Reply with exactly: Hi there. Do not use tools.", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil { // idempotent
		t.Fatal(err)
	}
	if n := waitGoroutines(baseline); n > baseline {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines: %d > baseline %d\n%s", n, baseline, buf[:runtime.Stack(buf, true)])
	}
	if len(ofKind(rec.all(), agentsession.KindError)) != 0 {
		t.Fatal("an intended stop must not report an error")
	}
}
