package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"flint/internal/mcpserver"
)

func (f *fixture) allowAdd() {
	s := f.h.Settings()
	s.MCPServer.Actions.Projects = true
	f.h.SetSettings(s)
}

// add plans and runs one add_project_item call as chat "chat-1".
func (f *fixture) add(args string) (string, error) {
	return f.addAs("chat-1", args)
}

func (f *fixture) addAs(chat, args string) (string, error) {
	ctx := mcpserver.WithCaller(f.t.Context(), chat)
	p, err := f.api.planAdd(ctx, f.root, json.RawMessage(args))
	if err != nil {
		return "", err
	}
	return p.Run(ctx)
}

func TestAddToolIsOptInAndActs(t *testing.T) {
	f := newFixture(t, nil)
	names := func() []string {
		var out []string
		for _, tool := range f.api.agentTools() {
			out = append(out, tool.Name)
		}
		return out
	}
	if got := names(); len(got) != 1 || got[0] != ToolName {
		t.Fatalf("tools by default: %v", got)
	}
	f.allowAdd()
	tools := f.api.agentTools()
	if len(tools) != 2 || tools[1].Name != "add_project_item" || !tools[1].Acts || tools[1].Plan == nil || tools[1].Call != nil {
		t.Fatalf("tools = %+v", tools)
	}
	var schema map[string]any
	if err := json.Unmarshal(tools[1].InputSchema, &schema); err != nil || schema["additionalProperties"] != false {
		t.Fatalf("schema %s: %v", tools[1].InputSchema, err)
	}
	if tools[0].Acts {
		t.Error("get_project acts")
	}
}

// Through Flint's MCP server: nothing is written without the user's yes,
// and the question shows what will be written.
func TestAddToolNeedsApproval(t *testing.T) {
	f := newFixture(t, map[string]string{"a": item("Alpha", "done")})
	f.open()
	f.allowAdd()
	var mu sync.Mutex
	var asked []mcpserver.Approval
	answer := mcpserver.ErrNotApproved
	s := mcpserver.New(mcpserver.Options{Tools: f.api.agentTools, Approve: func(_ context.Context, a mcpserver.Approval) error {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, a)
		return answer
	}})
	t.Cleanup(func() { _ = s.Close() })
	url, token, _, err := s.Endpoint(f.root, func() string { return "chat-9" })
	if err != nil {
		t.Fatal(err)
	}
	call := func(args string) (string, bool) {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_project_item","arguments":%s}}`, args)
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct {
			Result struct {
				Content []struct{ Text string } `json:"content"`
				IsError bool                    `json:"isError"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || len(r.Result.Content) != 1 {
			t.Fatalf("reply: %+v %v", r, err)
		}
		return r.Result.Content[0].Text, r.Result.IsError
	}
	args := `{"project":"Acme","title":"Beta","text":"Do beta.","priority":"high","repos":["api"],"after":["a"]}`
	if text, isErr := call(args); !isErr || text != mcpserver.ErrNotApproved.Error() {
		t.Fatalf("denied: %q %v", text, isErr)
	}
	if _, err := os.Stat(ItemFile(f.plan, "beta")); err == nil {
		t.Fatal("written without approval")
	}
	if len(asked) != 1 || asked[0].Tool != "add_project_item" || !strings.Contains(asked[0].Summary, `project "Acme"`) ||
		!strings.Contains(asked[0].Summary, "Title: Beta") || !strings.Contains(asked[0].Summary, "Priority: high · Repositories: api · After: a") ||
		!strings.Contains(asked[0].Summary, "File: items/beta.md") || !strings.Contains(asked[0].Summary, "Do beta.") {
		t.Fatalf("asked %+v", asked)
	}
	// A refused call is refused before the user is asked.
	if text, isErr := call(`{"project":"Acme","title":"Beta","text":"","after":["zz"]}`); !isErr || !strings.Contains(text, `no item "zz"`) || len(asked) != 1 {
		t.Fatalf("refusal: %q %v, asked %d", text, isErr, len(asked))
	}
	answer = nil
	if text, isErr := call(args); isErr || !strings.Contains(text, "Added the item beta (items/beta.md)") || !strings.Contains(text, "It waits for a.") {
		t.Fatalf("approved: %q %v", text, isErr)
	}
	if got := f.read("beta"); got != "---\ntitle: Beta\nstatus: todo\npriority: high\nrepos: api\nafter: a\n---\nDo beta.\n" {
		t.Errorf("file %q", got)
	}
	if f.api.added["chat-9"] != 1 {
		t.Errorf("counted %v", f.api.added)
	}
}

func TestAddToolRefusals(t *testing.T) {
	f := newFixture(t, map[string]string{
		"a":      item("Alpha", "todo"),
		"dangle": item("Dangle", "todo", "after: new-one"),
	})
	f.open()
	untrusted := newProject(t, filepath.Join(f.base, "other"), nil)
	if _, err := f.api.Open(untrusted); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ args, want string }{
		{`{"project":"Acme","title":"T","text":"","extra":1}`, `unknown field "extra"`},
		{`{"project":"Acme","title":"T","text":""} {}`, "more than one object"},
		{`{"project":"Acme","title":"T"}`, "text is required"},
		{`{"title":"T","text":""}`, "project is required"},
		{`{"project":"Nope","title":"T","text":""}`, `no open project is "Nope"`},
		{`{"project":"` + f.base + `","title":"T","text":""}`, "no open project is"},
		{`{"project":"` + untrusted + `","title":"T","text":""}`, "Restricted Mode"},
		{`{"project":"Acme","title":"","text":""}`, "give the item a title"},
		{`{"project":"Acme","title":"` + strings.Repeat("x", MaxTitle+1) + `","text":""}`, "longer than 120"},
		{`{"project":"Acme","title":"a\nstatus: done","text":""}`, "one line"},
		{`{"project":"Acme","title":"a\u200bb","text":""}`, "invisible"},
		{`{"project":"Acme","title":"T","text":"\u202e"}`, "invisible characters (U+202E)"},
		{`{"project":"Acme","title":"T","text":"` + strings.Repeat("x", MaxDescription) + `"}`, "longer than 8000 bytes"},
		{`{"project":"Acme","title":"T","text":"` + strings.Repeat("x", MaxDescription+1) + `"}`, "too long"},
		{`{"project":"Acme","title":"T","text":"","priority":"urgent"}`, "priority"},
		{`{"project":"Acme","title":"T","text":"","agent":"planner"}`, `"planner" is not available`},
		{`{"project":"Acme","title":"T","text":"","agent":"bad name"}`, "is not available"},
		{`{"project":"Acme","title":"T","text":"","repos":["db"]}`, `repository "db" is not in`},
		{`{"project":"Acme","title":"T","text":"","repos":"api"}`, "invalid arguments"},
		{`{"project":"Acme","title":"T","text":"","after":["zz"]}`, `no item "zz"`},
		{`{"project":"Acme","title":"T","text":"","after":["t"]}`, `no item "t"`},
		{`{"project":"Acme","title":"T","text":"","after":["a","a"]}`, "listed twice"},
		{`{"project":"Acme","title":"T","text":"","after":["../a"]}`, "not an item id"},
		{`{"project":"Acme","title":"New one","text":"","after":["dangle"]}`, "after: a cycle: new-one → dangle → new-one"},
	} {
		_, err := f.add(tc.args)
		wantErr(t, err, tc.want)
	}
	entries, _ := os.ReadDir(filepath.Join(f.plan, ItemsDir))
	if len(entries) != 2 {
		t.Errorf("files written: %v", entries)
	}
	// Two trusted projects with one name: the folder decides.
	f.h.SetTrusted(untrusted)
	_, err := f.add(`{"project":"Acme","title":"T","text":""}`)
	wantErr(t, err, `several open projects are named "Acme": pass the folder`)
	if _, err := f.add(`{"project":"` + untrusted + `","title":"T","text":""}`); err != nil {
		t.Fatal(err)
	}
}

// The id comes from the title; an existing file is never overwritten.
func TestAddToolIDsAndLimits(t *testing.T) {
	f := newFixture(t, map[string]string{"alpha": item("Alpha", "done")})
	f.open()
	before := f.read("alpha")
	for i, want := range []string{"alpha-2", "alpha-3"} {
		out, err := f.add(`{"project":"` + f.plan + `","title":"Alpha","text":"Again."}`)
		if err != nil || !strings.Contains(out, "Added the item "+want+" ") {
			t.Fatalf("%d: %q %v", i, out, err)
		}
	}
	if f.read("alpha") != before || !strings.Contains(f.read("alpha-3"), "status: todo") {
		t.Error("an item was overwritten")
	}
	b := f.load()
	if len(b.Items) != 3 || len(b.Problems) != 0 {
		t.Errorf("loaded %+v", b)
	}
	// Per chat.
	f.api.added["chat-1"] = maxAddsPerChat
	_, err := f.add(`{"project":"Acme","title":"More","text":""}`)
	wantErr(t, err, "this chat added 50 items to projects")
	if _, err := f.addAs("chat-2", `{"project":"Acme","title":"More","text":""}`); err != nil {
		t.Fatal(err)
	}
	// The project's limit.
	g := newFixture(t, nil)
	g.open()
	for i := range MaxItems {
		write(t, ItemFile(g.plan, fmt.Sprintf("i%d", i)), item("I", "todo"))
	}
	_, err = g.add(`{"project":"Acme","title":"One more","text":""}`)
	wantErr(t, err, "the project has 500 items")
}

// The write checks again: a project that became untrusted while the user
// was asked is refused.
func TestAddToolChecksAgainAfterTheQuestion(t *testing.T) {
	f := newFixture(t, nil)
	other := newProject(t, filepath.Join(f.base, "other"), nil)
	f.h.SetTrusted(other)
	f.open()
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	ctx := mcpserver.WithCaller(t.Context(), "c")
	p, err := f.api.planAdd(ctx, f.root, json.RawMessage(`{"project":"`+other+`","title":"T","text":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.api.Close(other); err != nil {
		t.Fatal(err)
	}
	_, err = p.Run(ctx)
	wantErr(t, err, "no open project")
}

func TestPlanPrompt(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	got, err := f.api.PlanPrompt(f.plan, "  Add rate limiting\r\nto checkout ")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Plan this goal as work items of the Flint project "Acme" (folder ` + f.plan + "):\n\nAdd rate limiting\nto checkout\n\n",
		"get_project tool", "add_project_item tool", `- project: "` + f.plan + `"`, "at most 120 characters", "at most 8000 bytes",
		"one of claude, backend; default: claude", "  - api: git@github.com:acme/api.git\n  - web: https://github.com/acme/web.git",
		"after (optional)", "at most 20",
	} {
		if !strings.Contains(got.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got.Prompt)
		}
	}
	if got.ToolOn {
		t.Error("ToolOn while the switch is off")
	}
	f.allowAdd()
	if got, _ := f.api.PlanPrompt(f.plan, "x"); !got.ToolOn {
		t.Error("ToolOn is false")
	}
	for _, tc := range []struct{ goal, want string }{
		{"  ", "say what the goal is"},
		{strings.Repeat("é", MaxGoal+1), "longer than 2000"},
		{"a\x1bb", "control or invisible character (U+001B)"},
		{"a\u2066b", "U+2066"},
	} {
		_, err := f.api.PlanPrompt(f.plan, tc.goal)
		wantErr(t, err, tc.want)
	}
	_, err = f.api.PlanPrompt(f.base, "x")
	wantErr(t, err, "not an open project")
	other := newProject(t, filepath.Join(f.base, "other"), nil)
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.PlanPrompt(other, "x")
	wantErr(t, err, "Restricted Mode")
}

func TestGetProjectShowsDependencies(t *testing.T) {
	f := newFixture(t, map[string]string{
		"a": item("Alpha", "done"),
		"b": item("Bravo", "todo", "after: a"),
		"c": item("Charlie", "todo", "after: b, a"),
		"d": item("Delta", "todo", "after: d"),
	})
	f.open()
	out := f.api.describe("", "")
	for _, want := range []string{
		"## b: Bravo\nstatus: todo; priority: normal; after: a (ready)\n",
		"## c: Charlie\nstatus: todo; priority: normal; after: b, a (not ready: waits for b)\n",
		"## d: Delta\nstatus: todo; priority: normal; after: d (never ready: after: the item names itself)\n",
		"## a: Alpha\nstatus: done; priority: normal\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("get_project lacks %q:\n%s", want, out)
		}
	}
}
