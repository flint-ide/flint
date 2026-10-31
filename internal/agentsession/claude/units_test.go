package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"flint/internal/agent"
	"flint/internal/agentsession"
)

func TestDescribeTool(t *testing.T) {
	cwd := t.TempDir()
	tests := []struct {
		name, input   string
		kind, title   string
		paths         []string
		input0, diff0 string // expected Input prefix / first diff NewText
	}{
		{"Read", `{"file_path":"` + cwd + `/a/b.go"}`, agentsession.ToolRead, "Read a/b.go", []string{cwd + "/a/b.go"}, "", ""},
		{"Read", `{"file_path":"/etc/hosts"}`, agentsession.ToolRead, "Read /etc/hosts", []string{"/etc/hosts"}, "", ""},
		{"Glob", `{"pattern":"**/*.go"}`, agentsession.ToolSearch, "Find **/*.go", nil, "", ""},
		{"Grep", `{"pattern":"x","path":"` + cwd + `"}`, agentsession.ToolSearch, `Search "x" in .`, []string{cwd}, "", ""},
		{"LS", `{"path":"sub"}`, agentsession.ToolSearch, "List sub", []string{cwd + "/sub"}, "", ""},
		{"MultiEdit", `{"file_path":"m.go","edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d"}]}`, agentsession.ToolEdit, "Edit m.go", []string{cwd + "/m.go"}, "", "b"},
		{"NotebookEdit", `{"notebook_path":"n.ipynb","new_source":"print(1)"}`, agentsession.ToolEdit, "Edit n.ipynb", []string{cwd + "/n.ipynb"}, "", "print(1)"},
		{"Bash", `{"command":"go test ./...\necho done","description":"test"}`, agentsession.ToolExecute, "Run: go test ./... …", nil, "go test ./...\necho done", ""},
		{"BashOutput", `{"bash_id":"1"}`, agentsession.ToolExecute, "BashOutput", nil, "{\n  \"bash_id\"", ""},
		{"WebSearch", `{"query":"go 1.26"}`, agentsession.ToolFetch, "Search the web: go 1.26", nil, "", ""},
		{"Agent", `{"subagent_type":"Explore"}`, agentsession.ToolOther, "Agent: Explore", nil, "", ""},
		{"ExitPlanMode", `{"plan":"1. do it"}`, agentsession.ToolThink, "Plan ready for review", nil, "1. do it", ""},
		{"Skill", `{"skill":"pdf"}`, agentsession.ToolOther, "Skill: pdf", nil, "", ""},
		{"mcp__bad", `{}`, agentsession.ToolOther, "mcp__bad", nil, "", ""},
		{"Mystery", `not json`, agentsession.ToolOther, "Mystery", nil, "not json", ""},
	}
	for _, tt := range tests {
		tc := describeTool("id1", tt.name, json.RawMessage(tt.input), cwd)
		if tc.ID != "id1" || tc.Kind != tt.kind || tc.Title != tt.title || tc.Status != agentsession.StatusPending {
			t.Errorf("%s %s: got kind %q title %q", tt.name, tt.input, tc.Kind, tc.Title)
		}
		if strings.Join(tc.Paths, ",") != strings.Join(tt.paths, ",") {
			t.Errorf("%s: paths %v, want %v", tt.name, tc.Paths, tt.paths)
		}
		if tt.input0 != "" && !strings.HasPrefix(tc.Input, tt.input0) {
			t.Errorf("%s: input %q", tt.name, tc.Input)
		}
		if tt.diff0 != "" && (len(tc.Diffs) == 0 || tc.Diffs[0].NewText != tt.diff0) {
			t.Errorf("%s: diffs %+v", tt.name, tc.Diffs)
		}
	}
	long := strings.Repeat("x", 300)
	if tc := describeTool("i", "Bash", json.RawMessage(`{"command":"`+long+`"}`), cwd); len([]rune(tc.Title)) != len("Run: ")+maxTitleRunes+1 {
		t.Errorf("long command title not clipped: %d", len(tc.Title))
	}
}

func TestWriteDiffReadsExistingFile(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "f.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := describeTool("w", "Write", json.RawMessage(`{"file_path":"f.txt","content":"after"}`), cwd)
	if tc.Diffs[0] != (agentsession.Diff{Path: filepath.Join(cwd, "f.txt"), OldText: "before", NewText: "after"}) {
		t.Fatalf("diff = %+v", tc.Diffs)
	}
	if got := readSmallFile(cwd); got != "" {
		t.Errorf("directory read as %q", got)
	}
	if got := readSmallFile(""); got != "" {
		t.Errorf("empty path read as %q", got)
	}
	big := filepath.Join(cwd, "big")
	if err := os.WriteFile(big, make([]byte, maxDiffFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readSmallFile(big); got != "" {
		t.Error("oversized file was read")
	}
}

func TestWriteOriginal(t *testing.T) {
	for raw, want := range map[string][2]any{
		`{"type":"update","originalFile":"x"}`:  {"x", true},
		`{"type":"create","originalFile":null}`: {"", true},
		`{"type":"create"}`:                     {"", false},
		`"Error: denied"`:                       {"", false},
		``:                                      {"", false},
	} {
		got, ok := writeOriginal(json.RawMessage(raw))
		if got != want[0] || ok != want[1] {
			t.Errorf("writeOriginal(%s) = %q, %v", raw, got, ok)
		}
	}
}

func TestTruncateAndClip(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Error("short string changed")
	}
	s := strings.Repeat("é", 10) // 2 bytes each
	got := truncate(s, 5)
	if !strings.HasPrefix(got, "éé\n") || !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("truncate = %q", got)
	}
	if clip("abc", 5) != "abc" || clip("abcdef", 3) != "abc…" {
		t.Error("clip")
	}
	if firstNonEmpty("", " ", "x") != "x" || firstNonEmpty() != "" {
		t.Error("firstNonEmpty")
	}
	if displayPath("", "/w") != "." || displayPath("../x", "/w/a") != "/w/x" {
		t.Errorf("displayPath: %q %q", displayPath("", "/w"), displayPath("../x", "/w/a"))
	}
}

func TestToolResultText(t *testing.T) {
	if toolResultText(nil) != "" || toolResultText(json.RawMessage(`{"weird":1}`)) != "" {
		t.Error("unexpected text for empty/odd content")
	}
	if got := toolResultText(json.RawMessage(`[{"type":"text","text":"a"},{"type":"other"},{"type":"text","text":"b"}]`)); got != "a\nb" {
		t.Errorf("got %q", got)
	}
}

func TestBuildUserMessage(t *testing.T) {
	if _, err := buildUserMessage(" \n", nil); err == nil {
		t.Error("empty prompt accepted")
	}
	if _, err := buildUserMessage("", []agentsession.Attachment{{}}); err == nil {
		t.Error("empty attachment counted as content")
	}
	msg, err := buildUserMessage("", []agentsession.Attachment{
		{Path: "a.md", Text: "has ``` fence\n"},
		{Path: "b.go", Text: "x", StartLine: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != "user" || msg.Message.Role != "user" || len(msg.Message.Content) != 2 {
		t.Fatalf("msg = %+v", msg)
	}
	if got := msg.Message.Content[0].Text; got != "a.md\n````\nhas ``` fence\n````" {
		t.Errorf("fenced = %q", got)
	}
	if got := msg.Message.Content[1].Text; got != "b.go:7-7\n```\nx\n```" {
		t.Errorf("selection = %q", got)
	}
	data, _ := json.Marshal(msg)
	if !strings.Contains(string(data), `"parent_tool_use_id":null`) || !strings.Contains(string(data), `"session_id":""`) {
		t.Errorf("wire shape = %s", data)
	}
}

func TestAlwaysAllowRules(t *testing.T) {
	rule := json.RawMessage(`{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"ls"}],"behavior":"allow","destination":"localSettings"}`)
	mode := json.RawMessage(`{"type":"setMode","mode":"acceptEdits","destination":"session"}`)
	dirs := json.RawMessage(`{"type":"addDirectories","directories":["/x"],"destination":"session"}`)
	if got := alwaysAllowRules([]json.RawMessage{rule, dirs, mode}); len(got) != 1 || string(got[0]) != string(rule) {
		t.Errorf("with rules: %s", got)
	}
	if got := alwaysAllowRules([]json.RawMessage{mode}); len(got) != 1 || string(got[0]) != string(mode) {
		t.Errorf("edit fallback: %s", got)
	}
	if got := permissionOptions(false); len(got) != 2 {
		t.Errorf("options without suggestions = %+v", got)
	}
}

func TestResolveModel(t *testing.T) {
	init := json.RawMessage(`{"models":[{"value":"default","resolvedModel":"claude-opus-5-5"},{"value":"haiku","resolvedModel":"claude-haiku-5-5"}]}`)
	for want, in := range map[string]string{"claude-opus-5-5": "", "claude-haiku-5-5": "haiku", "my-custom": "my-custom"} {
		if got := resolveModel(init, in); got != want {
			t.Errorf("resolveModel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := resolveModel(nil, ""); got != "" {
		t.Errorf("no models: %q", got)
	}
}

func TestMapResult(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		r         resultMsg
		cancelled bool
		stop, err string
	}{
		{resultMsg{Subtype: "success", StopReason: str("end_turn")}, false, agentsession.StopEndTurn, ""},
		{resultMsg{Subtype: "success", StopReason: str("refusal")}, false, agentsession.StopRefusal, ""},
		{resultMsg{Subtype: "success", StopReason: str("tool_use")}, false, agentsession.StopEndTurn, ""},
		{resultMsg{Subtype: "error_during_execution", IsError: true}, true, agentsession.StopCancelled, ""},
		{resultMsg{Subtype: "error_during_execution", IsError: true}, false, agentsession.StopError, "turn failed (error_during_execution)"},
	}
	for _, tt := range tests {
		stop, errText := mapResult(&tt.r, tt.cancelled)
		if stop != tt.stop || errText != tt.err {
			t.Errorf("mapResult(%+v, %v) = %q, %q", tt.r, tt.cancelled, stop, errText)
		}
	}
}

func TestTaskList(t *testing.T) {
	var l taskList
	if l.created(json.RawMessage(`{"subject":"x"}`), json.RawMessage(`{"task":{}}`)) {
		t.Error("created without id")
	}
	l.created(json.RawMessage(`{"subject":"A"}`), json.RawMessage(`{"task":{"id":"1"}}`))
	l.created(nil, json.RawMessage(`{"task":{"id":"2","subject":"B"}}`))
	if !l.updated(json.RawMessage(`{"taskId":"1","status":"completed","subject":"A2"}`)) {
		t.Fatal("update failed")
	}
	if l.updated(json.RawMessage(`{"taskId":"9","status":"completed"}`)) || l.updated(json.RawMessage(`{}`)) {
		t.Error("bogus update applied")
	}
	if p := l.plan(); len(p) != 2 || p[0] != (agentsession.PlanEntry{Content: "A2", Status: "completed"}) || p[1].Content != "B" {
		t.Fatalf("plan = %+v", p)
	}
	l.updated(json.RawMessage(`{"taskId":"1","status":"deleted"}`))
	if p := l.plan(); len(p) != 1 || p[0].Content != "B" {
		t.Fatalf("after delete = %+v", p)
	}
	if plan, ok := todoPlan(json.RawMessage(`{"todos":[{"content":"x","status":"weird","priority":"high"}]}`)); !ok || plan[0].Status != "pending" || plan[0].Priority != "high" {
		t.Errorf("todoPlan = %+v", plan)
	}
	if _, ok := todoPlan(json.RawMessage(`{}`)); ok {
		t.Error("todoPlan without todos")
	}
}

func TestResolveBinary(t *testing.T) {
	old := waitShellEnv
	waitShellEnv = func() {}
	defer func() { waitShellEnv = old }()

	dir := t.TempDir()
	bin := filepath.Join(dir, "flint-fake-claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "relative/dir"+string(os.PathListSeparator)+dir)
	if got, err := resolveBinary("flint-fake-claude"); err != nil || got != bin {
		t.Fatalf("resolveBinary = %q, %v", got, err)
	}
	if got, err := resolveBinary(bin); err != nil || got != bin {
		t.Fatalf("absolute = %q, %v", got, err)
	}
	if _, err := resolveBinary("flint-no-such-binary"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing = %v", err)
	}
}

func TestBuildArgsAndEnv(t *testing.T) {
	build := func(resume bool, opts Options, untrusted bool) string {
		t.Helper()
		args, err := buildArgs("sid", resume, opts, untrusted)
		if err != nil {
			t.Fatalf("buildArgs() error = %v", err)
		}
		return strings.Join(args, " ")
	}
	joined := build(false, Options{PermissionMode: "acceptEdits", ExtraArgs: []string{"--add-dir", "/tmp/notes"}}, false)
	if !strings.Contains(joined, "--permission-mode acceptEdits") || strings.Contains(joined, "--model") ||
		!strings.Contains(joined, "--session-id sid") || strings.Contains(joined, "--resume") ||
		!strings.HasPrefix(joined, "--add-dir /tmp/notes -p ") {
		t.Errorf("args = %q", joined)
	}
	resumed := build(true, Options{}, false)
	if untrusted := build(false, Options{ExtraArgs: []string{"--mcp-config", ".mcp.json"}}, true); !strings.HasSuffix(untrusted, "--setting-sources user") ||
		strings.Contains(untrusted, "--mcp-config") {
		t.Errorf("untrusted folder args %q", untrusted)
	}
	if !strings.Contains(resumed, "--resume sid") || strings.Contains(resumed, "--session-id") {
		t.Errorf("resume args = %q", resumed)
	}
	t.Setenv("CLAUDECODE", "1")
	env := buildEnv([]string{"CLAUDE_CODE_ENABLE_TODO_TOOLS=0"}, "/w")
	if lookup(env, "CLAUDECODE") != "" || lookup(env, "CLAUDE_CODE_ENABLE_TODO_TOOLS") != "0" || lookup(env, "PWD") != "/w" {
		t.Errorf("env lookups: %q %q %q", lookup(env, "CLAUDECODE"), lookup(env, "CLAUDE_CODE_ENABLE_TODO_TOOLS"), lookup(env, "PWD"))
	}
	id, err := newSessionID()
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Errorf("session id %q, %v", id, err)
	}
	if !isSessionID(id) || isSessionID("--model=x") || isSessionID("") {
		t.Error("isSessionID accepts only UUIDs")
	}
}

// lookup returns the last value of key in env (os/exec semantics).
func lookup(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, _ := strings.Cut(kv, "="); k == key {
			v = val
		}
	}
	return v
}

func TestExitErrorMessage(t *testing.T) {
	inner := errors.New("exit status 3")
	e := &ExitError{Err: inner, Stderr: " oops \n"}
	if e.Error() != "claude: process exited: exit status 3; stderr: oops" || !errors.Is(e, inner) {
		t.Errorf("Error() = %q", e.Error())
	}
	if (&ExitError{}).Error() != "claude: process exited" {
		t.Error("bare ExitError")
	}
}

func TestRingBufferAndLineReader(t *testing.T) {
	rb := newRingBuffer(8)
	_, _ = rb.Write([]byte("abc"))
	if string(rb.Bytes()) != "abc" {
		t.Fatalf("bytes = %q", rb.Bytes())
	}
	_, _ = rb.Write([]byte("defghij"))
	if string(rb.Bytes()) != "cdefghij" || rb.Tail(3) != "hij" {
		t.Fatalf("wrapped = %q", rb.Bytes())
	}
	_, _ = rb.Write([]byte("0123456789"))
	if string(rb.Bytes()) != "23456789" {
		t.Fatalf("overflow = %q", rb.Bytes())
	}

	lr := newLineReader(strings.NewReader("one\r\n"+strings.Repeat("x", 100)+"\nlast"), 1<<20)
	for _, want := range []string{"one", strings.Repeat("x", 100), "last"} {
		line, err := lr.next()
		if err != nil || string(line) != want {
			t.Fatalf("line = %q, %v", line, err)
		}
	}
	if _, err := lr.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("eof = %v", err)
	}
	small := newLineReader(strings.NewReader(strings.Repeat("y", 200)+"\n"), 10)
	if _, err := small.next(); !errors.Is(err, errMessageTooLarge) {
		t.Fatalf("too large = %v", err)
	}
}

// Settings changes reach new sessions through Options.Settings, and extra
// arguments that would override Flint's flags stop the start.
func TestConfigIsReadOnStart(t *testing.T) {
	cfg := agent.Config{Path: "/nonexistent/from-settings"}
	d := New(Options{Binary: "/bin/sh", Settings: func() agent.Config { return cfg }})
	defer d.Close()
	sink := func(agentsession.Event) {}
	if _, err := d.Start(context.Background(), t.TempDir(), sink); err == nil || !strings.Contains(err.Error(), "from-settings") {
		t.Errorf("Start with configured path: %v", err)
	}
	cfg = agent.Config{Path: "/bin/sh", Args: []string{"--setting-sources", "user,project"}}
	if _, err := d.Start(context.Background(), t.TempDir(), sink); err == nil || !strings.Contains(err.Error(), "--setting-sources") {
		t.Errorf("Start with reserved extra argument: %v", err)
	}
}
