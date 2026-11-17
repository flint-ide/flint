package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/settings"
	"flint/internal/terminal"
)

// events records everything a Hub reports about terminals, under the ids the
// window sees.
type events struct {
	*termLog
	mu     sync.Mutex
	titles map[string]terminal.Info
	agent  map[string]string
}

func newEvents() *events {
	return &events{termLog: newTermLog(), titles: map[string]terminal.Info{}, agent: map[string]string{}}
}

func (e *events) Title(id string, info terminal.Info) {
	e.mu.Lock()
	e.titles[id] = info
	e.mu.Unlock()
}

func (e *events) AgentState(id, state string) {
	e.mu.Lock()
	e.agent[id] = state
	e.mu.Unlock()
}

func (e *events) agentState(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agent[id]
}

func (e *events) exitCode(id string) (int, bool) {
	e.termLog.mu.Lock()
	defer e.termLog.mu.Unlock()
	c, ok := e.exits[id]
	return c, ok
}

type hubFixture struct {
	*env
	hub      *Hub
	ev       *events
	clock    *fakeClock
	st       *statusLog
	mu       sync.Mutex
	asked    [][]string // argv of every dialog
	kinds    []string
	answer   bool
	statuses []MachineStatus
}

func newHubFixture(t *testing.T) *hubFixture {
	t.Helper()
	f := &hubFixture{env: newEnv(t), ev: newEvents(), clock: newFakeClock(), st: &statusLog{}, answer: true}
	// The remote shell starts in the "remote" user's home, here a directory of the test.
	t.Setenv("HOME", realPath(t, t.TempDir()))
	t.Setenv("SHELL", "/bin/sh")
	f.hub = NewHub(HubConfig{
		Version: "test", Events: f.ev,
		Confirm: func(_ settings.Machine, kind string, argv []string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.asked, f.kinds = append(f.asked, argv), append(f.kinds, kind)
			return f.answer
		},
		OnStatus: func(s MachineStatus) {
			f.mu.Lock()
			f.statuses = append(f.statuses, s)
			f.mu.Unlock()
		},
		newConn: func(c Config) (*Conn, error) {
			c.Clock, c.Jitter = f.clock, func() float64 { return 0.5 }
			return New(c)
		},
	})
	f.hub.SetMachines([]settings.Machine{testMachine()})
	t.Cleanup(f.hub.Detach)
	return f
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// start opens a shell as the window does: a size, nothing else.
func (f *hubFixture) start(id string) {
	f.t.Helper()
	if err := f.hub.Start(id, terminal.Options{Cols: 100, Rows: 30}); err != nil {
		f.t.Fatal(err)
	}
}

// say types a command line into a terminal.
func (f *hubFixture) say(id, line string) {
	f.t.Helper()
	if err := f.hub.Write(id, []byte(line+"\n")); err != nil {
		f.t.Fatal(err)
	}
}

func (f *hubFixture) dialogs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

func (f *hubFixture) setAnswer(yes bool) {
	f.mu.Lock()
	f.answer = yes
	f.mu.Unlock()
}

func (f *hubFixture) lastStatus() MachineStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[len(f.statuses)-1]
}

func TestHubRefusesIDsThatAreNotRemoteOrNotConfigured(t *testing.T) {
	f := newHubFixture(t)
	if err := f.hub.Start("0f8e6c1e-1111-4222-8333-444455556666", terminal.Options{}); !errors.Is(err, ErrNotRemoteID) {
		t.Errorf("a local id: %v", err)
	}
	for _, id := range []string{"other:abc", "BOX:abc", "box:", ":abc", "box:a:b", "-x:abc"} {
		if err := f.hub.Start(id, terminal.Options{}); err == nil {
			t.Errorf("Start(%q) was accepted", id)
		}
		if err := f.hub.Write(id, []byte("x")); err == nil {
			t.Errorf("Write(%q) was accepted", id)
		}
	}
	if err := f.hub.Start("other:abc", terminal.Options{}); !errors.Is(err, ErrUnknownMachine) {
		t.Errorf("unknown machine: %v", err)
	}
	if len(f.pids()) != 0 || f.dialogs() != 0 {
		t.Errorf("something ran for a refused id: %d ssh, %d dialogs", len(f.pids()), f.dialogs())
	}
}

func TestHubDeclinedDialogMeansNoConnection(t *testing.T) {
	f := newHubFixture(t)
	f.setAnswer(false)
	err := f.hub.Start("box:t1", terminal.Options{})
	if !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("Start = %v, want ErrNotConfirmed", err)
	}
	if len(f.pids()) != 0 {
		t.Fatal("ssh ran although the user declined")
	}
	if s := f.lastStatus(); s.State != StateFailed || !strings.Contains(s.Error, "declined") {
		t.Errorf("status after declining: %+v", s)
	}
	// Asked again at the next attempt, and a yes then connects.
	f.setAnswer(true)
	f.start("box:t1")
	f.say("box:t1", "echo OK$((40+2))")
	f.ev.waitFor(t, "box:t1", "OK42")
	if f.dialogs() != 2 {
		t.Errorf("dialogs = %d, want 2", f.dialogs())
	}
}

// The dialog shows exactly the command that runs, and only once per run.
func TestHubAsksOncePerMachineWithTheExactCommand(t *testing.T) {
	f := newHubFixture(t)
	f.start("box:t1")
	f.start("box:t2")
	f.say("box:t2", "echo TWO$((1+1))")
	f.ev.waitFor(t, "box:t2", "TWO2")
	want, _ := HostArgv(testMachine())
	if f.dialogs() != 1 || !slices.Equal(f.asked[0], want) || f.kinds[0] != ConfirmConnect {
		t.Fatalf("dialogs %v kinds %v, want one for %q", f.asked, f.kinds, want)
	}
	if len(f.pids()) != 1 {
		t.Errorf("ssh ran %d times: one Conn per machine", len(f.pids()))
	}
}

func TestHubStartsInTheRemoteHomeAndSendsNoLocalCwdOrEnv(t *testing.T) {
	f := newHubFixture(t)
	home := os.Getenv("HOME")
	t.Setenv("AWS_PROFILE", "")
	opts := terminal.Options{
		Cwd: "/Users/someone/project", Env: []string{"AWS_PROFILE=prod"}, Shell: "/bin/sh", Args: []string{"-c", "echo owned"},
	}
	if err := f.hub.Start("box:t1", opts); err != nil {
		t.Fatal(err)
	}
	// Neither the local path, the env, nor a shell chosen by the window reach the remote.
	f.say("box:t1", "pwd -P; echo profile=${AWS_PROFILE:-none}")
	f.ev.waitFor(t, "box:t1", "profile=none")
	f.ev.waitFor(t, "box:t1", home)
	if out := f.ev.text("box:t1"); strings.Contains(out, "someone") || strings.Contains(out, "profile=prod") || strings.Contains(out, "owned") {
		t.Errorf("local cwd or env leaked to the remote shell: %q", out)
	}
}

func TestHubAgentStateTitleAndExitFlowThroughTheRoute(t *testing.T) {
	f := newHubFixture(t)
	f.start("box:t1")
	f.say("box:t1", `printf '\033]6977;state=waiting\007'; echo RAN$((1+2)); sleep 0.3; exit 3`)
	f.ev.waitFor(t, "box:t1", "RAN3")
	eventually(t, "agent state", func() bool { return f.ev.agentState("box:t1") == terminal.AgentWaiting })
	eventually(t, "exit code", func() bool { c, ok := f.ev.exitCode("box:t1"); return ok && c == 3 })
	if _, ok := f.ev.exitCode("t1"); ok {
		t.Error("an event carried the unprefixed id")
	}
}

func TestPrefixedEventsAddTheMachineToEveryID(t *testing.T) {
	ev := newEvents()
	p := prefixed{machine: "box", next: ev}
	p.Title("t", terminal.Info{Command: "vim", Cwd: "/srv"})
	p.AgentState("t", terminal.AgentDone)
	p.Exited("t", 7)
	if ev.titles["box:t"].Command != "vim" || ev.agentState("box:t") != terminal.AgentDone {
		t.Errorf("titles %v agent %v", ev.titles, ev.agent)
	}
	if c, ok := ev.exitCode("box:t"); !ok || c != 7 {
		t.Errorf("exit %v %v", c, ok)
	}
}

func TestHubReconnectsAndReattachesAfterTheWindowClosed(t *testing.T) {
	f := newHubFixture(t)
	f.start("box:t1")
	f.say("box:t1", "echo FIRST$((1+0))")
	f.ev.waitFor(t, "box:t1", "FIRST1")

	// The network drops: the Conn reconnects by itself and the tab goes on.
	f.killSSH()
	eventually(t, "reconnecting", func() bool { return f.lastStatus().State == StateReconnecting })
	if got := f.clock.nextAsked(t); got != time.Second {
		t.Errorf("first retry after %v", got)
	}
	f.clock.fire()
	eventually(t, "connected again", func() bool { return f.lastStatus().State == StateConnected })
	f.say("box:t1", "echo SECOND$((1+1))")
	f.ev.waitFor(t, "box:t1", "SECOND2")

	// Closing the window leaves the remote session running.
	f.hub.Detach()
	f.noSSHLeft()

	// The next run: a new Hub (no Conn yet) re-attaches under the same id.
	ev2 := newEvents()
	h2 := NewHub(HubConfig{Version: "test", Events: ev2, Confirm: func(settings.Machine, string, []string) bool { return true }})
	h2.SetMachines([]settings.Machine{testMachine()})
	t.Cleanup(h2.Detach)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := h2.Attach(ctx, "box:t1"); err != nil {
		t.Fatal(err)
	}
	ev2.waitFor(t, "box:t1", "SECOND2") // replayed from the host's buffer
	if err := h2.Write("box:t1", []byte("echo THIRD$((1+2))\n")); err != nil {
		t.Fatal(err)
	}
	ev2.waitFor(t, "box:t1", "THIRD3")
	// A session that is gone is reported, never replaced by a new shell.
	if err := h2.Attach(ctx, "box:gone"); !errors.Is(err, ErrNoSession) {
		t.Errorf("attach of a missing session = %v, want ErrNoSession", err)
	}
	if err := h2.Write("box:gone", []byte("x")); err == nil {
		// Write to an id never started or attached must fail on the remote.
		t.Error("write to a session that does not exist succeeded")
	}
	// Closing the tab ends the remote session.
	h2.Close("box:t1")
	eventually(t, "session closed on the remote", func() bool {
		cl, err := dialHost(ctx, f.cfg)
		if err != nil {
			return false
		}
		defer cl.Close()
		list, err := cl.ListTerminals(ctx)
		return err == nil && len(list) == 0
	})
}

func TestHubReportsASessionTheMachineLost(t *testing.T) {
	f := newHubFixture(t)
	f.start("box:t1")
	f.say("box:t1", "echo UP$((1+0))")
	f.ev.waitFor(t, "box:t1", "UP1")
	f.stopHost() // the remote's host goes away with its sessions
	f.killSSH()
	f.clock.nextAsked(t)
	f.clock.fire()
	eventually(t, "session ended notice", func() bool { return strings.Contains(f.ev.text("box:t1"), "session ended on box") })
	eventually(t, "exit reported", func() bool { _, ok := f.ev.exitCode("box:t1"); return ok })
}

func TestHubSettingsChangeTearsDownTheConnection(t *testing.T) {
	f := newHubFixture(t)
	f.start("box:t1")
	f.say("box:t1", "echo UP$((1+0))")
	f.ev.waitFor(t, "box:t1", "UP1")

	// A new name is only a label: the connection stays.
	renamed := testMachine()
	renamed.Name = "Build box"
	f.hub.SetMachines([]settings.Machine{renamed})
	if _, ok := f.ev.exitCode("box:t1"); ok || len(f.hub.allConns()) != 1 {
		t.Fatal("a rename closed the connection")
	}
	if s := f.hub.Statuses(); len(s) != 1 || s[0].Name != "Build box" {
		t.Errorf("statuses %+v", s)
	}

	// Another target is another machine: closed, told, and asked about again.
	changed := renamed
	changed.Target = "other@box.example"
	f.hub.SetMachines([]settings.Machine{changed})
	f.noSSHLeft()
	if _, ok := f.ev.exitCode("box:t1"); !ok || !strings.Contains(f.ev.text("box:t1"), "changed in Settings") {
		t.Errorf("the tab was not told: %q", f.ev.text("box:t1"))
	}
	if len(f.hub.allConns()) != 0 {
		t.Error("the old Conn is still there")
	}
	f.start("box:t2")
	f.say("box:t2", "echo AGAIN$((1+0))")
	f.ev.waitFor(t, "box:t2", "AGAIN1")
	if f.dialogs() != 2 {
		t.Errorf("dialogs = %d: a changed machine must be confirmed again", f.dialogs())
	}
	if argv := f.asked[1]; !slices.Contains(argv, "other@box.example") {
		t.Errorf("second dialog showed %q", argv)
	}

	// Removing it closes the Conn and refuses further use.
	f.hub.SetMachines(nil)
	f.noSSHLeft()
	if err := f.hub.Write("box:t2", []byte("x")); !errors.Is(err, ErrUnknownMachine) {
		t.Errorf("write after removal = %v", err)
	}
	if err := f.hub.Start("box:t3", terminal.Options{}); !errors.Is(err, ErrUnknownMachine) {
		t.Errorf("start after removal = %v", err)
	}
}

func TestHubDetachLeavesNoSSHAndRefusesNewConnections(t *testing.T) {
	f := newHubFixture(t)
	f.start("box:t1")
	f.say("box:t1", "echo UP$((1+0))")
	f.ev.waitFor(t, "box:t1", "UP1")
	f.hub.Detach()
	f.noSSHLeft()
	if err := f.hub.Start("box:t2", terminal.Options{}); !errors.Is(err, ErrClosed) {
		t.Errorf("start after detach = %v", err)
	}
}

func TestHubCheckAsksFirstAndUsesTheCheckArgv(t *testing.T) {
	f := newHubFixture(t)
	f.setAnswer(false)
	if _, err := f.hub.Check(context.Background(), "box"); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("declined check = %v", err)
	}
	if len(f.pids()) != 0 {
		t.Fatal("ssh ran for a declined check")
	}
	f.setAnswer(true)
	res, err := f.hub.Check(context.Background(), "box") // the fake ssh runs this test binary as the relay: --version is not a flag it knows
	if err != nil {
		t.Fatal(err)
	}
	want, _ := CheckArgv(testMachine())
	if f.kinds[len(f.kinds)-1] != ConfirmCheck || !slices.Equal(f.asked[len(f.asked)-1], want) {
		t.Errorf("dialog %v %v", f.kinds, f.asked)
	}
	if res.Status == "" {
		t.Error("no verdict")
	}
	if _, err := f.hub.Check(context.Background(), "nope"); !errors.Is(err, ErrUnknownMachine) {
		t.Errorf("unknown machine = %v", err)
	}
}
