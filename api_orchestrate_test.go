package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/mcptools"
	"flint/internal/orchestrate"
)

// orchApp is a chat app behind the real MCP server: orchestration on, a
// trusted project with one chat in it (the caller, "lead"), a token for that
// chat, and a dialog that counts its questions.
type orchApp struct {
	*grantApp
	url, token string
	caller     atomic.Value // the chat id the token resolves to
}

func newOrchApp(t *testing.T) *orchApp {
	t.Helper()
	g := newGrantApp(t)
	g.settings.MCPServer.Enabled = true
	g.mcp, g.mcpSources = newMCP(g.App)
	t.Cleanup(func() { _ = g.mcp.Close() })
	o := &orchApp{grantApp: g}
	o.caller.Store(g.chat)
	url, token, _, err := g.mcp.Endpoint(g.project, func() string { return o.caller.Load().(string) })
	if err != nil {
		t.Fatal(err)
	}
	o.url, o.token = url, token
	return o
}

// tool calls an MCP tool as the token's chat and returns its text and whether
// it failed.
func (o *orchApp) tool(t *testing.T, name, args string) (string, bool) {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	status, reply := mcpPost(t, o.url, o.token, body)
	var r struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(reply), &r); err != nil || status != http.StatusOK {
		t.Fatalf("%s: %d %s (%v)", name, status, reply, err)
	}
	if r.Error != nil {
		return r.Error.Message, true
	}
	if len(r.Result.Content) != 1 {
		t.Fatalf("%s: %s", name, reply)
	}
	return r.Result.Content[0].Text, r.Result.IsError
}

// ok calls a tool that must succeed.
func (o *orchApp) ok(t *testing.T, name, args string) string {
	t.Helper()
	text, failed := o.tool(t, name, args)
	if failed {
		t.Fatalf("%s %s failed: %s", name, args, text)
	}
	return text
}

// fails calls a tool that must be refused with a message containing want.
func (o *orchApp) fails(t *testing.T, name, args, want string) {
	t.Helper()
	text, failed := o.tool(t, name, args)
	if !failed || !strings.Contains(text, want) {
		t.Fatalf("%s %s = %q (failed %v), want an error containing %q", name, args, text, failed, want)
	}
}

// start starts a child of the caller and returns its id.
func (o *orchApp) start(t *testing.T, args string) string {
	t.Helper()
	var r struct{ ID string }
	if err := json.Unmarshal([]byte(o.ok(t, "start_agent", args)), &r); err != nil || r.ID == "" {
		t.Fatalf("start_agent answer: %v", err)
	}
	return r.ID
}

// idle ends the chat's turn in the registry and waits for the fake driver to
// finish it too, so the chat accepts a message.
func (o *orchApp) idle(t *testing.T, id string) {
	t.Helper()
	o.orch.Observe(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	for i := 0; i < 500 && o.agents.Busy(id); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if o.agents.Busy(id) {
		t.Fatalf("%s stays busy", id)
	}
}

func (o *orchApp) toolNames(t *testing.T) []string {
	t.Helper()
	_, reply := mcpPost(t, o.url, o.token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var r struct {
		Result struct{ Tools []struct{ Name string } }
	}
	if err := json.Unmarshal([]byte(reply), &r); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range r.Result.Tools {
		names = append(names, tl.Name)
	}
	return names
}

func TestOrchestrationToolsAreListedOnlyWhenEnabled(t *testing.T) {
	o := newOrchApp(t)
	count := func() int {
		n := 0
		for _, name := range o.toolNames(t) {
			if mcptools.IsOrchestrationTool(name) {
				n++
			}
		}
		return n
	}
	if got := count(); got != len(mcptools.OrchestrationNames) {
		t.Fatalf("%d orchestration tools listed while enabled", got)
	}
	o.settings.Orchestration.Enabled = false
	if got := count(); got != 0 {
		t.Fatalf("%d orchestration tools listed while off", got)
	}
	o.fails(t, "list_agents", `{}`, "unknown tool")
	if o.asked.Load() != 0 {
		t.Error("the user was asked about a tool that is not offered")
	}
}

func TestOrchestrationNeedsACaller(t *testing.T) {
	o := newOrchApp(t)
	o.caller.Store("")
	o.fails(t, "list_agents", `{}`, "orchestration is only available")
	if o.asked.Load() != 0 {
		t.Error("a call without a caller reached the user")
	}
}

func TestFirstCallAsksOnceThenRuns(t *testing.T) {
	o := newOrchApp(t)
	text := o.ok(t, "list_agents", `{}`)
	if !strings.Contains(text, o.chat) || !strings.Contains(text, `"self": true`) {
		t.Errorf("list_agents = %s", text)
	}
	o.ok(t, "list_agents", `{}`)
	o.start(t, `{"task":"look around"}`)
	if n := o.asked.Load(); n != 1 {
		t.Fatalf("the grant dialog was shown %d times", n)
	}
	if !strings.Contains(o.text, "start and direct up to 4 agents") {
		t.Errorf("dialog: %s", o.text)
	}
}

func TestDeclinedGrantRefusesWithoutAskingAgain(t *testing.T) {
	o := newOrchApp(t)
	o.answer(false)
	for i := 0; i < 3; i++ {
		o.fails(t, "start_agent", `{"task":"x"}`, "did not approve")
	}
	o.fails(t, "list_agents", `{}`, "did not approve")
	if n := o.asked.Load(); n != 1 {
		t.Fatalf("a declined chat was asked %d times", n)
	}
	if got := o.driver.next; got != 1 {
		t.Errorf("%d agents started although the user said no", got-1)
	}
}

func TestRestrictedModeGetsNoGrant(t *testing.T) {
	o := newOrchApp(t)
	o.ok(t, "list_agents", `{}`) // granted while the folder was trusted
	o.state.TrustedFolders = nil // then it lost its trust
	if o.orchGranted(o.chat) {
		t.Error("the grant outlived the folder's trust")
	}
	// The server turns the whole project away.
	status, _ := mcpPost(t, o.url, o.token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_agents","arguments":{}}}`)
	if status != http.StatusForbidden {
		t.Errorf("status %d for an untrusted folder", status)
	}
	if n := o.asked.Load(); n != 1 {
		t.Errorf("the user was asked %d times", n)
	}
}

func TestStartAgentOpensAChildOfTheCaller(t *testing.T) {
	o := newOrchApp(t)
	writeRoleFile(t, orchestrate.PersonalRolesDir(o.configDir), "fixer.md", "---\nagent: fake\n---\nYou fix bugs.")
	id := o.start(t, `{"role":"fixer","task":"fix it, token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"}`)

	n, ok := o.orch.Get(id)
	if !ok || n.Parent != o.chat || n.Role != "fixer" || n.Agent != "fake" || n.Depth != 1 || n.Cwd != o.project {
		t.Fatalf("child = %+v, %v", n, ok)
	}
	got := o.driver.waitPrompts(t, id, 1)
	if !strings.HasPrefix(got[0], "You fix bugs.\n\nfix it") || strings.Contains(got[0], "ghp_0123") {
		t.Errorf("child's prompt = %q", got[0])
	}
	var shown *as.Event
	for _, e := range o.userMessages() {
		if e.SessionID == id {
			shown = &e
		}
	}
	if shown == nil || shown.Origin == nil || shown.Origin.SessionID != o.chat || shown.Origin.Agent != "fake" {
		t.Errorf("the child's transcript shows %+v", shown)
	}
	last := o.opened[len(o.opened)-1]
	if last.Info.ID != id || last.Parent != o.chat || !last.Background {
		t.Errorf("chat:opened = %+v", last)
	}
	if !o.orchGranted(id) {
		t.Error("the child did not inherit the grant")
	}
}

func TestStartAgentValidatesItsArguments(t *testing.T) {
	o := newOrchApp(t)
	o.ok(t, "list_agents", `{}`) // get the grant out of the way
	for _, tc := range []struct{ args, want string }{
		{`{}`, "task is required"},
		{`{"task":"   "}`, "task is required"},
		{`{"task":"x","extra":1}`, "invalid arguments"},
		{`{"task":"x","agent":"../codex"}`, "agent must be"},
		{`{"task":"x","role":"a b"}`, "role must be"},
		{`{"task":"x","role":"nope"}`, "unknown role"},
		{`{"task":"x","base":"main"}`, "only used with worktree"},
		{`{"task":"x","worktree":true,"base":"--upload-pack=x"}`, "base must be"},
		{`{"task":"x","worktree":true,"base":"main","extra":1}`, "invalid arguments"},
		{`{"task":"x","agent":"missing-agent"}`, ""},
		{`{"task":"` + strings.Repeat("a", 33<<10) + `"}`, "longer than"},
		{"{\"task\":\"a\\u0000b\"}", "control character"},
	} {
		text, failed := o.tool(t, "start_agent", tc.args)
		if !failed || !strings.Contains(text, tc.want) {
			t.Errorf("start_agent %.60s = %q (failed %v), want %q", tc.args, text, failed, tc.want)
		}
	}
	if o.driver.next != 1 {
		t.Errorf("a refused start left %d agents running", o.driver.next-1)
	}
}

func TestChildAndDepthLimits(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxChildren = 2
	o.settings.Orchestration.MaxDepth = 2
	a := o.start(t, `{"task":"one"}`)
	o.start(t, `{"task":"two"}`)
	o.fails(t, "start_agent", `{"task":"three"}`, "already have 2 agents")

	// A child may start agents one level down, a grandchild may not.
	o.caller.Store(a)
	g := o.start(t, `{"task":"grandchild"}`)
	o.caller.Store(g)
	o.fails(t, "start_agent", `{"task":"too deep"}`, "depth limit")
	// It can still look around.
	o.ok(t, "list_agents", `{}`)

	// Stopping a child frees its slot.
	o.caller.Store(o.chat)
	o.ok(t, "stop_agent", fmt.Sprintf(`{"id":%q}`, a))
	o.start(t, `{"task":"three"}`)
}

func TestSendPromptOnlyReachesDescendants(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	o.idle(t, child)
	stranger, _ := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: o.project}) // not started by the caller
	outside, _ := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: t.TempDir()})

	send := func(id string) (string, bool) {
		return o.tool(t, "send_prompt", fmt.Sprintf(`{"id":%q,"text":"also check main.go"}`, id))
	}
	if text, failed := send(stranger.ID); !failed || !strings.Contains(text, "not your descendant") {
		t.Errorf("stranger: %q", text)
	}
	if text, failed := send(outside.ID); !failed || !strings.Contains(text, "not in this project") {
		t.Errorf("outside the project: %q", text)
	}
	if text, failed := send("nope"); !failed || !strings.Contains(text, "unknown agent") {
		t.Errorf("unknown: %q", text)
	}
	if got := len(o.driver.got(stranger.ID)) + len(o.driver.got(outside.ID)); got != 0 {
		t.Errorf("%d prompts reached chats the caller may not direct", got)
	}
	if text, failed := send(child); failed {
		t.Fatalf("child: %q", text)
	}
	got := o.driver.waitPrompts(t, child, 2)
	if got[1] != "also check main.go" {
		t.Errorf("child got %q", got)
	}
}

func TestSendPromptRefusalsLeaveTheTargetUnprompted(t *testing.T) {
	permission := as.Event{Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p"}}
	for _, tc := range []struct {
		name  string
		setup func(o *orchApp, id string)
		want  string
	}{
		{"waiting for approval", func(o *orchApp, id string) {
			e := permission
			e.SessionID = id
			o.orch.Observe(e)
		}, "waiting for approval"},
		{"draft open", func(o *orchApp, id string) {
			o.idle(t, id)
			if err := (&AgentChatAPI{app: o.App}).DraftChanged(id, true); err != nil {
				t.Fatal(err)
			}
		}, "draft open"},
		{"busy", func(o *orchApp, id string) {}, "busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newOrchApp(t)
			child := o.start(t, `{"task":"work"}`)
			o.driver.waitPrompts(t, child, 1)
			tc.setup(o, child)
			o.fails(t, "send_prompt", fmt.Sprintf(`{"id":%q,"text":"more"}`, child), tc.want)
			if got := o.driver.got(child); len(got) != 1 {
				t.Errorf("the target was prompted again: %q", got)
			}
		})
	}
}

func TestSendPromptValidatesItsArguments(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	o.idle(t, child)
	for _, tc := range []struct{ args, want string }{
		{`{"text":"x"}`, "id must be"},
		{`{"id":"../c2","text":"x"}`, "id must be"},
		{fmt.Sprintf(`{"id":%q,"text":""}`, child), "text is required"},
		{fmt.Sprintf(`{"id":%q,"text":"x","extra":1}`, child), "invalid arguments"},
	} {
		o.fails(t, "send_prompt", tc.args, tc.want)
	}
}

func TestSendPromptHonoursTheUsageGate(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	o.idle(t, child)
	o.settings.Usage.PauseOverBudget = true
	o.settings.Usage.DailyBudgetTokens = 100
	o.openUsage()
	t.Cleanup(o.closeUsage)
	o.recordUsage(child, mustChat(t, o.App, child), &as.Usage{InputTokens: 90, OutputTokens: 20})
	before := len(o.driver.got(child))

	o.fails(t, "send_prompt", fmt.Sprintf(`{"id":%q,"text":"more"}`, child), "paused")
	if got := len(o.driver.got(child)); got != before {
		t.Error("a message the usage gate refused reached the agent")
	}
	o.fails(t, "start_agent", `{"task":"more"}`, "paused")
}

func TestWaitForAgent(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	id := fmt.Sprintf(`"id":%q`, child)

	// Waits on the registry and wakes when the child reaches a state.
	done := make(chan string, 1)
	go func() {
		text, _ := o.tool(t, "wait_for_agent", `{`+id+`,"until":["finished","failed"],"timeoutSeconds":30}`)
		done <- text
	}()
	select {
	case text := <-done:
		t.Fatalf("returned before the child finished: %s", text)
	case <-time.After(200 * time.Millisecond):
	}
	o.orch.Observe(as.Event{SessionID: child, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	var res struct {
		State    string
		Error    string
		TimedOut bool
	}
	select {
	case text := <-done:
		if err := json.Unmarshal([]byte(text), &res); err != nil || res.State != "finished" || res.TimedOut {
			t.Fatalf("wait = %s (%v)", text, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait_for_agent did not return after the child finished")
	}

	// Already there: returns at once. A failed child carries its error.
	o.orch.Prompted(child)
	o.orch.Observe(as.Event{SessionID: child, Kind: as.KindError, Text: "boom api_key=sk-abcdefghijklmnopqrstuvwxyz0123456789"})
	text := o.ok(t, "wait_for_agent", `{`+id+`,"until":["failed"]}`)
	if err := json.Unmarshal([]byte(text), &res); err != nil || res.State != "failed" || !strings.Contains(res.Error, "boom") || strings.Contains(res.Error, "sk-abcdef") {
		t.Fatalf("failed wait = %s", text)
	}

	// Times out with the state it is in.
	o.orch.Prompted(child)
	start := time.Now()
	text = o.ok(t, "wait_for_agent", `{`+id+`,"until":["finished"],"timeoutSeconds":1}`)
	if err := json.Unmarshal([]byte(text), &res); err != nil || !res.TimedOut || res.State != "working" {
		t.Fatalf("timeout wait = %s", text)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 5*time.Second {
		t.Errorf("timed out after %v", d)
	}

	// The setting caps what the agent asks for.
	o.settings.Orchestration.MaxWaitSeconds = 1
	start = time.Now()
	text = o.ok(t, "wait_for_agent", `{`+id+`,"until":["finished"],"timeoutSeconds":500}`)
	if err := json.Unmarshal([]byte(text), &res); err != nil || !res.TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("capped wait = %s after %v", text, time.Since(start))
	}
}

func TestWaitForAgentValidatesAndScopes(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	outside, _ := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: t.TempDir()})
	for _, tc := range []struct{ args, want string }{
		{fmt.Sprintf(`{"id":%q}`, child), "until"},
		{fmt.Sprintf(`{"id":%q,"until":[]}`, child), "until is required"},
		{fmt.Sprintf(`{"id":%q,"until":["asleep"]}`, child), "not a state"},
		{fmt.Sprintf(`{"id":%q,"until":["idle"],"timeoutSeconds":0}`, child), "at least 1"},
		{`{"id":"nope","until":["idle"]}`, "unknown agent"},
		{fmt.Sprintf(`{"id":%q,"until":["idle"]}`, outside.ID), "not in this project"},
	} {
		o.fails(t, "wait_for_agent", tc.args, tc.want)
	}
}

func TestReadAgentOutputIsRedactedAndLimited(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	var text strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&text, "line %d\n", i)
	}
	text.WriteString("token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\n")
	o.orch.Observe(as.Event{SessionID: child, Kind: as.KindMessageChunk, Text: text.String()})

	out := o.ok(t, "read_agent_output", fmt.Sprintf(`{"id":%q}`, child))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 200 || strings.Contains(out, "ghp_0123") || !strings.Contains(out, "line 299") {
		t.Fatalf("default output: %d lines, redacted %v", len(lines), !strings.Contains(out, "ghp_0123"))
	}
	if got := o.ok(t, "read_agent_output", fmt.Sprintf(`{"id":%q,"lines":5}`, child)); len(strings.Split(got, "\n")) != 5 {
		t.Errorf("5 lines: %q", got)
	}
	o.settings.Orchestration.OutputLines = 10
	if got := o.ok(t, "read_agent_output", fmt.Sprintf(`{"id":%q,"lines":500}`, child)); len(strings.Split(got, "\n")) != 10 {
		t.Errorf("the setting must cap lines: %d", len(strings.Split(got, "\n")))
	}
	o.fails(t, "read_agent_output", fmt.Sprintf(`{"id":%q,"lines":-1}`, child), "positive")
}

func TestStopAgentStopsTheSubtreeAndOnlyDescendants(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	o.caller.Store(child)
	grandchild := o.start(t, `{"task":"sub"}`)
	o.caller.Store(o.chat)
	stranger, _ := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: o.project})

	o.fails(t, "stop_agent", fmt.Sprintf(`{"id":%q}`, stranger.ID), "not your descendant")
	o.fails(t, "stop_agent", fmt.Sprintf(`{"id":%q}`, o.chat), "not your descendant")
	o.ok(t, "stop_agent", fmt.Sprintf(`{"id":%q}`, child))

	for _, id := range []string{child, grandchild} {
		if _, ok := o.orch.Get(id); ok {
			t.Errorf("%s is still registered", id)
		}
		if o.orchGranted(id) {
			t.Errorf("%s keeps its grant", id)
		}
	}
	stopped := strings.Join(o.driver.stopped, ",")
	if stopped != grandchild+","+child {
		t.Errorf("stopped %q, want the grandchild first and then the child", stopped)
	}
	if _, ok := o.orch.Get(stranger.ID); !ok {
		t.Error("an agent that is not a descendant was stopped")
	}
}

func TestListAgentsShowsOnlyTheProject(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	outside, _ := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: t.TempDir()})
	text := o.ok(t, "list_agents", `{}`)
	var r struct {
		Agents []mcptools.AgentInfo
	}
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Agents) != 2 || strings.Contains(text, outside.ID) {
		t.Fatalf("agents = %+v", r.Agents)
	}
	c := r.Agents[1]
	if c.ID != child || c.Parent != o.chat || c.Depth != 1 || c.Cwd != "." || c.State != "working" || c.Kind != "chat" || r.Agents[0].Self != true {
		t.Errorf("child listed as %+v", c)
	}
}
