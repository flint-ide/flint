package host

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"flint/internal/extensions/rpc"
)

// The host's tests drive testdata/testext, a Go program built once per run.

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

// testextSource returns a folder with the test extension, built; the test is
// skipped when there is no go command to build it.
func testextSource(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "flint-testext-")
		if buildErr != nil {
			return
		}
		out, err := exec.Command("go", "build", "-o", filepath.Join(buildDir, "testext"), "./testdata/testext").CombinedOutput()
		if err != nil {
			buildErr = errors.New(string(out))
			return
		}
		data, err := os.ReadFile("testdata/testext/flint-extension.json")
		if err == nil {
			err = os.WriteFile(filepath.Join(buildDir, "flint-extension.json"), data, 0o644)
		}
		buildErr = err
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildDir
}

func TestMain(m *testing.M) {
	backoffBase = 20 * time.Millisecond
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// fakeApp is App in memory.
type fakeApp struct {
	mu       sync.Mutex
	roots    []string
	trusted  map[string]bool
	events   []string
	opened   []string
	asked    []string
	confirm  bool
	disabled map[string]bool
	settings map[string]json.RawMessage
}

func (a *fakeApp) Roots() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.roots...)
}

func (a *fakeApp) Trusted(p string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.trusted[p]
}

func (a *fakeApp) Emit(event string, data ...any) {
	b, _ := json.Marshal(data)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event+" "+string(b))
}

func (a *fakeApp) OpenURL(link string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.opened = append(a.opened, link)
	return nil
}

func (a *fakeApp) Confirm(title, message, _ string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, title+"\n"+message)
	return a.confirm
}

func (a *fakeApp) Choose(string, string, []string) string { return "" }

func (a *fakeApp) Enabled(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.disabled[id]
}

func (a *fakeApp) Settings(id string) json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings[id]
}

func (a *fakeApp) Version() string { return "v9.9.9" }

// eventsLike returns the events starting with prefix.
func (a *fakeApp) eventsLike(prefix string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, e := range a.events {
		if strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Get(service, account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[service+"/"+account]
	if !ok {
		return "", fs.ErrNotExist
	}
	return v, nil
}

func (s *memSecrets) Set(service, account, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[service+"/"+account] = value
	return nil
}

func (s *memSecrets) Delete(service, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, service+"/"+account)
	return nil
}

type fixture struct {
	h       *Host
	app     *fakeApp
	secrets *memSecrets
	root    string // trusted
	other   string // open, untrusted
}

func setup(t *testing.T, change func(o *Options)) fixture {
	t.Helper()
	src := testextSource(t)
	root, other := realDir(t), realDir(t)
	app := &fakeApp{
		roots: []string{root, other}, trusted: map[string]bool{root: true}, confirm: true,
		disabled: map[string]bool{}, settings: map[string]json.RawMessage{"test.ext": json.RawMessage(`{"count": 42}`)},
	}
	secrets := &memSecrets{m: map[string]string{}}
	o := Options{
		Dir: filepath.Join(t.TempDir(), "extensions"), App: app, Secrets: secrets, Environ: MinimalEnv,
		CallTimeout: 3 * time.Second, StopTimeout: time.Second,
	}
	if change != nil {
		change(&o)
	}
	h := New(o)
	t.Cleanup(h.Close)
	if _, err := h.Install(src); err != nil {
		t.Fatal(err)
	}
	return fixture{h: h, app: app, secrets: secrets, root: root, other: other}
}

func realDir(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func (f fixture) exec(t *testing.T, command string, args any) error {
	t.Helper()
	raw, _ := json.Marshal(args)
	return f.h.Execute(context.Background(), "test.ext", "test.ext."+command, raw)
}

func (f fixture) log(t *testing.T) string {
	t.Helper()
	s, err := f.h.Log("test.ext")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// waitFor polls cond for up to 5 s (tests only).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f fixture) state() string { return f.h.List()[0].State }

func TestInstallIsLazyAndConfirmed(t *testing.T) {
	f := setup(t, nil)
	list := f.h.List()
	if len(list) != 1 || list[0].ID != "test.ext" || list[0].State != StateStopped {
		t.Fatalf("List = %+v", list)
	}
	asked := f.app.asked[0]
	for _, want := range []string{"Test Extension 1.0.0 (test.ext)", "Program: testext", "workspace.read", "secrets", "Agent tools: lookup", "your user's permissions"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, asked)
		}
	}
	if f.log(t) != "" {
		t.Errorf("installing started the process: %s", f.log(t))
	}
	// Installing again asks to replace; saying no changes nothing.
	f.app.confirm = false
	if _, err := f.h.Install(testextSource(t)); !errors.Is(err, ErrCancelled) {
		t.Errorf("cancelled install = %v", err)
	}
	if !strings.HasPrefix(f.app.asked[1], "Replace Test Extension 1.0.0 with 1.0.0?") {
		t.Errorf("second confirmation: %s", f.app.asked[1])
	}
	if err := f.h.Uninstall("test.ext"); !errors.Is(err, ErrCancelled) {
		t.Errorf("cancelled uninstall = %v", err)
	}
	f.app.confirm = true
	if err := f.h.Uninstall("test.ext"); err != nil {
		t.Fatal(err)
	}
	if len(f.h.List()) != 0 {
		t.Errorf("still listed after uninstall: %+v", f.h.List())
	}
}

func TestResolveStartsAndCleansTheView(t *testing.T) {
	f := setup(t, nil)
	v, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.state() != StateRunning {
		t.Errorf("state = %s", f.state())
	}
	if len(v.Items) != 2 {
		t.Fatalf("items = %+v (the duplicate id is dropped)", v.Items)
	}
	a, b := v.Items[0], v.Items[1]
	if a.Label != "Alpha[31m" || a.Icon != "files" || !a.HasChildren || a.Command == nil || a.Command.ID != "test.ext.status" {
		t.Errorf("first item = %+v", a)
	}
	if b.Icon != "" || b.Command != nil {
		t.Errorf("an unknown icon and a foreign command are dropped: %+v", b)
	}
	child, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", "a")
	if err != nil || len(child.Items) != 1 || child.Items[0].ID != "a/child" {
		t.Errorf("children = %+v, %v", child, err)
	}
	log := f.log(t)
	// initialize: trusted roots only, settings with defaults filled in and
	// invalid values replaced.
	if !strings.Contains(log, `"roots":[{"path":"`+f.root+`","trusted":true}]`) || strings.Contains(log, f.other) {
		t.Errorf("initialize roots:\n%s", log)
	}
	if !strings.Contains(log, `"settings":{"count":3,"greeting":"hi"}`) {
		t.Errorf("initialize settings:\n%s", log)
	}
	if _, err := f.h.Resolve(context.Background(), "test.ext", "git.panel", ""); err == nil {
		t.Error("a panel the extension doesn't have resolves")
	}
}

func TestStatusRefreshAndMessage(t *testing.T) {
	f := setup(t, nil)
	if err := f.exec(t, "status", map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a message", func() bool { return len(f.app.eventsLike(EventMessage)) > 0 })
	status := f.app.eventsLike(EventStatus)
	if len(status) != 1 || !strings.Contains(status[0], `"text":"`+strings.Repeat("x", 39)+`…"`) || strings.Contains(status[0], "bogus") {
		t.Errorf("status = %v", status)
	}
	if got := f.h.List()[0].StatusItems[0]; got.Command != "test.ext.status" || len([]rune(got.Text)) != 40 {
		t.Errorf("List keeps the last status: %+v", got)
	}
	if r := f.app.eventsLike(EventRefresh); len(r) != 1 || !strings.Contains(r[0], `"view":"test.ext.items"`) {
		t.Errorf("refresh = %v", r)
	}
	if m := f.app.eventsLike(EventMessage)[0]; !strings.Contains(m, `"type":"warning"`) || !strings.Contains(m, `hello {\"n\":1}`) {
		t.Errorf("message = %s", m)
	}
	if err := f.h.Execute(context.Background(), "test.ext", "git.push", nil); err == nil {
		t.Error("a command the extension doesn't have runs")
	}
}

func TestHostMethodsAndPermissions(t *testing.T) {
	f := setup(t, nil)
	inside := filepath.Join(f.root, "notes.txt")
	outside := filepath.Join(f.other, "secret.txt")
	env := filepath.Join(f.root, ".env")
	for _, p := range []string{inside, outside, env} {
		if err := os.WriteFile(p, []byte("text of "+filepath.Base(p)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	results := func() []string {
		var out []string
		for _, line := range strings.Split(f.log(t), "\n") {
			if _, r, ok := strings.Cut(line, "[result] "); ok {
				out = append(out, r)
			}
		}
		return out
	}
	steps := []struct {
		command string
		args    any
		want    string
	}{
		{"read", map[string]string{"path": inside}, `{"text":"text of notes.txt"}`},
		{"read", map[string]string{"path": outside}, `"code":-32001`},
		{"read", map[string]string{"path": env}, `may hold credentials`},
		{"open", map[string]string{"path": inside}, `null`},
		{"secret", map[string]string{"key": "token", "value": "s3cret"}, `{"value":"s3cret"}`},
		{"ask", map[string]string{"path": inside}, `needs the \"agent.ask\" permission`},
		{"unknown", nil, `"code":-32601`},
		{"url", map[string]string{"url": "file:///etc/passwd"}, `only http and https`},
		{"url", map[string]string{"url": "https://example.com/a"}, `null`},
	}
	for i, s := range steps {
		if err := f.exec(t, s.command, s.args); err != nil {
			t.Fatal(err)
		}
		waitFor(t, s.command+" result", func() bool { return len(results()) > i })
		if got := results()[i]; !strings.Contains(got, s.want) {
			t.Errorf("%s %v = %s, want %s", s.command, s.args, got, s.want)
		}
	}
	if _, ok := f.secrets.m["flint-extensions/test.ext:token"]; !ok {
		t.Errorf("secrets are namespaced: %v", f.secrets.m)
	}
	if open := f.app.eventsLike(EventOpenFile); len(open) != 1 || !strings.Contains(open[0], `"line":3`) {
		t.Errorf("open file events = %v", open)
	}
	if len(f.app.opened) != 1 || f.app.opened[0] != "https://example.com/a" {
		t.Errorf("opened = %v", f.app.opened)
	}
}

func TestEnvironmentIsMinimal(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "do-not-pass")
	f := setup(t, nil)
	if _, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", ""); err != nil {
		t.Fatal(err)
	}
	log := f.log(t)
	if strings.Contains(log, "do-not-pass") || !strings.Contains(log, "FLINT_EXTENSION_ID=test.ext") {
		t.Errorf("environment:\n%s", log)
	}
}

func TestIdleStopAndRestart(t *testing.T) {
	f := setup(t, func(o *Options) { o.IdleTimeout = 100 * time.Millisecond })
	if err := f.exec(t, "status", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the idle stop", func() bool { return f.state() == StateStopped })
	if !strings.Contains(f.log(t), "stopping: idle") {
		t.Errorf("log:\n%s", f.log(t))
	}
	// A visible panel keeps it running.
	if err := f.h.SetVisible("test.ext", "test.ext.items", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if f.state() != StateRunning {
		t.Errorf("stopped while its panel is visible: %s", f.state())
	}
	_ = f.h.SetVisible("test.ext", "test.ext.items", false)
	waitFor(t, "the idle stop after hiding", func() bool { return f.state() == StateStopped })
}

func TestCrashesAndGiveUp(t *testing.T) {
	f := setup(t, nil)
	for i := 1; i <= maxCrashes; i++ {
		if err := f.exec(t, "crash", nil); err == nil {
			t.Fatal("a crashing command succeeds")
		}
		want := StateCrashed
		if i == maxCrashes {
			want = StateFailed
		}
		waitFor(t, want, func() bool { return f.state() == want })
	}
	if err := f.exec(t, "status", nil); err == nil || !strings.Contains(err.Error(), "stopped after crashing") {
		t.Errorf("a failed extension runs: %v", err)
	}
	if m := f.app.eventsLike(EventMessage); len(m) != 1 || !strings.Contains(m[0], "stopped after crashing 3 times") {
		t.Errorf("messages = %v", m)
	}
	if err := f.h.Restart("test.ext"); err != nil {
		t.Fatal(err)
	}
	if err := f.exec(t, "status", nil); err != nil {
		t.Errorf("after Restart: %v", err)
	}
}

func TestProtocolErrorStopsTheProcess(t *testing.T) {
	f := setup(t, nil)
	if err := f.exec(t, "garbage", nil); err == nil {
		t.Fatal("garbage on stdout is accepted")
	}
	waitFor(t, "the crash", func() bool { return f.state() == StateCrashed })
	if log := f.log(t); !strings.Contains(log, "protocol error: not a JSON-RPC message") {
		t.Errorf("log:\n%s", log)
	}
}

func TestTimeout(t *testing.T) {
	f := setup(t, func(o *Options) { o.CallTimeout = 200 * time.Millisecond })
	err := f.exec(t, "hang", nil)
	if err == nil || !strings.Contains(err.Error(), "not responding") {
		t.Errorf("err = %v", err)
	}
	if f.state() != StateRunning {
		t.Errorf("a slow request stopped the process: %s", f.state())
	}
}

func TestCloseKillsTheProcessGroup(t *testing.T) {
	f := setup(t, nil)
	if err := f.exec(t, "child", nil); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitFor(t, "the child's pid", func() bool {
		_, after, ok := strings.Cut(f.log(t), "[result] ")
		if ok {
			pid, _ = strconv.Atoi(strings.TrimSpace(strings.SplitN(after, "\n", 2)[0]))
		}
		return pid > 0
	})
	f.h.Close()
	waitFor(t, "the grandchild to be gone", func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) })
	if err := f.exec(t, "status", nil); err == nil {
		t.Error("runs after Close")
	}
}

func TestAgentTools(t *testing.T) {
	f := setup(t, nil)
	tools := f.h.Tools(func(string) bool { return true })
	if len(tools) != 1 || tools[0].Name != "ext_lookup" {
		t.Fatalf("tools = %+v", tools)
	}
	text, err := tools[0].Call(context.Background(), f.root, json.RawMessage(`{"q":"x"}`))
	if err != nil || text != "lookup in "+f.root+" with x" {
		t.Errorf("call = %q, %v", text, err)
	}
	if _, err := tools[0].Call(context.Background(), f.other, nil); err == nil {
		t.Error("a tool runs for an untrusted folder")
	}
	if got := f.h.Tools(func(string) bool { return false }); len(got) != 0 {
		t.Errorf("tools turned off: %+v", got)
	}
	f.app.disabled["test.ext"] = true
	if got := f.h.Tools(func(string) bool { return true }); len(got) != 0 {
		t.Errorf("tools of a disabled extension: %+v", got)
	}
}

func TestToolNames(t *testing.T) {
	taken := map[string]bool{}
	a := uniqueName(toolPrefix("acme.jira-cloud")+"_issue", taken)
	taken[a] = true
	b := uniqueName(a, taken)
	long := uniqueName(strings.Repeat("x", 80), taken)
	if a != "jira_cloud_issue" || b != "jira_cloud_issue_2" || len(long) != maxToolName {
		t.Errorf("names = %q %q %q", a, b, long)
	}
}

func TestSettingsAndDisable(t *testing.T) {
	f := setup(t, nil)
	if _, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", ""); err != nil {
		t.Fatal(err)
	}
	f.app.settings["test.ext"] = json.RawMessage(`{"greeting":"hello","count":5}`)
	f.h.SettingsChanged("test.ext")
	waitFor(t, "settings/changed", func() bool {
		return strings.Contains(f.log(t), `settings/changed {"settings":{"count":5,"greeting":"hello"}}`)
	})
	f.app.disabled["test.ext"] = true
	f.h.SettingsChanged("test.ext")
	if f.state() != StateDisabled {
		t.Errorf("state = %s", f.state())
	}
	if _, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", ""); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Errorf("a disabled extension resolves: %v", err)
	}
}

func TestContractMismatch(t *testing.T) {
	f := setup(t, func(o *Options) {
		o.Environ = func() []string { return append(MinimalEnv(), "TESTEXT_CONTRACT=99") }
	})
	err := f.exec(t, "status", nil)
	if err == nil || !strings.Contains(err.Error(), "contract 99") {
		t.Errorf("err = %v", err)
	}
}

func TestInvalidFolders(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "broken.ext"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := New(Options{Dir: dir, App: &fakeApp{disabled: map[string]bool{}}, Environ: MinimalEnv})
	defer h.Close()
	list := h.List()
	if len(list) != 1 || list[0].State != StateInvalid || !strings.Contains(list[0].Error, "no flint-extension.json") {
		t.Errorf("List = %+v", list)
	}
	var rpcErr *rpc.Error
	if err := h.Execute(context.Background(), "broken.ext", "broken.ext.x", nil); err == nil || errors.As(err, &rpcErr) {
		t.Errorf("a broken extension runs: %v", err)
	}
}
