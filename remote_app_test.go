package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/agenttask"
	"flint/internal/remote"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// fakeRemote is the router's remote side: it records calls like fakeTerminals
// and what Attach and Detach were asked.
type fakeRemote struct {
	*fakeTerminals
	attached []string
	detached bool
	started  []terminal.Options
}

func newFakeRemote() *fakeRemote { return &fakeRemote{fakeTerminals: newFakeTerminals()} }

func (f *fakeRemote) Start(id string, opts terminal.Options) error {
	f.started = append(f.started, opts)
	return f.fakeTerminals.Start(id, opts)
}
func (f *fakeRemote) Attach(_ context.Context, id string) error {
	f.attached = append(f.attached, id)
	return nil
}
func (f *fakeRemote) Detach() { f.detached = true }

func TestRouterSendsMachinePrefixedIDsToTheRemoteSideOnly(t *testing.T) {
	main, streams, rm := newFakeTerminals(), newFakeTerminals(), newFakeRemote()
	r := newTerminalsRouter(main, streams)
	r.SetRemote(rm)

	if err := r.Start("box:abc", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Write("box:abc", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if !rm.saw("start:box:abc") || !rm.saw("write:box:abc") {
		t.Errorf("remote calls %v", rm.calls)
	}
	if len(main.calls)+len(streams.calls) != 0 {
		t.Errorf("a remote id reached a local manager: main %v streams %v", main.calls, streams.calls)
	}

	// Local ids keep going where they went before.
	for _, id := range []string{"0f8e6c1e-1111-4222-8333-444455556666", "agent-9a8b7c", "run-1"} {
		if err := r.Start(id, terminal.Options{}); err != nil {
			t.Fatal(err)
		}
		if !main.saw("start:" + id) {
			t.Errorf("local id %q did not reach main: %v", id, main.calls)
		}
	}
	if len(rm.calls) != 2 {
		t.Errorf("a local id reached the remote side: %v", rm.calls)
	}

	// Even an unknown or malformed prefix is the remote side's to refuse, never a fall-through to local.
	main.calls = nil
	for _, id := range []string{"nosuch:abc", "-x:abc", "a:b:c", ":x"} {
		_ = r.Write(id, []byte("x"))
		r.Close(id)
	}
	if len(main.calls) != 0 {
		t.Errorf("an unknown prefix fell through to the local manager: %v", main.calls)
	}
}

func TestRouterStreamIDsWinOverTheMachinePrefix(t *testing.T) {
	main, streams, rm := newFakeTerminals(), newFakeTerminals(), newFakeRemote()
	r := newTerminalsRouter(main, streams)
	r.SetRemote(rm)
	if err := r.StartStream("docker:web", terminal.Stream{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Write("docker:web", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if !streams.saw("write:docker:web") || len(rm.calls) != 0 {
		t.Errorf("streams %v remote %v", streams.calls, rm.calls)
	}
}

func TestRouterWithoutRemoteRefusesMachineIDs(t *testing.T) {
	main, streams := newFakeTerminals(), newFakeTerminals()
	r := newTerminalsRouter(main, streams)
	if err := r.Start("box:abc", terminal.Options{}); err == nil {
		t.Error("started a terminal on a machine without remote support")
	}
	if err := r.AttachRemote(context.Background(), "box:abc"); err == nil {
		t.Error("attached without remote support")
	}
	if len(main.calls) != 0 {
		t.Errorf("fell through to main: %v", main.calls)
	}
}

// Closing the window detaches from machines (their sessions keep running);
// closing one tab ends only that session.
func TestRouterCloseAllDetachesRemoteAndCloseEndsOne(t *testing.T) {
	main, streams, rm := newFakeTerminals(), newFakeTerminals(), newFakeRemote()
	r := newTerminalsRouter(main, streams)
	r.SetRemote(rm)
	r.Close("box:abc")
	if !rm.saw("close:box:abc") || rm.detached {
		t.Errorf("close: %v detached %v", rm.calls, rm.detached)
	}
	r.CloseAll()
	if !rm.detached || rm.closed {
		t.Errorf("CloseAll detached %v closed-sessions %v: it must detach, not end them", rm.detached, rm.closed)
	}
}

func TestTerminalBindingsKeepLocalPathsOutOfRemoteStarts(t *testing.T) {
	main, streams, rm := newFakeTerminals(), newFakeTerminals(), newFakeRemote()
	r := newTerminalsRouter(main, streams)
	r.SetRemote(rm)
	a := &App{terminals: r, termRouter: r}
	api := &TerminalAPI{app: a}

	// The generic start (local cwd, env) refuses a machine id outright.
	if err := api.Start("box:abc", "/Users/me/project", 80, 24, map[string]string{"AWS_PROFILE": "prod"}); err == nil {
		t.Error("Start accepted a remote id with a local cwd")
	}
	if len(rm.calls) != 0 || len(main.calls) != 0 {
		t.Fatalf("calls %v %v", rm.calls, main.calls)
	}
	// StartRemote takes an id and a size, and sends nothing else.
	if err := api.StartRemote("box:abc", 120, 40); err != nil {
		t.Fatal(err)
	}
	got := rm.started[0]
	if got.Cwd != "" || len(got.Env) != 0 || got.Shell != "" || len(got.Args) != 0 || got.Cols != 120 || got.Rows != 40 {
		t.Errorf("options sent to the machine: %+v", got)
	}
	// It does not start local terminals under a bare id, nor accept a malformed prefix.
	for _, id := range []string{"0f8e6c1e-1111-4222-8333-444455556666", "BOX:abc", "box:", "a:b:c", ""} {
		if err := api.StartRemote(id, 80, 24); err == nil {
			t.Errorf("StartRemote(%q) was accepted", id)
		}
	}
	if len(main.calls) != 0 {
		t.Errorf("StartRemote reached a local manager: %v", main.calls)
	}
	// A restored tab attaches through Attach, which sends the machine id only.
	if err := api.Attach("box:abc"); err != nil || len(rm.attached) != 1 || rm.attached[0] != "box:abc" {
		t.Errorf("Attach = %v %v", err, rm.attached)
	}
}

// Every id the app makes for a local terminal is free of ':' — the web view's
// UUIDs, randomID and task ids — so none can be read as a machine prefix.
func TestLocalTerminalIDsNeverLookRemote(t *testing.T) {
	for range 200 {
		for _, id := range []string{randomID(), "agent-" + randomID(), agenttask.NewID()} {
			if remote.IsRemoteID(id) {
				t.Fatalf("local id %q has a machine prefix", id)
			}
		}
	}
}

func TestConfirmRemoteCommandShowsTheExactCommandLine(t *testing.T) {
	m := settings.Machine{ID: "box", Name: "Build box", Target: "me@box.example", Port: 2222, Command: "~/bin/flint"}
	connect, _ := remote.HostArgv(m)
	check, _ := remote.CheckArgv(m)
	for _, tt := range []struct {
		kind string
		argv []string
		ok   string
	}{{remote.ConfirmConnect, connect, "Connect"}, {remote.ConfirmCheck, check, "Run Check"}} {
		var title, message, button string
		a := &App{ask: func(ti, me, ok string) bool { title, message, button = ti, me, ok; return false }}
		if a.confirmRemoteCommand(m, tt.kind, tt.argv) {
			t.Errorf("%s: a declined dialog was taken for a yes", tt.kind)
		}
		if !strings.Contains(message, commandLine(tt.argv[0], tt.argv[1:])) || !strings.Contains(message, "--") {
			t.Errorf("%s: the dialog does not show the command line:\n%s", tt.kind, message)
		}
		if !strings.Contains(title, "Build box") || button != tt.ok {
			t.Errorf("%s: title %q button %q", tt.kind, title, button)
		}
		a.ask = func(string, string, string) bool { return true }
		if !a.confirmRemoteCommand(m, tt.kind, tt.argv) {
			t.Errorf("%s: a yes was lost", tt.kind)
		}
	}
	// Without a window (no native dialog available) the answer is no.
	if (&App{}).confirmRemoteCommand(m, remote.ConfirmConnect, connect) {
		t.Error("confirmed without any dialog")
	}
}

func TestMachinesAPICheckListAppliesTheDefaultCommand(t *testing.T) {
	api := &MachinesAPI{app: &App{}}
	if got := api.CheckList([]settings.Machine{{ID: "box", Target: "box"}}); got != "" {
		t.Errorf("an empty command is the default: %q", got)
	}
	if got := api.CheckList([]settings.Machine{{ID: "box", Target: "-oProxyCommand=x"}}); got == "" {
		t.Error("a hostile target passed")
	}
	if got := api.CheckList([]settings.Machine{{ID: "a:b", Target: "box"}}); got == "" {
		t.Error("an id with a colon passed")
	}
	if st := api.Status(); st == nil || len(st) != 0 {
		t.Errorf("status without a hub = %v", st)
	}
}

// ---- end to end, with a fake ssh on PATH ----

// The fake ssh becomes the "remote" Flint: the test binary run with
// --agent-host --stdio (TestMain turns it into the host when
// FLINT_TEST_AGENT_HOST is set), on a config directory of its own.
const e2eSSH = `#!/bin/sh
echo "$@" >> "$FAKE_SSH_DIR/argv"
exec "$FLINT_TEST_BIN" --agent-host --stdio
`

func waitRemote(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRemoteTerminalEndToEndThroughTheRouter(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "fr") // short: socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	bin, fake := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(e2eSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home) // the "remote user's" home
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("FLINT_CONFIG_DIR", cfg)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	t.Setenv("FLINT_TEST_AGENT_HOST", "1")
	t.Setenv("FLINT_TEST_BIN", exe)
	t.Setenv("FAKE_SSH_DIR", fake)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() { stopHostOf(cfg) })

	machines := []settings.Machine{{ID: "box", Name: "Box", Target: "me@box.example", Port: 2222, Command: "flint"}}
	var asked atomic.Int32
	newWindow := func() (*App, *termRecorder) {
		ev := newTermEventsRecorder()
		a := &App{configDir: cfg, settings: settings.Settings{Machines: machines}}
		a.ask = func(string, string, string) bool { asked.Add(1); return true }
		a.emitHook = func(string, ...any) {}
		a.wireTerminals(ev, dialHost, spawnHost)
		return a, ev
	}

	// First run: open a remote terminal through the router, as the tab does.
	a1, ev1 := newWindow()
	api1 := &TerminalAPI{app: a1}
	if err := api1.StartRemote("box:t1", 100, 30); err != nil {
		t.Fatal(err)
	}
	if err := a1.terminals.Write("box:t1", []byte("pwd -P; echo E2E$((40+2)); printf '\\033]6977;state=working\\007'\n")); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, "output of the remote shell", func() bool { return strings.Contains(ev1.output("box:t1"), "E2E42") })
	if out := ev1.output("box:t1"); !strings.Contains(out, home) {
		t.Errorf("the remote shell did not start in the remote home %q:\n%s", home, out)
	}
	waitRemote(t, "the agent state", func() bool { return ev1.agentState("box:t1") == terminal.AgentWorking })
	if asked.Load() != 1 {
		t.Errorf("dialogs = %d, want 1 for the first use of the machine", asked.Load())
	}

	// A local terminal beside it still behaves as before.
	if err := a1.terminals.Start("local1", terminal.Options{Cwd: cfg, Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	if err := a1.terminals.Write("local1", []byte("echo L$((20+1))\n")); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, "local output", func() bool { return strings.Contains(ev1.output("local1"), "L21") })

	// Quitting leaves the remote session running and the local shell ended.
	a1.closeTerminals()

	// Next run: the restored tab attaches to the session the machine kept.
	a2, ev2 := newWindow()
	t.Cleanup(a2.closeTerminals)
	api2 := &TerminalAPI{app: a2}
	if err := api2.Attach("box:t1"); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, "replayed output", func() bool { return strings.Contains(ev2.output("box:t1"), "E2E42") })
	if err := a2.terminals.Write("box:t1", []byte("echo AGAIN$((1+1))\nexit 5\n")); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, "output after re-attach", func() bool { return strings.Contains(ev2.output("box:t1"), "AGAIN2") })
	waitRemote(t, "the exit code", func() bool { c, ok := ev2.exit("box:t1"); return ok && c == 5 })

	// A session that no longer exists is an error, never a new shell.
	if err := api2.Attach("box:never-existed"); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("attach of a missing session = %v", err)
	}
	// An unknown machine is refused and nothing fell through to a local shell.
	if err := api2.StartRemote("elsewhere:t9", 80, 24); err == nil {
		t.Error("started a terminal on an unknown machine")
	}
	if err := api2.Attach("elsewhere:t9"); err == nil {
		t.Error("attached on an unknown machine")
	}
	a2.terminals.Close("box:t1") // closing the tab ends the remote session
}

// stopHostOf asks the host of a config directory to stop, and kills it if it will not.
func stopHostOf(cfg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := dialHost(ctx, cfg, client.Config{Version: "cleanup"})
	if err != nil {
		return
	}
	pid := c.Hello().Pid
	_ = c.Shutdown(ctx, true)
	_ = c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
