package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/httpfile"
	"flint/internal/mcpserver"
	"flint/internal/runner"
	"flint/internal/settings"
)

// actingApp is an App with the MCP server, a runner and an HTTP client, a
// trusted project and a recorded answer to every confirmation.
type actingApp struct {
	*App
	project    string
	url, token string

	mu     sync.Mutex
	asked  []string
	answer bool
}

func newActingApp(t *testing.T, actions settings.MCPActions) *actingApp {
	t.Helper()
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Default()
	s.MCPServer.Actions = actions
	s.MCPServer.TestWaitSeconds = 60
	a := &actingApp{App: &App{configDir: t.TempDir(), settings: s, http: httpfile.NewClient()}, project: project}
	a.runEvents = newRunEvents(a.App)
	a.runs = runner.NewManager(a.runEvents)
	a.mcp, a.mcpSources = newMCP(a.App)
	a.ask = func(_, message, _ string) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.asked = append(a.asked, message)
		return a.answer
	}
	t.Cleanup(func() {
		a.runs.CloseAll()
		_ = a.mcp.Close()
	})
	if err := a.setTrust(project, true); err != nil {
		t.Fatal(err)
	}
	srv, ok := a.offerMCP(project)
	if !ok {
		t.Fatal("no MCP server offered")
	}
	a.url, a.token = srv.URL, srv.Token
	return a
}

func (a *actingApp) questions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.asked...)
}

func (a *actingApp) allow(yes bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answer = yes
}

// tool calls a tool and returns its text and whether it failed.
func (a *actingApp) tool(t *testing.T, name, args string) (string, bool) {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	status, reply := mcpPost(t, a.url, a.token, body)
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

func (a *actingApp) write(t *testing.T, rel, content string) string {
	t.Helper()
	path := filepath.Join(a.project, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The tools that act are not listed (and can't be called) until their group
// is switched on, one group at a time.
func TestActingToolsAreListedOnlyWhenSwitchedOn(t *testing.T) {
	a := newActingApp(t, settings.MCPActions{})
	list := func() string {
		_, body := mcpPost(t, a.url, a.token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		return body
	}
	acting := []string{"run_tests", "run_configuration", "get_run_output", "send_http_request", "run_sql_query"}
	body := list()
	for _, name := range acting {
		if strings.Contains(body, name) {
			t.Errorf("%s is listed by default", name)
		}
	}
	if text, failed := a.tool(t, "run_tests", `{"path":"."}`); !failed || !strings.Contains(text, "unknown tool") {
		t.Fatalf("a switched-off tool answered: %q", text)
	}
	if len(a.questions()) != 0 {
		t.Fatal("asked about a tool that is off")
	}
	for _, tc := range []struct {
		actions settings.MCPActions
		want    []string
	}{
		{settings.MCPActions{Tests: true}, []string{"run_tests"}},
		{settings.MCPActions{Run: true}, []string{"run_configuration", "get_run_output"}},
		{settings.MCPActions{HTTP: true}, []string{"send_http_request"}},
	} {
		s := settings.Default()
		s.MCPServer.Actions = tc.actions
		a.setSettingsForTest(s)
		body := list()
		for _, name := range acting {
			if listed, want := strings.Contains(body, `"`+name+`"`), strings.Contains(strings.Join(tc.want, " "), name); listed != want {
				t.Errorf("%+v: %s listed = %v", tc.actions, name, listed)
			}
		}
		if !a.mcp.Acts(tc.want[0]) || a.mcp.Acts("get_run_output") || a.mcp.Acts("get_diagnostics") {
			t.Errorf("%+v: Acts is wrong", tc.actions)
		}
	}
	// Restricted Mode: nothing at all, as for the read tools.
	if err := a.setTrust(a.project, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := mcpPost(t, a.url, a.token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); status != http.StatusForbidden {
		t.Fatalf("untrusted folder: %d", status)
	}
}

// run_configuration starts nothing without the user's yes; with it the
// configuration runs through Flint's runner and get_run_output reads it.
func TestRunConfigurationNeedsApproval(t *testing.T) {
	a := newActingApp(t, settings.MCPActions{Run: true})
	a.write(t, ".flint/run.json", `{"version":1,"configurations":[{"id":"hello","name":"Say hello","type":"command","command":"echo","args":["hello-from-flint"]}]}`)

	text, failed := a.tool(t, "run_configuration", `{"name":"Say hello"}`)
	if !failed || text != mcpserver.ErrNotApproved.Error() {
		t.Fatalf("denied: %q %v", text, failed)
	}
	asked := a.questions()
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	for _, want := range []string{a.project, `"Run a configuration" (run_configuration)`, "Run configuration: Say hello", "echo hello-from-flint", `Arguments: {"name":"Say hello"}`} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the question lacks %q:\n%s", want, asked[0])
		}
	}
	if list := a.mcpSources.Terminals.List(a.project); len(list) != 0 {
		t.Fatalf("a denied call started %+v", list)
	}
	// A call that can't run isn't asked about.
	if text, failed = a.tool(t, "run_configuration", `{"name":"nope"}`); !failed || !strings.Contains(text, "no run configuration named") || len(a.questions()) != 1 {
		t.Fatalf("unknown configuration: %q, asked %d", text, len(a.questions()))
	}

	a.allow(true)
	text, failed = a.tool(t, "run_configuration", `{"name":"Say hello"}`)
	if failed || !strings.Contains(text, `Started "Say hello"`) {
		t.Fatalf("approved: %q %v", text, failed)
	}
	list := a.mcpSources.Terminals.List(a.project)
	if len(list) != 1 || !strings.HasPrefix(list[0].ID, "agent-") || !strings.Contains(text, list[0].ID) {
		t.Fatalf("consoles %+v, answer %q", list, text)
	}
	if got := a.configRuns.current(a.project, "hello"); got != list[0].ID {
		t.Errorf("the configuration's run is %q", got)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		text, failed = a.tool(t, "get_run_output", fmt.Sprintf(`{"run":%q}`, list[0].ID))
		if failed {
			t.Fatalf("get_run_output: %s", text)
		}
		if strings.Contains(text, "exited with code 0") && strings.Contains(text, "hello-from-flint") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run never finished:\n%s", text)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(a.questions()) != 2 {
		t.Fatalf("reading the output asked the user: %d questions", len(a.questions()))
	}
}

// A yes the user gave in the chat (the agent's own permission request) is
// good for exactly one call with the same tool and arguments.
func TestChatApprovalIsAOneTimeGrant(t *testing.T) {
	a := newActingApp(t, settings.MCPActions{Run: true})
	a.write(t, ".flint/run.json", `{"version":1,"configurations":[
		{"id":"one","name":"one","type":"command","command":"true"},
		{"id":"two","name":"two","type":"command","command":"true"}]}`)
	gate := a.actionGate()
	if !gate.Acts("run_configuration") || gate.Acts("get_run_output") || gate.Acts("get_diagnostics") {
		t.Fatal("the gate names the wrong tools")
	}
	// The agent wrote the arguments its own way; the values are what counts.
	gate.Approved(a.project+"/", "run_configuration", json.RawMessage(`{ "name" : "one" }`))

	if text, failed := a.tool(t, "run_configuration", `{"name":"two"}`); !failed || len(a.questions()) != 1 {
		t.Fatalf("other arguments used the grant: %q", text)
	}
	if text, failed := a.tool(t, "run_configuration", `{"name":"one"}`); failed || len(a.questions()) != 1 {
		t.Fatalf("the granted call: %q, asked %d", text, len(a.questions()))
	}
	if text, failed := a.tool(t, "run_configuration", `{"name":"one"}`); !failed || len(a.questions()) != 2 {
		t.Fatalf("the grant was used twice: %q", text)
	}
	// Another project's approval is no grant here.
	gate.Approved(t.TempDir(), "run_configuration", json.RawMessage(`{"name":"one"}`))
	gate.Approved("relative", "run_configuration", json.RawMessage(`{"name":"one"}`))
	if _, failed := a.tool(t, "run_configuration", `{"name":"one"}`); !failed || len(a.questions()) != 3 {
		t.Fatal("another project's approval was used")
	}
}

func TestActionGrantsExpireAndAreBounded(t *testing.T) {
	now := time.Unix(1000, 0)
	g := &actionGrants{now: func() time.Time { return now }}
	args := json.RawMessage(`{"a":1,"b":[1.0,2]}`)
	g.add("/p", "t", args)
	if g.take("/p", "other", args) || g.take("/q", "t", args) || g.take("/p", "t", json.RawMessage(`{"a":1,"b":[1,2]}`)) {
		t.Fatal("a grant matched another call")
	}
	if !g.take("/p", "t", json.RawMessage(`{"b":[1.0,2],"a":1}`)) || g.take("/p", "t", args) {
		t.Fatal("the grant isn't good for exactly one call")
	}
	g.add("/p", "t", args)
	now = now.Add(grantTTL)
	if g.take("/p", "t", args) {
		t.Fatal("an expired grant was used")
	}
	for i := range maxGrants + 10 {
		g.add("/p", "t", json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)))
	}
	if len(g.until) != maxGrants {
		t.Fatalf("%d grants kept", len(g.until))
	}
	for raw, want := range map[string]string{``: `{}`, `null`: `{}`, ` {"b":1, "a":2} `: `{"a":2,"b":1}`, `{"n":12345678901234567890}`: `{"n":12345678901234567890}`, `{"a":1} x`: `invalid:{"a":1} x`} {
		if got := canonicalArgs(json.RawMessage(raw)); got != want {
			t.Errorf("canonicalArgs(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The question is bounded and redacted, and an answer that comes after the
// call gave up runs nothing.
func TestApproveAction(t *testing.T) {
	q := actionQuestion(mcpserver.Approval{Root: "/p", Tool: "x_tool", Summary: "Do it",
		Args: json.RawMessage(`{"note":"` + strings.Repeat("y", 5000) + `","key":"AKIAABCDEFGHIJKLMNOP"}`)})
	if len(q) > maxDialogArgs+400 || strings.Contains(q, "AKIAABCDEFGHIJKLMNOP") || !strings.Contains(q, `"x_tool" (x_tool)`) || !strings.Contains(q, "Do it") {
		t.Fatalf("question (%d bytes): %s", len(q), q)
	}

	a := &App{}
	release := make(chan struct{})
	a.ask = func(string, string, string) bool {
		<-release
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := a.approveAction(ctx, mcpserver.Approval{Root: "/p", Tool: "t"})
	close(release)
	if err == nil || !strings.Contains(err.Error(), "in time") {
		t.Fatalf("unanswered question: %v", err)
	}
}

// send_http_request sends the named request through Flint's HTTP client
// after the user's yes, and the response is kept in the file's history.
func TestSendHTTPRequestTool(t *testing.T) {
	var hits int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "sid=abcdef0123456789")
		_, _ = fmt.Fprintf(w, `{"path":%q,"api_key":"sk-live-0123456789abcdef0123456789abcdef"}`, r.URL.Path)
	}))
	t.Cleanup(server.Close)
	a := newActingApp(t, settings.MCPActions{HTTP: true})
	file := a.write(t, "api/ping.http", "### ping\nGET "+server.URL+"/ping\n")

	if text, failed := a.tool(t, "send_http_request", `{"file":"api/ping.http","request":"ping"}`); !failed || text != mcpserver.ErrNotApproved.Error() {
		t.Fatalf("denied: %q", text)
	}
	mu.Lock()
	if hits != 0 {
		t.Fatal("a denied request was sent")
	}
	mu.Unlock()
	if asked := a.questions(); len(asked) != 1 || !strings.Contains(asked[0], "Send GET "+server.URL+"/ping") {
		t.Fatalf("asked %v", asked)
	}
	a.allow(true)
	text, failed := a.tool(t, "send_http_request", `{"file":"api/ping.http","request":"ping"}`)
	if failed || !strings.Contains(text, "200 OK") || !strings.Contains(text, `"path": "/ping"`) {
		t.Fatalf("approved: %q %v", text, failed)
	}
	if strings.Contains(text, "abcdef0123456789abcdef") || strings.Contains(text, "sid=") {
		t.Errorf("the answer shows a secret:\n%s", text)
	}
	history := a.http.History(file)
	if len(history) != 1 || history[0].Status != 200 || !strings.HasPrefix(history[0].ID, "agent-") {
		t.Fatalf("history = %+v", history)
	}
}

// run_tests runs a Go package's tests through Flint's test runner after the
// user's yes and answers with the results; the run is the project's last one.
func TestRunTestsTool(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	if testing.Short() {
		t.Skip("runs go test")
	}
	a := newActingApp(t, settings.MCPActions{Tests: true})
	a.write(t, "go.mod", "module example.test/m\n\ngo 1.21\n")
	a.write(t, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n\nfunc TestBad(t *testing.T) { t.Fatal(\"broken on purpose\") }\n")

	if text, failed := a.tool(t, "run_tests", `{"path":"calc"}`); !failed || text != mcpserver.ErrNotApproved.Error() {
		t.Fatalf("denied: %q", text)
	}
	if _, ok := a.mcpSources.Tests.Last(a.project); ok {
		t.Fatal("a denied call ran tests")
	}
	if asked := a.questions(); len(asked) != 1 || !strings.Contains(asked[0], "Run tests: go test ./calc") || !strings.Contains(asked[0], "go test -json ./calc") {
		t.Fatalf("asked %v", asked)
	}
	a.allow(true)
	text, failed := a.tool(t, "run_tests", `{"path":"calc"}`)
	if failed {
		t.Fatalf("run_tests: %s", text)
	}
	for _, want := range []string{"Test run: go test ./calc", "finished with exit code 1", "Tests: 1 passed, 1 failed", "TestBad", "broken on purpose"} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q:\n%s", want, text)
		}
	}
	if run, ok := a.mcpSources.Tests.Last(a.project); !ok || !run.Done || run.Title != "go test ./calc" {
		t.Fatalf("the Tests store has %+v", run)
	}
	if text, failed = a.tool(t, "run_tests", `{"path":"calc","tests":["TestNope"]}`); !failed || !strings.Contains(text, `no test "TestNope"`) {
		t.Fatalf("unknown test: %q", text)
	}
}
