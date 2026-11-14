package host

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"flint/internal/extensions/manifest"
	"flint/internal/extensions/sandbox"
)

// The sandbox, through the host: the test extension with a run.sandbox
// section, started by the launcher (this test binary, see TestMain). Tests
// that need a real sandbox skip where the system has none.

func needSandbox(t *testing.T) sandbox.Support {
	t.Helper()
	s := sandbox.Probe()
	if !s.Available {
		t.Skipf("no sandbox on this system: %s", s.Reason)
	}
	return s
}

// sandboxedSource is the test extension's folder with "sandbox": section in
// its manifest's run.
func sandboxedSource(t *testing.T, section string) string {
	t.Helper()
	src := testextSource(t)
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join(src, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	const run = `"run": { "command": "testext" }`
	if !strings.Contains(string(data), run) {
		t.Fatal("the test extension's manifest changed")
	}
	data = []byte(strings.Replace(string(data), run, `"run": { "command": "testext", "sandbox": `+section+` }`, 1))
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(src, "testext"), filepath.Join(dir, "testext")); err != nil {
		bin, err := os.ReadFile(filepath.Join(src, "testext"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "testext"), bin, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// setupSandboxed is setup with the sandboxed manifest.
func setupSandboxed(t *testing.T, section string, change func(o *Options)) fixture {
	t.Helper()
	src := sandboxedSource(t, section)
	root, other := realDir(t), realDir(t)
	app := &fakeApp{
		roots: []string{root, other}, trusted: map[string]bool{root: true}, confirm: true,
		disabled: map[string]bool{}, settings: map[string]json.RawMessage{},
	}
	o := Options{
		Dir: filepath.Join(realDir(t), "extensions"), App: app, Secrets: &memSecrets{m: map[string]string{}}, Environ: MinimalEnv,
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
	return fixture{h: h, app: app, root: root, other: other}
}

// results are the "[result] …" lines of the log, in order.
func (f fixture) results(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(f.log(t), "\n") {
		if _, after, ok := strings.Cut(line, "[result] "); ok {
			out = append(out, after)
		}
	}
	return out
}

// fs asks the extension to read or write path itself and returns what it
// logged: "<op> <path> -> <content or error: …>".
func (f fixture) fs(t *testing.T, op, path string) string {
	t.Helper()
	n := len(f.results(t))
	if err := f.exec(t, "fs", map[string]string{"path": path, "value": op}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the result of "+op+" "+path, func() bool { return len(f.results(t)) > n })
	return f.results(t)[n]
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSandboxedExtension(t *testing.T) {
	s := needSandbox(t)
	f := setupSandboxed(t, `{}`, nil)
	asked := f.app.asked[0]
	for _, want := range []string{"Sandbox: Sandboxed (" + s.Mechanism + ")", "• Reads: its own folder, system files, trusted project folders", "• Writes: its data folder, a private temporary folder", "• Network: blocked", "• Other programs: only its own", "runs inside this sandbox"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, asked)
		}
	}
	if strings.Contains(asked, "cannot confine") {
		t.Errorf("the confirmation of a sandboxed extension says it is unconfined:\n%s", asked)
	}

	// JSON-RPC over stdio works through the launcher.
	v, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", "")
	if err != nil || len(v.Items) != 2 {
		t.Fatalf("resolve = %+v, %v", v, err)
	}
	in := f.h.List()[0]
	if in.State != StateRunning || in.Sandbox.State != SandboxOn || in.Sandbox.Refused || !strings.HasPrefix(in.Sandbox.Summary, "Sandboxed (") {
		t.Errorf("info = %s, %+v", in.State, in.Sandbox)
	}
	log := f.log(t)
	for _, want := range []string{"[flint] sandboxed (", `"sandbox":{"active":true,"exec":false,"localSockets":false,"network":false}`, "FLINT_EXTENSION_SANDBOX=1", "FLINT_EXTENSION_DATA="} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "FLINT_SANDBOX_LAUNCH") {
		t.Errorf("the launcher's variable reached the program:\n%s", log)
	}
	tmp := regexp.MustCompile(`TMPDIR=(\S*flint-ext-\S+)`).FindStringSubmatch(log)
	if tmp == nil {
		t.Fatalf("no private TMPDIR:\n%s", log)
	}

	// What it can and can't touch directly.
	config := filepath.Dir(f.h.o.Dir)
	ext := filepath.Join(f.h.o.Dir, "test.ext")
	data := filepath.Join(config, "extension-data", "test.ext")
	neighbour := filepath.Join(f.h.o.Dir, "other.ext")
	if err := os.Mkdir(neighbour, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := writeFile(t, filepath.Join(realDir(t), "secret"), "secret")
	// A refusal is "permission denied" on Linux and "operation not
	// permitted" on macOS: any error counts.
	const denied = "-> error: "
	for _, tt := range []struct{ op, path, want string }{
		{"read", filepath.Join(ext, manifest.FileName), `"id": "test.ext"`},
		{"read", writeFile(t, filepath.Join(f.root, "in-project"), "project file"), "-> project file"},
		{"read", writeFile(t, filepath.Join(f.other, "untrusted"), "x"), denied},
		{"read", secret, denied},
		{"read", writeFile(t, filepath.Join(config, "settings.json"), "{}"), denied},
		{"read", writeFile(t, filepath.Join(neighbour, "file"), "x"), denied},
		{"write", filepath.Join(data, "state"), "-> "},
		{"read", filepath.Join(data, "state"), "-> written by testext"},
		{"write", filepath.Join(tmp[1], "scratch"), "-> "},
		{"write", filepath.Join(f.root, "new"), denied},
		{"write", filepath.Join(f.root, "in-project"), denied},
		{"write", filepath.Join(ext, "new"), denied},
		{"write", secret, denied},
		{"write", filepath.Join(os.TempDir(), "flint-sandbox-test-escape"), denied},
	} {
		got := f.fs(t, tt.op, tt.path)
		if !strings.Contains(got, tt.want) || (tt.want != denied && strings.Contains(got, "error:")) {
			t.Errorf("%s %s: %q, want %q", tt.op, tt.path, got, tt.want)
		}
	}
	if got, _ := os.ReadFile(secret); string(got) != "secret" {
		t.Errorf("the secret changed: %q", got)
	}

	// The network: blocked where this kernel can, reported where it can't.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	defer l.Close()
	n := len(f.results(t))
	if err := f.exec(t, "dial", map[string]string{"url": l.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the dial", func() bool { return len(f.results(t)) > n })
	got := f.results(t)[n]
	blocked := !strings.Contains(strings.Join(s.Gaps, " "), "are not blocked")
	if blocked != strings.Contains(got, denied) {
		t.Errorf("dial: %q; the system reports %v", got, s.Gaps)
	}
	if !blocked {
		if d := strings.Join(in.Sandbox.Details, "\n"); !strings.Contains(d, "are not blocked") {
			t.Errorf("the degraded sandbox isn't disclosed:\n%s", d)
		}
	}

	// Its temporary folder goes with it.
	f.h.Close()
	if _, err := os.Stat(tmp[1]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temporary folder stays: %v", err)
	}
}

func TestSandboxDeclaredAccess(t *testing.T) {
	needSandbox(t)
	shared := realDir(t)
	writeFile(t, filepath.Join(shared, "doc"), "shared doc")
	out := realDir(t)
	section, _ := json.Marshal(map[string]any{"read": []string{shared, "~/.config", "/nothing/here"}, "write": []string{out}, "network": true})
	f := setupSandboxed(t, string(section), nil)
	if got := f.fs(t, "read", filepath.Join(shared, "doc")); !strings.Contains(got, "-> shared doc") {
		t.Errorf("a declared read path: %q", got)
	}
	if got := f.fs(t, "write", filepath.Join(shared, "doc")); !strings.Contains(got, "-> error: ") {
		t.Errorf("a read path was written: %q", got)
	}
	if got := f.fs(t, "write", filepath.Join(out, "result")); strings.Contains(got, "error:") {
		t.Errorf("a declared write path: %q", got)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	defer l.Close()
	n := len(f.results(t))
	if err := f.exec(t, "dial", map[string]string{"url": l.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the dial", func() bool { return len(f.results(t)) > n })
	if got := f.results(t)[n]; !strings.Contains(got, "-> connected") {
		t.Errorf("dial with the network declared: %q", got)
	}
	// What was asked for but not granted is said, in Settings and in the log.
	details := strings.Join(f.h.List()[0].Sandbox.Details, "\n")
	for _, want := range []string{"Network: allowed, to any host", "/nothing/here is not granted: it doesn't exist"} {
		if !strings.Contains(details, want) {
			t.Errorf("details lack %q:\n%s", want, details)
		}
	}
	if !strings.Contains(f.log(t), "sandbox: /nothing/here is not granted") {
		t.Errorf("log:\n%s", f.log(t))
	}
}

// Idle stop, crashes and the process-group kill behave as they do without a
// sandbox (TestIdleStopAndRestart, TestCrashesAndGiveUp,
// TestCloseKillsTheProcessGroup).
func TestSandboxedLifecycle(t *testing.T) {
	needSandbox(t)
	t.Run("idle stop and restart", func(t *testing.T) {
		f := setupSandboxed(t, `{}`, func(o *Options) { o.IdleTimeout = 100 * time.Millisecond })
		if err := f.exec(t, "status", nil); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the idle stop", func() bool { return f.state() == StateStopped })
		if !strings.Contains(f.log(t), "stopping: idle") {
			t.Errorf("log:\n%s", f.log(t))
		}
		if err := f.exec(t, "status", nil); err != nil {
			t.Errorf("the next use doesn't start it again: %v", err)
		}
		if n := strings.Count(f.log(t), "[flint] sandboxed ("); n != 2 {
			t.Errorf("sandboxed %d times, want 2:\n%s", n, f.log(t))
		}
	})
	t.Run("crashes and give up", func(t *testing.T) {
		f := setupSandboxed(t, `{}`, nil)
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
		if !strings.Contains(f.log(t), "exited: exit status 3") {
			t.Errorf("the program's exit status is lost behind the launcher:\n%s", f.log(t))
		}
		if err := f.h.Restart("test.ext"); err != nil {
			t.Fatal(err)
		}
		if err := f.exec(t, "status", nil); err != nil {
			t.Errorf("after Restart: %v", err)
		}
	})
	t.Run("the process group is killed", func(t *testing.T) {
		f := setupSandboxed(t, `{"exec": true}`, nil)
		if err := f.exec(t, "child", nil); err != nil {
			t.Fatal(err)
		}
		var pid int
		waitFor(t, "the child's pid", func() bool {
			if r := f.results(t); len(r) > 0 {
				pid, _ = strconv.Atoi(strings.TrimSpace(r[0]))
			}
			return pid > 0
		})
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatalf("the grandchild isn't running: %v", err)
		}
		f.h.Close()
		waitFor(t, "the grandchild to be gone", func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) })
	})
	t.Run("a hanging process is killed", func(t *testing.T) {
		f := setupSandboxed(t, `{}`, func(o *Options) { o.CallTimeout = 200 * time.Millisecond; o.StopTimeout = 200 * time.Millisecond })
		if err := f.exec(t, "hang", nil); err == nil {
			t.Fatal("a hanging command returns")
		}
		if f.state() != StateRunning {
			t.Fatalf("state = %s", f.state())
		}
		f.h.Close()
		if f.state() == StateRunning {
			t.Error("still running after Close")
		}
	})
}

// A sandbox can't grow: when the trusted folders change, a confined
// extension that reads them is restarted instead of notified.
func TestSandboxFollowsTrustedRoots(t *testing.T) {
	needSandbox(t)
	f := setupSandboxed(t, `{}`, nil)
	later := realDir(t)
	file := writeFile(t, filepath.Join(later, "file"), "added later")
	if got := f.fs(t, "read", file); !strings.Contains(got, "-> error: ") {
		t.Fatalf("a folder that isn't open is readable: %q", got)
	}
	if err := f.h.SetVisible("test.ext", "test.ext.items", true); err != nil {
		t.Fatal(err)
	}
	f.app.mu.Lock()
	f.app.roots = append(f.app.roots, later)
	f.app.trusted[later] = true
	f.app.mu.Unlock()
	f.h.RootsChanged()
	waitFor(t, "the restart", func() bool {
		return strings.Count(f.log(t), "[flint] sandboxed (") == 2 && f.state() == StateRunning
	})
	if !strings.Contains(f.log(t), "stopping: the project folders changed") || strings.Contains(f.log(t), "workspace/rootsChanged") {
		t.Errorf("log:\n%s", f.log(t))
	}
	if got := f.fs(t, "read", file); !strings.Contains(got, "-> added later") {
		t.Errorf("after the restart: %q", got)
	}
	// Nothing changed: no restart.
	f.h.RootsChanged()
	time.Sleep(100 * time.Millisecond)
	if n := strings.Count(f.log(t), "[flint] sandboxed ("); n != 2 {
		t.Errorf("restarted %d times without a change", n-1)
	}
}

func TestUnsandboxedStatesAreDisclosed(t *testing.T) {
	// No run.sandbox: runs as before, and says so.
	f := setup(t, nil)
	if !strings.Contains(f.app.asked[0], "Sandbox: Not sandboxed: the extension doesn't declare a sandbox (run.sandbox)") || !strings.Contains(f.app.asked[0], "cannot confine") {
		t.Errorf("confirmation:\n%s", f.app.asked[0])
	}
	if err := f.exec(t, "status", nil); err != nil {
		t.Fatal(err)
	}
	in := f.h.List()[0].Sandbox
	if in.State != SandboxUndeclared || in.Refused || !strings.Contains(f.log(t), "[flint] not sandboxed: the extension doesn't declare a sandbox") {
		t.Errorf("info = %+v\n%s", in, f.log(t))
	}
	if strings.Contains(f.log(t), "FLINT_EXTENSION_SANDBOX") || !strings.Contains(f.log(t), `"sandbox":{"active":false}`) {
		t.Errorf("an unconfined process is told it is sandboxed:\n%s", f.log(t))
	}

	// "mode": "none": declared by the extension.
	f = setupSandboxed(t, `{"mode": "none"}`, nil)
	if !strings.Contains(f.app.asked[0], "Sandbox: Not sandboxed (declared by the extension)") {
		t.Errorf("confirmation:\n%s", f.app.asked[0])
	}
	if err := f.exec(t, "status", nil); err != nil {
		t.Fatal(err)
	}
	if in := f.h.List()[0].Sandbox; in.State != SandboxDeclaredOff || in.Summary != "Not sandboxed (declared by the extension)" {
		t.Errorf("info = %+v", in)
	}

	// Declared, but the system has none: runs unconfined, with the reason.
	none := func() sandbox.Support { return sandbox.Support{Reason: "this kernel has no Landlock"} }
	f = setupSandboxed(t, `{}`, func(o *Options) { o.Sandbox = none })
	if err := f.exec(t, "status", nil); err != nil {
		t.Fatal(err)
	}
	in = f.h.List()[0].Sandbox
	if in.State != SandboxUnavailable || in.Summary != "Not sandboxed: not available on this system (this kernel has no Landlock)" {
		t.Errorf("info = %+v", in)
	}
	if !strings.Contains(f.log(t), "[flint] not sandboxed: not available on this system (this kernel has no Landlock)") || strings.Contains(f.log(t), "FLINT_EXTENSION_SANDBOX") {
		t.Errorf("log:\n%s", f.log(t))
	}
}

func TestRequireSandbox(t *testing.T) {
	require := true
	on := func(o *Options) { o.RequireSandbox = func() bool { return require } }
	// Without a sandbox section the extension is not started, and that is
	// not a crash.
	f := setup(t, on)
	for range maxCrashes + 1 {
		err := f.exec(t, "status", nil)
		if err == nil || !strings.Contains(err.Error(), "only runs sandboxed extensions") || !strings.Contains(err.Error(), "doesn't declare a sandbox") {
			t.Fatalf("err = %v", err)
		}
	}
	in := f.h.List()[0]
	if in.State != StateStopped || !in.Sandbox.Refused || f.log(t) != "" {
		t.Errorf("state %s, sandbox %+v, log %q", in.State, in.Sandbox, f.log(t))
	}
	// Turning the setting off lets it run; turning it on again stops it.
	require = false
	if err := f.exec(t, "status", nil); err != nil {
		t.Fatal(err)
	}
	require = true
	f.h.SandboxSettingChanged()
	if f.state() != StateStopped || !strings.Contains(f.log(t), "stopping: only sandboxed extensions may run") {
		t.Errorf("state %s\n%s", f.state(), f.log(t))
	}

	// Declared but unavailable, and declared off: refused as well.
	none := func() sandbox.Support { return sandbox.Support{Reason: "no kernel support"} }
	f = setupSandboxed(t, `{}`, func(o *Options) { on(o); o.Sandbox = none })
	if err := f.exec(t, "status", nil); err == nil || !strings.Contains(err.Error(), "not available on this system (no kernel support)") {
		t.Errorf("err = %v", err)
	}
	f = setupSandboxed(t, `{"mode": "none"}`, on)
	if err := f.exec(t, "status", nil); err == nil || !strings.Contains(err.Error(), "declared by the extension") {
		t.Errorf("err = %v", err)
	}

	// A sandboxed one runs.
	if sandbox.Probe().Available {
		f = setupSandboxed(t, `{}`, on)
		if err := f.exec(t, "status", nil); err != nil {
			t.Errorf("a sandboxed extension is refused: %v", err)
		}
		if in := f.h.List()[0].Sandbox; in.Refused {
			t.Errorf("info = %+v", in)
		}
	}
}

func TestDescribeSandbox(t *testing.T) {
	parse := func(run, permissions string) *manifest.Manifest {
		t.Helper()
		m, err := manifest.Parse([]byte(`{"contract": 1, "id": "acme.ext", "name": "Ext", "version": "1.0.0", "run": ` + run + `, "permissions": ` + permissions + `,
		  "contributes": {"commands": [{"id": "acme.ext.go", "title": "Go"}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	old := sandbox.Support{Available: true, Mechanism: "Landlock (ABI 1)", Gaps: []string{"Network access and local sockets are not blocked (this needs seccomp, or Linux 6.7 for Landlock's TCP rules)."}}
	in := describeSandbox(parse(`{"command": "x", "env": ["KUBECONFIG"], "sandbox": {"read": ["~/.kube", "${env:KUBECONFIG}"], "write": ["~/out"], "localSockets": true, "exec": true}}`, `["workspace.read", "network:api.example.com,*.example.org"]`), old, []string{"~/x is not granted: it doesn't exist"})
	want := []string{
		"Reads: its own folder, system files, trusted project folders, ~/.kube, ${env:KUBECONFIG}",
		"Writes: its data folder, a private temporary folder, ~/out",
		"Network: allowed, to any host (declared: api.example.com, *.example.org; the sandbox can't limit it to them)",
		"Local sockets: allowed (services on this machine, such as Docker or the desktop's message bus)",
		"Other programs: may start any program it can read; they run in the same sandbox",
		"Limits: 4096 open files, no core dumps",
		"Network access and local sockets are not blocked (this needs seccomp, or Linux 6.7 for Landlock's TCP rules).",
		"~/x is not granted: it doesn't exist",
	}
	if in.State != SandboxOn || in.Summary != "Sandboxed (Landlock (ABI 1))" || strings.Join(in.Details, "\n") != strings.Join(want, "\n") {
		t.Errorf("sandboxed: %+v", in)
	}
	var b strings.Builder
	sandboxMessage(&b, in)
	if msg := b.String(); !strings.Contains(msg, "Sandbox: Sandboxed (Landlock (ABI 1))\n• Reads: its own folder") || !strings.Contains(msg, "• Network access and local sockets are not blocked") {
		t.Errorf("message:\n%s", msg)
	}
	if in := describeSandbox(parse(`{"command": "x", "sandbox": {}}`, `[]`), old, nil); in.Details[0] != "Reads: its own folder, system files" || in.Details[2] != "Network: blocked" || in.Details[3] != "Other programs: only its own" || len(in.Details) != 6 {
		t.Errorf("strict: %+v", in)
	}
	for _, tt := range []struct {
		run, summary, state string
		s                   sandbox.Support
	}{
		{`{"command": "x"}`, "Not sandboxed: the extension doesn't declare a sandbox (run.sandbox)", SandboxUndeclared, old},
		{`{"command": "x", "sandbox": {"mode": "none"}}`, "Not sandboxed (declared by the extension)", SandboxDeclaredOff, old},
		{`{"command": "x", "sandbox": {}}`, "Not sandboxed: not available on this system (/usr/bin/sandbox-exec is missing)", SandboxUnavailable, sandbox.Support{Reason: "/usr/bin/sandbox-exec is missing"}},
	} {
		in := describeSandbox(parse(tt.run, `[]`), tt.s, nil)
		if in.State != tt.state || in.Summary != tt.summary || len(in.Details) != 1 || !strings.Contains(in.Details[0], "your user's permissions") {
			t.Errorf("%s: %+v", tt.run, in)
		}
		var b strings.Builder
		sandboxMessage(&b, in)
		if !strings.Contains(b.String(), "Sandbox: "+tt.summary+"\n") || !strings.Contains(b.String(), "cannot confine what it does") {
			t.Errorf("%s: message:\n%s", tt.run, b.String())
		}
	}
	// A theme-only extension has nothing to confine.
	m, err := manifest.Parse([]byte(`{"contract": 1, "id": "acme.theme", "name": "T", "version": "1.0.0", "contributes": {"themes": [{"id": "acme.theme.dark", "label": "D", "base": "dark", "colors": {"--bg": "#000000"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if in := describeSandbox(m, old, nil); in.State != SandboxNone {
		t.Errorf("no program: %+v", in)
	}
}
