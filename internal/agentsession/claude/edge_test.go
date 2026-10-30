package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/agentsession"
)

// syntheticTranscript exercises shapes the captured transcripts do not:
// stray output, an unsupported control request, subagent lines, unstreamed
// assistant blocks, a withdrawn permission, assorted tools and failed
// results. Three turns.
var syntheticTranscript = []string{
	// Turn 1.
	`{"type":"system","subtype":"init","model":"claude-sonnet-5-5","session_id":"s"}`,
	`this is not json`,
	`{"type":"control_request","request_id":"hk1","request":{"subtype":"hook_callback","callback_id":"x"}}`,
	`{"type":"assistant","parent_tool_use_id":"toolu_sub","message":{"id":"m0","content":[{"type":"text","text":"SUBAGENT"}]}}`,
	`{"type":"stream_event","parent_tool_use_id":"toolu_sub","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"SUBAGENT"}}}`,
	`{"type":"user","parent_tool_use_id":"toolu_sub","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tsub","content":"x"}]}}`,
	`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"m1","content":[{"type":"thinking","thinking":"pondering"},{"type":"text","text":"Hello "}]}}`,
	`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"message_start","message":{"id":"m2"}}}`,
	`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"deep"}}}`,
	`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"world"}}}`,
	`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"m2","content":[{"type":"thinking","thinking":"deep"}]}}`,
	`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"m2","content":[{"type":"text","text":"world"}]}}`,
	`{"type":"assistant","parent_tool_use_id":null,"message":{"id":"m3","content":[` +
		`{"type":"tool_use","id":"tu1","name":"Grep","input":{"pattern":"TODO","path":"src"}},` +
		`{"type":"tool_use","id":"tu2","name":"mcp__github__create_issue","input":{"title":"x"}},` +
		`{"type":"tool_use","id":"tu3","name":"Task","input":{"description":"Explore code","subagent_type":"Explore"}},` +
		`{"type":"tool_use","id":"tu4","name":"WebFetch","input":{"url":"https://example.com"}},` +
		`{"type":"tool_use","id":"tu5","name":"Write","input":{"file_path":"/FIXTURE_CWD/existing.txt","content":"new"}},` +
		`{"type":"tool_use","id":"tu1","name":"Grep","input":{"pattern":"dupe"}}]}}`,
	`{"type":"control_request","request_id":"p1","request":{"subtype":"can_use_tool","tool_name":"WebFetch","input":{"url":"https://example.com"},"tool_use_id":"tu4"}}`,
	`{"type":"control_cancel_request","request_id":"p1"}`,
	`{"type":"control_cancel_request","request_id":"unknown"}`,
	`{"type":"user","parent_tool_use_id":null,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":"src/a.go:1"},{"type":"image","source":{}}]},{"type":"tool_result","tool_use_id":"tu2","is_error":true,"content":"boom"}]}}`,
	`{"type":"user","parent_tool_use_id":null,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu5","content":"ok"}]},"tool_use_result":{"type":"update","originalFile":"old\n"}}`,
	`{"type":"user","parent_tool_use_id":null,"message":{"role":"user","content":"[Request interrupted by user]"}}`,
	`{"type":"result","subtype":"success","is_error":true,"result":"API Error: 529 overloaded","stop_reason":null,"total_cost_usd":0.5,"usage":{"input_tokens":10,"output_tokens":3}}`,
	// Turn 2.
	`{"type":"system","subtype":"init","model":"claude-sonnet-5-5","session_id":"s"}`,
	`{"type":"result","subtype":"success","is_error":false,"stop_reason":"max_tokens","total_cost_usd":0.7}`,
	// Turn 3.
	`{"type":"system","subtype":"init","model":"claude-sonnet-5-5","session_id":"s"}`,
	`{"type":"result","subtype":"error_max_turns","is_error":true,"errors":["Reached maximum number of turns (3)"],"stop_reason":null,"total_cost_usd":0.7}`,
}

func TestSyntheticEdgeCases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(syntheticTranscript, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := startFake(t, "raw:"+path)
	if err := os.WriteFile(filepath.Join(h.cwd, "existing.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var perm *agentsession.PermissionRequest
	h.rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			perm = ev.Permission
		}
	})
	attachments := []agentsession.Attachment{
		{Path: "main.go", Text: "func greet() string {\n", StartLine: 3, EndLine: 3},
		{Path: "notes.txt"},
	}
	stop, err := h.d.Prompt(context.Background(), h.info.ID, "look", attachments)
	if err != nil || stop != agentsession.StopError {
		t.Fatalf("turn 1 = %q, %v", stop, err)
	}
	evs := h.rec.reset()
	if got := text(evs, agentsession.KindMessageChunk); got != "Hello world" {
		t.Fatalf("text = %q", got)
	}
	if got := text(evs, agentsession.KindThoughtChunk); got != "ponderingdeep" {
		t.Fatalf("thoughts = %q", got)
	}
	titles := map[string]agentsession.ToolCall{}
	for _, e := range ofKind(evs, agentsession.KindToolCall) {
		titles[e.ToolCall.ID] = *e.ToolCall
	}
	if len(titles) != 5 {
		t.Fatalf("tool calls = %+v", titles)
	}
	want := map[string][2]string{
		"tu1": {agentsession.ToolSearch, `Search "TODO" in src`},
		"tu2": {agentsession.ToolOther, "github: create_issue"},
		"tu3": {agentsession.ToolOther, "Agent: Explore code"},
		"tu4": {agentsession.ToolFetch, "Fetch https://example.com"},
		"tu5": {agentsession.ToolEdit, "Write existing.txt"},
	}
	for id, w := range want {
		if titles[id].Kind != w[0] || titles[id].Title != w[1] {
			t.Errorf("%s = %+v, want %v", id, titles[id], w)
		}
	}
	if d := titles["tu5"].Diffs; len(d) != 1 || d[0].OldText != "old\n" || d[0].NewText != "new" {
		t.Errorf("write diff (from disk) = %+v", d)
	}
	if perm == nil || len(perm.Options) != 2 || perm.Options[1].Kind != agentsession.RejectOnce || perm.ToolCall.Title != "Fetch https://example.com" {
		t.Fatalf("permission = %+v", perm)
	}
	settled := ofKind(evs, agentsession.KindPermissionSettled)
	if len(settled) != 1 || settled[0].RequestID != "p1" {
		t.Fatalf("settled = %+v", settled)
	}
	updates := map[string]agentsession.ToolCall{}
	for _, e := range ofKind(evs, agentsession.KindToolCallUpdate) {
		updates[e.ToolCall.ID] = *e.ToolCall
	}
	if u := updates["tu1"]; u.Status != agentsession.StatusCompleted || u.Output != "src/a.go:1\n[image]" {
		t.Errorf("tu1 update = %+v", u)
	}
	if u := updates["tu2"]; u.Status != agentsession.StatusFailed || u.Output != "boom" {
		t.Errorf("tu2 update = %+v", u)
	}
	if u := updates["tu5"]; len(u.Diffs) != 1 || u.Diffs[0].OldText != "old\n" {
		t.Errorf("tu5 update = %+v", u)
	}
	if _, ok := updates["tsub"]; ok {
		t.Error("subagent tool result leaked")
	}
	errs := ofKind(evs, agentsession.KindError)
	if len(errs) != 1 || errs[0].Text != "API Error: 529 overloaded" {
		t.Fatalf("errors = %+v", errs)
	}
	if u := ofKind(evs, agentsession.KindUsage); len(u) != 1 || u[0].Usage.CostUSD != 0.5 || u[0].Usage.InputTokens != 10 {
		t.Fatalf("usage = %+v", u)
	}
	if m, _ := h.d.Model(h.info.ID); m != "claude-sonnet-5-5" {
		t.Errorf("model from system/init = %q", m)
	}

	stop, _ = h.d.Prompt(context.Background(), h.info.ID, "more", nil)
	evs = h.rec.reset()
	if stop != agentsession.StopMaxTokens {
		t.Fatalf("turn 2 stop = %q", stop)
	}
	if u := ofKind(evs, agentsession.KindUsage); len(u) != 1 || u[0].Usage.CostUSD < 0.199 || u[0].Usage.CostUSD > 0.201 {
		t.Fatalf("turn 2 usage (cumulative cost delta) = %+v", u)
	}

	stop, _ = h.d.Prompt(context.Background(), h.info.ID, "again", nil)
	evs = h.rec.reset()
	if stop != agentsession.StopError || text(evs, agentsession.KindError) != "Reached maximum number of turns (3)" {
		t.Fatalf("turn 3 = %q, %v", stop, evs)
	}
	if len(ofKind(evs, agentsession.KindUsage)) != 0 {
		t.Fatal("zero-cost turn without usage must not report usage")
	}

	_ = h.d.Stop(h.info.ID)
	var sawHookError, sawAttachments bool
	for _, l := range h.fakeLog(t) {
		raw := string(l["stdin"])
		if strings.Contains(raw, `"request_id":"hk1"`) && strings.Contains(raw, `"subtype":"error"`) {
			sawHookError = true
		}
		if strings.Contains(raw, `"text":"main.go:3-3\n`+"```"+`\nfunc greet() string {\n`+"```"+`"`) &&
			strings.Contains(raw, `"text":"Relevant file: notes.txt"`) {
			sawAttachments = true
		}
	}
	if !sawHookError {
		t.Error("unsupported control request was not answered with an error")
	}
	if !sawAttachments {
		t.Error("attachments not rendered into the user message")
	}
}

func TestProcessDeathWhileIdleIsReported(t *testing.T) {
	h := startFake(t, "exitidle")
	deadline := time.Now().Add(5 * time.Second)
	for len(ofKind(h.rec.all(), agentsession.KindError)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no error event after the process died")
		}
		time.Sleep(10 * time.Millisecond)
	}
	errs := ofKind(h.rec.all(), agentsession.KindError)
	if !strings.Contains(errs[0].Text, "idle crash") || len(ofKind(h.rec.all(), agentsession.KindTurnEnd)) != 0 {
		t.Fatalf("events = %+v", h.rec.all())
	}
	if err := h.d.Cancel(h.info.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWriteFailureEndsTurnWithError(t *testing.T) {
	h := startFake(t, "closestdin")
	time.Sleep(50 * time.Millisecond) // let the fake close its stdin
	stop, err := h.d.Prompt(context.Background(), h.info.ID, "hello", nil)
	if stop != agentsession.StopError || err == nil {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	evs := h.rec.all()
	if evs[len(evs)-1].Kind != agentsession.KindTurnEnd || len(ofKind(evs, agentsession.KindError)) != 1 {
		t.Fatalf("events = %v", kinds(evs))
	}
}

func TestStartWithCancelledContext(t *testing.T) {
	opts, _ := fakeOptions(t, "hang")
	d := New(opts)
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Start(ctx, newWorkspace(t), (&recorder{}).sink); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Start = %v", err)
	}
}

func TestFixturesAreScrubbed(t *testing.T) {
	files, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"@", "/Users/", "/private/tmp/", "organization", "account"} {
			if strings.Contains(string(data), bad) && !(bad == "/Users/" && strings.Contains(string(data), "/Users/user")) {
				t.Errorf("%s contains %q", f, bad)
			}
		}
		if strings.HasSuffix(f, ".jsonl") {
			for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if !json.Valid([]byte(line)) {
					t.Errorf("%s:%d is not JSON", f, i+1)
				}
			}
		}
	}
}
