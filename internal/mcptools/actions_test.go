package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/agentsession"
	"flint/internal/httpfile"
	"flint/internal/runconfig"
	"flint/internal/testrun"
)

var actionsOn = Config{RunTests: true, RunConfigs: true, HTTP: true, TerminalLines: 200, TestWait: 2 * time.Second}

// fakeActor records what the acting tools ask for.
type fakeActor struct {
	s       *Sources
	configs []runconfig.Config

	tests    []testrun.Request
	finish   bool // StartTests ends the run at once
	started  []runconfig.Config
	sent     []httpfile.Prepared
	sentFile string
	response *httpfile.Response
	startErr error
}

func (f *fakeActor) PlanTests(_ string, req testrun.Request) (testrun.Plan, error) {
	return testrun.Plan{Title: "planned " + filepath.Base(req.Dirs[0]), Cwd: req.Dirs[0], Steps: []runconfig.Command{runconfig.NewCommand("go", "test", "./...")}}, nil
}

func (f *fakeActor) StartTests(_ string, req testrun.Request, plan testrun.Plan) (string, <-chan struct{}, error) {
	if f.startErr != nil {
		return "", nil, f.startErr
	}
	f.tests = append(f.tests, req)
	f.s.Tests.Begin("run-1", plan.Title, plan.Cwd)
	f.s.Tests.Update("run-1", []testrun.Node{
		{ID: "p", Name: "example.com/p", Kind: testrun.KindPackage, Status: testrun.Failed},
		{ID: "p/TestOK", Parent: "p", Name: "TestOK", Test: "TestOK", Kind: testrun.KindTest, Status: testrun.Passed},
		{ID: "p/TestBad", Parent: "p", Name: "TestBad", Test: "TestBad", Kind: testrun.KindTest, Status: testrun.Failed, Output: "boom\npassword=hunter2hunter2"},
	})
	done := make(chan struct{})
	if f.finish {
		f.s.Tests.Finish("run-1", 1, false, 1500, "")
		close(done)
	}
	return "run-1", done, nil
}

func (f *fakeActor) RunConfigs(string) ([]runconfig.Config, error) { return f.configs, nil }

func (f *fakeActor) StartConfig(_ string, cfg runconfig.Config) (string, error) {
	if f.startErr != nil {
		return "", f.startErr
	}
	f.started = append(f.started, cfg)
	return "run-cfg", nil
}

func (f *fakeActor) SendHTTP(_ context.Context, _ string, file string, p httpfile.Prepared) (*httpfile.Response, error) {
	f.sent, f.sentFile = append(f.sent, p), file
	return f.response, nil
}

func newActing(t *testing.T) (*Sources, *fakeActor, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewSources()
	f := &fakeActor{s: s, finish: true}
	s.Actor = f
	return s, f, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// act plans a call of an acting tool and, when the plan is fine, runs it (as
// the server does after the user's yes).
func act(t *testing.T, s *Sources, name, root, args string) (summary, text string, err error) {
	t.Helper()
	tool := toolNamed(t, s, actionsOn, name)
	if !tool.Acts || tool.Plan == nil || tool.Call != nil {
		t.Fatalf("%s must act through a plan: %+v", name, tool)
	}
	planned, err := tool.Plan(context.Background(), root, json.RawMessage(args))
	if err != nil {
		return "", "", err
	}
	text, err = planned.Run(context.Background())
	return planned.Summary, text, err
}

// The acting tools are offered only when their group is on (and something
// can act), announce themselves as acting, and are never among the tools an
// agent may run without asking.
func TestActionToolsAreOptIn(t *testing.T) {
	s, _, _ := newActing(t)
	names := func(cfg Config) []string {
		var out []string
		for _, tool := range s.Tools(cfg) {
			out = append(out, tool.Name)
		}
		return out
	}
	if got := names(allOn); len(got) != len(Names) {
		t.Fatalf("with the defaults' groups off: %v", got)
	}
	for _, tc := range []struct {
		cfg  Config
		want []string
	}{
		{Config{RunTests: true}, []string{ToolRunTests}},
		{Config{RunConfigs: true}, []string{ToolRunConfig, ToolRunOutput}},
		{Config{HTTP: true}, []string{ToolHTTP}},
		{actionsOn, []string{ToolRunTests, ToolRunConfig, ToolRunOutput, ToolHTTP}},
	} {
		if got := names(tc.cfg); !slices.Equal(got, tc.want) {
			t.Errorf("%+v offers %v, want %v", tc.cfg, got, tc.want)
		}
	}
	for _, tool := range s.Tools(actionsOn) {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s: schema %s (%v)", tool.Name, tool.InputSchema, err)
		}
		if !slices.Contains(ActionNames, tool.Name) || slices.Contains(agentsession.MCPReadTools, tool.Name) {
			t.Errorf("%s: not an action name, or runs without asking", tool.Name)
		}
		if acts := tool.Name != ToolRunOutput; tool.Acts != acts {
			t.Errorf("%s: Acts = %v", tool.Name, tool.Acts)
		}
	}
	s.Actor = nil
	if got := names(actionsOn); len(got) != 0 {
		t.Fatalf("without an actor: %v", got)
	}
}

func TestRunTestsArguments(t *testing.T) {
	_, _, root := newActing(t)
	writeFile(t, filepath.Join(root, "pkg", "a_test.go"), "package pkg\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tt.Run(\"sub\", func(t *testing.T) {})\n}\n\nfunc TestB(t *testing.T) {}\n")
	writeFile(t, filepath.Join(root, "pkg", "a.go"), "package pkg\n")
	writeFile(t, filepath.Join(root, "empty", "x.go"), "package empty\n")
	writeFile(t, filepath.Join(root, "web", "sum.test.ts"), "import { test } from 'node:test'\n\ntest('adds', () => {})\ntest('adds more', () => {})\n")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "b_test.go"), "package out\n\nimport \"testing\"\n\nfunc TestOut(t *testing.T) {}\n")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "pkg")

	for _, tc := range []struct {
		args string
		want testrun.Request
	}{
		{`{"path":"pkg"}`, testrun.Request{Lang: "go", Dirs: []string{pkg}}},
		{`{"path":"pkg/a_test.go"}`, testrun.Request{Lang: "go", Dirs: []string{pkg}, File: filepath.Join(pkg, "a_test.go")}},
		{`{"path":"pkg/a_test.go","tests":["TestB","TestA/sub","TestB"]}`, testrun.Request{Lang: "go", Dirs: []string{pkg}, Tests: []string{"TestA/sub", "TestB"}}},
		{`{"path":"pkg","tests":["TestA/made at run time"]}`, testrun.Request{Lang: "go", Dirs: []string{pkg}, Tests: []string{"TestA/made at run time"}}},
		{`{"path":"` + filepath.Join(root, "web", "sum.test.ts") + `","tests":["adds"]}`,
			testrun.Request{Lang: "js", Dirs: []string{filepath.Join(root, "web")}, File: filepath.Join(root, "web", "sum.test.ts"), Tests: []string{"adds"}}},
	} {
		got, err := testRequest(root, json.RawMessage(tc.args))
		if err != nil {
			t.Errorf("%s: %v", tc.args, err)
			continue
		}
		if got.Lang != tc.want.Lang || !slices.Equal(got.Dirs, tc.want.Dirs) || got.File != tc.want.File || !slices.Equal(got.Tests, tc.want.Tests) {
			t.Errorf("%s = %+v, want %+v", tc.args, got, tc.want)
		}
	}

	many, _ := json.Marshal(slices.Repeat([]string{"TestA"}, maxTestNames+1))
	for _, tc := range []struct{ args, want string }{
		{`{}`, "path is required"},
		{`{"path":"pkg","run":"-exec=sh"}`, "invalid arguments"},
		{`{"path":"../other"}`, "outside the project"},
		{`{"path":"/etc"}`, "outside the project"},
		{`{"path":"link"}`, "leads outside the project"},
		{`{"path":"link/b_test.go"}`, "leads outside the project"},
		{`{"path":"missing"}`, "does not exist"},
		{`{"path":"empty"}`, "has no Go tests"},
		{`{"path":"pkg/a.go"}`, "is not a test file"},
		{`{"path":"pkg","tests":["TestNope"]}`, `no test "TestNope" in pkg (tests: TestA, TestB)`},
		{`{"path":"pkg","tests":["-run=x"]}`, `no test "-run=x"`},
		{`{"path":"pkg","tests":[""]}`, "a test name is required"},
		{`{"path":"pkg","tests":["Test\nA"]}`, "control character"},
		{`{"path":"pkg","tests":` + string(many) + `}`, "at most 20 test names"},
		{`{"path":"web/sum.test.ts","tests":["adds mo"]}`, `no test "adds mo" in web/sum.test.ts (tests: adds, adds more)`},
		{`{"path":"web"}`, "has no Go tests"},
	} {
		if _, err := testRequest(root, json.RawMessage(tc.args)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestRunTestsTool(t *testing.T) {
	s, f, root := newActing(t)
	writeFile(t, filepath.Join(root, "pkg", "a_test.go"), "package pkg\n\nimport \"testing\"\n\nfunc TestBad(t *testing.T) {}\n")

	summary, text, err := act(t, s, ToolRunTests, root, `{"path":"pkg","tests":["TestBad"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "Run tests: planned pkg") || !strings.Contains(summary, "In pkg:") || !strings.Contains(summary, "go test ./...") {
		t.Errorf("summary: %s", summary)
	}
	if len(f.tests) != 1 || !slices.Equal(f.tests[0].Tests, []string{"TestBad"}) {
		t.Fatalf("started %+v", f.tests)
	}
	for _, want := range []string{"Test run: planned pkg", "finished with exit code 1", "Tests: 1 passed, 1 failed", "- example.com/p TestBad", "boom"} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "still going") {
		t.Errorf("a finished run reported as going:\n%s", text)
	}

	// A run that outlasts the wait answers with the results so far.
	f.finish = false
	tool := toolNamed(t, s, Config{RunTests: true, TestWait: 20 * time.Millisecond}, ToolRunTests)
	planned, err := tool.Plan(context.Background(), root, json.RawMessage(`{"path":"pkg"}`))
	if err != nil {
		t.Fatal(err)
	}
	text, err = planned.Run(context.Background())
	if err != nil || !strings.Contains(text, "still running; results so far") || !strings.Contains(text, "still going after") || !strings.Contains(text, "run-1") {
		t.Fatalf("unfinished run: %v\n%s", err, text)
	}
	if tool.Timeout <= 20*time.Millisecond {
		t.Errorf("the call's timeout %s doesn't cover the wait", tool.Timeout)
	}

	f.startErr = errors.New("no go on PATH")
	if _, _, err := act(t, s, ToolRunTests, root, `{"path":"pkg"}`); err == nil || err.Error() != "no go on PATH" {
		t.Fatalf("start error: %v", err)
	}
	if _, _, err := act(t, s, ToolRunTests, root, `{"path":"nope"}`); err == nil {
		t.Fatal("a missing path was planned")
	}
}

func TestRunConfigurationTool(t *testing.T) {
	s, f, root := newActing(t)
	writeFile(t, filepath.Join(root, "web", "x"), "")
	f.configs = []runconfig.Config{
		{ID: "dev", Name: "Dev server", Type: runconfig.TypeCommand, Command: "npm run dev", Cwd: "web"},
		{ID: "twin-1", Name: "Twin", Type: runconfig.TypeCommand, Command: "true"},
		{ID: "twin-2", Name: "Twin", Type: runconfig.TypeCommand, Command: "false"},
		{ID: "gone", Name: "Gone", Type: runconfig.TypeCommand, Command: "true", Cwd: "missing"},
	}
	summary, text, err := act(t, s, ToolRunConfig, root, `{"name":"Dev server"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "Run configuration: Dev server") || !strings.Contains(summary, "In web:") || !strings.Contains(summary, "npm run dev") {
		t.Errorf("summary: %s", summary)
	}
	if len(f.started) != 1 || f.started[0].ID != "dev" || !strings.Contains(text, "run id run-cfg") || !strings.Contains(text, ToolRunOutput) {
		t.Fatalf("started %+v, answer %q", f.started, text)
	}
	if _, _, err := act(t, s, ToolRunConfig, root, `{"name":"twin-2"}`); err != nil || f.started[1].Command != "false" {
		t.Fatalf("by id: %v, %+v", err, f.started)
	}
	for _, tc := range []struct{ args, want string }{
		{`{}`, "name is required"},
		{`{"name":"Dev server","command":"rm -rf /"}`, "invalid arguments"},
		{`{"name":"npm run dev"}`, `no run configuration named "npm run dev" in this project (configurations: Dev server, Twin, Twin, Gone)`},
		{`{"name":"Twin"}`, "2 run configurations are named"},
		{`{"name":"Gone"}`, "does not exist"},
		{`{"name":"` + strings.Repeat("x", maxNameLen+1) + `"}`, "longer than"},
	} {
		if _, _, err := act(t, s, ToolRunConfig, root, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.args, err, tc.want)
		}
	}
	if len(f.started) != 2 {
		t.Fatalf("a refused call started something: %+v", f.started)
	}
}

func TestRunOutputTool(t *testing.T) {
	s, _, root := newActing(t)
	call := func(args string) (string, error) {
		return toolNamed(t, s, actionsOn, ToolRunOutput).Call(context.Background(), root, json.RawMessage(args))
	}
	if text, err := call(`{}`); err != nil || text != "No runs of this project in Flint's run tabs." {
		t.Fatalf("empty: %q %v", text, err)
	}
	s.Terminals.Open("term-1", KindTerminal, "zsh", root)
	s.Terminals.Write("term-1", []byte("typed by the user\n"))
	s.Terminals.Open("run-1", KindRun, "Dev server", root)
	s.Terminals.Write("run-1", []byte("listening on :3000\nAWS_SECRET_ACCESS_KEY=abcdefghijklmnopqrstuvwxyz0123456789abcd\n"))
	s.Terminals.Open("run-2", KindRun, "build", root)
	s.Terminals.Write("run-2", []byte("one\ntwo\nthree\n"))
	s.Terminals.Exit("run-2", 2)
	other := t.TempDir()
	s.Terminals.Open("run-other", KindRun, "elsewhere", other)
	s.Terminals.Write("run-other", []byte("not yours\n"))

	text, err := call(`{}`)
	if err != nil || !strings.Contains(text, "## run run-1: Dev server (in ., running)") || !strings.Contains(text, "## run run-2: build (in ., exited with code 2)") {
		t.Fatalf("list: %v\n%s", err, text)
	}
	if strings.Contains(text, "typed by the user") || strings.Contains(text, "not yours") {
		t.Fatalf("a terminal or another project's run is shown:\n%s", text)
	}
	if text, err = call(`{"run":"run-2","lines":2}`); err != nil || strings.Contains(text, "one") || !strings.Contains(text, "two\nthree") {
		t.Fatalf("one run: %v\n%s", err, text)
	}
	for _, id := range []string{"term-1", "run-other", "nope"} {
		if _, err := call(`{"run":"` + id + `"}`); err == nil || !strings.Contains(err.Error(), "no run") {
			t.Errorf("run %s: %v", id, err)
		}
	}
	if _, err := call(`{"lines":-1}`); err == nil {
		t.Error("negative lines accepted")
	}
}

const requests = `@host = https://api.example.test

### list users
GET {{host}}/users?page={{page}}
Accept: application/json
Authorization: Bearer {{token}}

### upload
POST {{host}}/upload

< ./payload.json

### leak
POST {{host}}/leak

< ../outside.txt

###
GET {{host}}/unnamed

### twin
GET {{host}}/1

### twin
GET {{host}}/2
`

func TestSendHTTPRequestTool(t *testing.T) {
	s, f, root := newActing(t)
	api := filepath.Join(root, "api")
	writeFile(t, filepath.Join(api, "users.http"), requests)
	writeFile(t, filepath.Join(api, "payload.json"), `{"a":1}`)
	writeFile(t, filepath.Join(api, "http-client.env.json"), `{"dev":{"page":"2","token":"tok-dev"}}`)
	writeFile(t, filepath.Join(api, "notes.txt"), "### list users\nGET http://x\n")
	writeFile(t, filepath.Join(filepath.Dir(root), "outside.txt"), "secret")
	t.Cleanup(func() { _ = os.Remove(filepath.Join(filepath.Dir(root), "outside.txt")) })
	// The body file of "leak" is reachable from the file, but outside the project.
	writeFile(t, filepath.Join(root, "top.http"), "### leak\nPOST http://x.test/\n\n< ../outside.txt\n")

	f.response = &httpfile.Response{
		ID: "r1", Method: "GET", URL: "https://api.example.test/users?page=2", FinalURL: "https://api.example.test/users?page=2",
		Status: 200, StatusText: "200 OK", Proto: "HTTP/2.0", TimeMs: 12.4, Size: 40000, ContentType: "application/json", Kind: httpfile.KindJSON,
		Headers: []httpfile.Header{
			{Name: "Content-Type", Value: "application/json"},
			{Name: "Set-Cookie", Value: "session=abcdef0123456789; HttpOnly"},
			{Name: "X-Long", Value: strings.Repeat("v", 1000)},
		},
		Body: strings.Repeat("0123456789", 4000),
	}
	summary, text, err := act(t, s, ToolHTTP, root, `{"file":"api/users.http","request":"list users","environment":"dev"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Send GET https://api.example.test/users?page=2", `Request "list users" of api/users.http, environment dev`, "Headers: Accept, Authorization"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "tok-dev") {
		t.Errorf("the summary shows a header value:\n%s", summary)
	}
	if len(f.sent) != 1 || f.sent[0].URL != "https://api.example.test/users?page=2" || f.sentFile != filepath.Join(api, "users.http") {
		t.Fatalf("sent %+v of %s", f.sent, f.sentFile)
	}
	if !slices.ContainsFunc(f.sent[0].Headers, func(h httpfile.Header) bool { return h.Name == "Authorization" && h.Value == "Bearer tok-dev" }) {
		t.Errorf("environment not applied: %+v", f.sent[0].Headers)
	}
	for _, want := range []string{"HTTP/2.0 200 OK (GET https://api.example.test/users?page=2, 12 ms, 40000 bytes, application/json)", "Set-Cookie: (hidden)", "Body (the first 16 KiB"} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q", want)
		}
	}
	if strings.Contains(text, "abcdef0123456789") || strings.Contains(text, strings.Repeat("v", maxHeaderValue+1)) || len(text) > maxHTTPBody+2000 {
		t.Errorf("answer not bounded or shows a cookie: %d bytes", len(text))
	}

	// A body file inside the project is sent; the summary says where it is from.
	summary, _, err = act(t, s, ToolHTTP, root, `{"file":"api/users.http","request":"upload"}`)
	if err != nil || !strings.Contains(summary, "Body: 7 bytes from api/payload.json") || string(f.sent[1].Body) != `{"a":1}` {
		t.Fatalf("upload: %v\n%s", err, summary)
	}

	for _, tc := range []struct{ args, want string }{
		{`{"request":"x"}`, "file is required"},
		{`{"file":"api/users.http"}`, "request is required"},
		{`{"file":"api/users.http","request":"list users","url":"http://evil.test"}`, "invalid arguments"},
		{`{"file":"api/users.http","request":"nope"}`, `no request named "nope" in api/users.http (named requests: list users, upload, leak, twin, twin)`},
		{`{"file":"api/users.http","request":"GET {{host}}/unnamed"}`, "no request named"},
		{`{"file":"api/users.http","request":"twin"}`, "2 requests are named"},
		{`{"file":"api/users.http","request":"list users"}`, `undefined variable "page"`},
		{`{"file":"api/users.http","request":"list users","environment":"prod"}`, `no environment "prod" next to api/users.http (environments: dev)`},
		{`{"file":"api/notes.txt","request":"list users"}`, "is not a .http or .rest file"},
		{`{"file":"api/missing.http","request":"x"}`, "does not exist"},
		{`{"file":"../outside.http","request":"x"}`, "outside the project"},
		{`{"file":"top.http","request":"leak"}`, "is outside the project"},
	} {
		if _, _, err := act(t, s, ToolHTTP, root, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.args, err, tc.want)
		}
	}
	if len(f.sent) != 2 {
		t.Fatalf("a refused call sent a request: %d sent", len(f.sent))
	}
}

func TestDescribeResponseWithoutText(t *testing.T) {
	empty := describeResponse(&httpfile.Response{Method: "DELETE", URL: "http://x.test/1", StatusText: "204 No Content", Proto: "HTTP/1.1", Kind: httpfile.KindEmpty})
	if !strings.HasSuffix(empty, "No body.") {
		t.Errorf("empty: %s", empty)
	}
	binary := describeResponse(&httpfile.Response{Method: "GET", URL: "http://x.test/a", FinalURL: "http://x.test/b", StatusText: "200 OK", Proto: "HTTP/1.1", Kind: httpfile.KindImage, Size: 9})
	if !strings.Contains(binary, "Final URL after redirects: http://x.test/b") || !strings.Contains(binary, "image data, not shown") {
		t.Errorf("binary: %s", binary)
	}
}
