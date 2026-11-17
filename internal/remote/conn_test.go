package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/settings"
	"flint/internal/terminal"
)

type fixture struct {
	*env
	conn  *Conn
	log   *termLog
	clock *fakeClock
	st    *statusLog
	lost  chan string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{env: newEnv(t), log: newTermLog(), clock: newFakeClock(), st: &statusLog{}, lost: make(chan string, 8)}
	c, err := New(Config{
		Machine: testMachine(), Version: "test",
		Handlers: client.Handlers{Terminal: f.log},
		OnStatus: f.st.record,
		OnLost:   func(kind, id string) { f.lost <- kind + ":" + id },
		Clock:    f.clock,
		Jitter:   func() float64 { return 0.5 }, // no spread: exact delays
	})
	if err != nil {
		t.Fatal(err)
	}
	f.conn = c
	t.Cleanup(func() { _ = c.Close() })
	return f
}

func sh(script string) terminal.Options {
	return terminal.Options{Shell: "/bin/sh", Args: []string{"-c", script}}
}

func (f *fixture) waitState(want State) {
	f.t.Helper()
	eventually(f.t, "state "+string(want), func() bool { return f.conn.Status().State == want })
}

// The whole path: argv, a child process, the relay, the host, a terminal.
func TestConnRunsTerminalsThroughSSH(t *testing.T) {
	f := newFixture(t)
	if got := f.conn.Status().State; got != StateIdle {
		t.Fatalf("a new Conn is %s, want idle: it connects at first use", got)
	}
	if len(f.pids()) != 0 {
		t.Fatal("ssh ran before the first use")
	}

	if err := f.conn.Terminals().Start("t1", sh("echo via-ssh; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "via-ssh")
	f.waitState(StateConnected)

	pids := f.pids()
	if len(pids) != 1 {
		t.Fatalf("ssh ran %d times", len(pids))
	}
	want, err := HostArgv(testMachine())
	if err != nil {
		t.Fatal(err)
	}
	if got := f.argv(pids[0]); !slices.Equal(got, want[1:]) {
		t.Errorf("ssh was run with\n %q\nwant\n %q", got, want[1:])
	}

	if err := f.conn.Terminals().Write("t1", []byte("\n")); err != nil {
		t.Errorf("Write: %v", err)
	}
	if cwd, err := f.conn.Terminals().Cwd("t1"); err != nil || cwd == "" {
		t.Errorf("Cwd = %q, %v", cwd, err)
	}

	if err := f.conn.Close(); err != nil {
		t.Fatal(err)
	}
	f.noSSHLeft()
	if got := f.conn.Status().State; got != StateClosed {
		t.Errorf("state after Close = %s", got)
	}
	if err := f.conn.Terminals().Write("t1", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Write after Close = %v", err)
	}
	if err := f.conn.Terminals().Start("t2", sh("true")); !errors.Is(err, ErrClosed) {
		t.Errorf("Start after Close = %v", err)
	}
	if states := f.st.states(); states[0] != StateConnecting || states[len(states)-1] != StateClosed || !slices.Contains(states, StateConnected) {
		t.Errorf("states = %v", states)
	}
}

// The remote session outlives the connection and a new connection continues
// from the right seq: nothing is lost, nothing twice.
func TestConnReconnectsAndContinuesFromTheLastSeq(t *testing.T) {
	f := newFixture(t)
	flag := filepath.Join(f.cfg, "go")
	script := `echo one; while [ ! -e ` + flag + ` ]; do sleep 0.05; done; echo two; read x; echo three-$x; sleep 60`
	if err := f.conn.Terminals().Start("t1", sh(script)); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "one")

	f.killSSH() // the network drops
	f.waitState(StateReconnecting)
	if err := f.conn.Terminals().Write("t1", []byte("x")); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Write while reconnecting = %v, want ErrNotConnected", err)
	}
	// The remote shell goes on while we are away.
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if d := f.clock.nextAsked(t); d != time.Second {
		t.Errorf("first retry after %v, want 1s", d)
	}
	f.clock.fire()
	f.log.waitFor(t, "t1", "two") // replayed from where we left off
	f.waitState(StateConnected)

	if err := f.conn.Terminals().Write("t1", []byte("Z\n")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "three-Z")
	out := f.log.text("t1")
	for _, w := range []string{"one", "two", "three-Z"} {
		if n := strings.Count(out, w+"\r\n"); n != 1 {
			t.Errorf("%q appears %d times in %q", w, n, out)
		}
	}
	if n := len(f.pids()); n != 2 {
		t.Errorf("ssh ran %d times, want 2", n)
	}
	f.clock.noneAsked(t)
}

// The backoff is 1, 2, 4 … 60 s while a failing attempt follows another and a
// session needs the machine; it stops when none does.
func TestConnBackoffSequenceAndStopWhenUnused(t *testing.T) {
	f := newFixture(t)
	if err := f.conn.Terminals().Start("t1", sh("echo up; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "up")

	f.setMode("deny")
	f.killSSH()
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i, w := range want {
		if d := f.clock.nextAsked(t); d != w*time.Second {
			t.Fatalf("retry %d after %v, want %v", i, d, w*time.Second)
		}
		f.clock.fire()
	}
	// Every failed attempt is reported as failed, with ssh's words.
	f.clock.nextAsked(t) // the next wait is scheduled: the last attempt ended
	st := f.conn.Status()
	if st.State != StateFailed && st.State != StateReconnecting {
		t.Errorf("state = %s", st.State)
	}
	eventually(t, "a failed status with ssh's stderr", func() bool {
		s := f.conn.Status()
		return s.State == StateFailed && strings.Contains(s.Stderr, "Permission denied (publickey).") && s.Attempt >= len(want)
	})

	// The last session ends: no further attempt, without waiting for the timer.
	f.conn.Terminals().Close("t1")
	eventually(t, "idle", func() bool { return f.conn.Status().State == StateIdle || f.conn.Status().State == StateFailed })
	f.clock.noneAsked(t)
	n := len(f.pids())
	f.clock.mu.Lock()
	pending := len(f.clock.pending)
	f.clock.mu.Unlock()
	for range pending {
		f.clock.fire() // a timer that was already running must not cause an attempt
	}
	time.Sleep(200 * time.Millisecond)
	if got := len(f.pids()); got != n {
		t.Errorf("ssh ran again after the last session ended (%d -> %d)", n, got)
	}
}

func TestConnDoesNotReconnectWhenNothingIsInUse(t *testing.T) {
	f := newFixture(t)
	if err := f.conn.Terminals().Start("t1", sh("echo done")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "done")
	eventually(t, "the exit", func() bool {
		f.log.mu.Lock()
		defer f.log.mu.Unlock()
		_, ok := f.log.exits["t1"]
		return ok
	})
	f.killSSH()
	f.waitState(StateIdle)
	f.clock.noneAsked(t)
	if n := len(f.pids()); n != 1 {
		t.Errorf("ssh ran %d times", n)
	}
	// A later use connects again by itself.
	if err := f.conn.Terminals().Start("t2", sh("echo again; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t2", "again")
}

// ssh exiting before any protocol byte is a failure with ssh's explanation,
// and nothing retries in a loop.
func TestConnReportsAuthenticationFailure(t *testing.T) {
	f := newFixture(t)
	f.setMode("deny")
	err := f.conn.Terminals().Start("t1", sh("true"))
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Fatalf("Start = %v, want a *ConnectError", err)
	}
	if ce.ExitCode != 255 || !strings.Contains(ce.Stderr, "Permission denied (publickey).") {
		t.Errorf("error = %+v", ce)
	}
	if !strings.Contains(err.Error(), "status 255") || !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("message = %q", err)
	}
	st := f.conn.Status()
	if st.State != StateFailed || !strings.Contains(st.Stderr, "Permission denied (publickey).") || !strings.Contains(st.Err, "Permission denied") {
		t.Errorf("status = %+v", st)
	}
	f.clock.noneAsked(t)
	time.Sleep(100 * time.Millisecond)
	if n := len(f.pids()); n != 1 {
		t.Errorf("ssh ran %d times after one failed Start", n)
	}
	f.noSSHLeft()

	// Fixing the problem is enough: the next use connects.
	f.setMode("")
	if err := f.conn.Terminals().Start("t1", sh("echo ok; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "ok")
	f.waitState(StateConnected)
}

func TestConnRedactsStderr(t *testing.T) {
	f := newFixture(t)
	f.setMode("secret")
	err := f.conn.Terminals().Start("t1", sh("true"))
	if err == nil {
		t.Fatal("Start succeeded")
	}
	st := f.conn.Status()
	for _, s := range []string{st.Stderr, st.Err, err.Error()} {
		if strings.Contains(s, "ghp_0123456789") || strings.Contains(s, "AKIAABCDEFGHIJKLMNOP") {
			t.Errorf("a secret reached the status: %q", s)
		}
	}
	if !strings.Contains(st.Stderr, "[github token]") || !strings.Contains(st.Stderr, "Permission denied (publickey).") {
		t.Errorf("stderr = %q", st.Stderr)
	}
}

func TestConnKeepsAtMost8KiBOfStderr(t *testing.T) {
	f := newFixture(t)
	f.setMode("noisy") // 400 lines of about 90 bytes, then the relay
	if err := f.conn.Terminals().Start("t1", sh("echo quiet; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "quiet")
	eventually(t, "the noise", func() bool { return strings.Contains(f.conn.Status().Stderr, "noise line 399") })
	s := f.conn.Status().Stderr
	if len(s) > StderrLimit {
		t.Errorf("stderr is %d bytes, limit %d", len(s), StderrLimit)
	}
	if strings.Contains(s, "noise line 0 ") {
		t.Error("the oldest lines were not dropped")
	}
}

// Close while ssh is still logging in ends the attempt and the process.
func TestConnCloseDuringAConnectionAttempt(t *testing.T) {
	f := newFixture(t)
	f.setMode("hang") // ssh that never speaks
	errc := make(chan error, 1)
	go func() { errc <- f.conn.Terminals().Start("t1", sh("true")) }()
	eventually(t, "ssh to start", func() bool { return len(f.pids()) == 1 })
	if st := f.conn.Status().State; st != StateConnecting {
		t.Errorf("state = %s, want connecting", st)
	}
	if err := f.conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Error("Start succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after Close")
	}
	f.noSSHLeft()
}

func TestConnCloseWhileWaitingToReconnect(t *testing.T) {
	f := newFixture(t)
	if err := f.conn.Terminals().Start("t1", sh("echo up; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "up")
	f.killSSH()
	f.waitState(StateReconnecting)
	f.clock.nextAsked(t)
	if err := f.conn.Close(); err != nil {
		t.Fatal(err)
	}
	f.noSSHLeft()
	if n := len(f.pids()); n != 1 {
		t.Errorf("ssh ran %d times", n)
	}
}

// A session the remote host lost (here: the host was restarted) is reported,
// not retried.
func TestConnReportsSessionsTheHostNoLongerHas(t *testing.T) {
	f := newFixture(t)
	if err := f.conn.Terminals().Start("t1", sh("echo up; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "up")

	f.setMode("hang")
	f.killSSH()
	f.waitState(StateReconnecting)
	f.stopHost() // the remote host ends, and takes its sessions with it
	f.setMode("")
	f.clock.nextAsked(t)
	f.clock.fire()
	select {
	case got := <-f.lost:
		if got != "terminal:t1" {
			t.Errorf("lost %q", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the lost terminal was not reported")
	}
	f.waitState(StateConnected)
	// Nothing is in use now: dropping the link does not start retries.
	f.killSSH()
	f.waitState(StateIdle)
	f.clock.noneAsked(t)
}

func TestNewRefusesAnInvalidMachine(t *testing.T) {
	_, err := New(Config{Machine: settings.Machine{ID: "x", Target: "-oProxyCommand=id", Command: "flint"}})
	if err == nil {
		t.Fatal("New accepted a target that is an ssh option")
	}
}

func TestSessionsClosedWhileDownAreClosedOnTheRemoteLater(t *testing.T) {
	f := newFixture(t)
	if err := f.conn.Terminals().Start("t1", sh("echo up; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t1", "up")
	f.setMode("hang")
	f.killSSH()
	f.waitState(StateReconnecting)
	f.conn.Terminals().Close("t1") // while down; also ends the need for the machine
	f.waitState(StateIdle)

	f.setMode("")
	if err := f.conn.Terminals().Start("t2", sh("echo next; sleep 60")); err != nil {
		t.Fatal(err)
	}
	f.log.waitFor(t, "t2", "next")
	// A third party looks at the host: t1 is gone, t2 is there.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hc, err := dialHost(ctx, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer hc.Close()
	if got := hc.Hello().Terminals; !slices.Equal(got, []string{"t2"}) {
		t.Errorf("the host holds %v, want [t2]", got)
	}
}

func TestBackoffTable(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60, 60}
	for i, w := range want {
		if got := backoff(i); got != w*time.Second {
			t.Errorf("backoff(%d) = %v, want %v", i, got, w*time.Second)
		}
	}
}

func TestJitterStaysWithinTwentyPercent(t *testing.T) {
	for _, r := range []float64{0, 0.25, 0.5, 0.999999, 7, -3} {
		c := &Conn{cfg: Config{Jitter: func() float64 { return r }}}
		for _, base := range []time.Duration{time.Second, 8 * time.Second, 60 * time.Second} {
			got := c.jittered(base)
			if lo, hi := time.Duration(0.8*float64(base)), time.Duration(1.2*float64(base)); got < lo || got > hi {
				t.Errorf("jitter %v of %v = %v, outside [%v, %v]", r, base, got, lo, hi)
			}
		}
	}
	if got := (&Conn{cfg: Config{Jitter: func() float64 { return 0.5 }}}).jittered(10 * time.Second); got != 10*time.Second {
		t.Errorf("the middle of the spread = %v", got)
	}
}

func TestTailRedactsAndBounds(t *testing.T) {
	tl := &tail{}
	// A secret split across writes is still caught: lines are redacted whole.
	_, _ = tl.Write([]byte("a token=ghp_0123456789abc"))
	_, _ = tl.Write([]byte("defghijABCDEFGHIJ0123\nnext line\n"))
	if s := tl.String(); strings.Contains(s, "ghp_0123") || !strings.Contains(s, "next line") {
		t.Errorf("tail = %q", s)
	}
	// An unfinished line is shown, redacted too.
	_, _ = tl.Write([]byte("partial AKIAABCDEFGHIJKLMNOP"))
	if s := tl.String(); strings.Contains(s, "AKIAABCD") || !strings.Contains(s, "partial") {
		t.Errorf("tail with a partial line = %q", s)
	}
	for range 2000 {
		_, _ = tl.Write([]byte("0123456789012345678901234567890123456789\n"))
	}
	if n := len(tl.String()); n > StderrLimit {
		t.Errorf("tail is %d bytes", n)
	}
	_, _ = tl.Write([]byte(strings.Repeat("x", 3*StderrLimit))) // one enormous line
	if n := len(tl.String()); n > StderrLimit {
		t.Errorf("tail after a huge line is %d bytes", n)
	}
	// Invalid UTF-8 from a cut is dropped, not shown.
	_, _ = tl.Write([]byte("\xff\xfe\n"))
	if s := tl.String(); !strings.HasSuffix(s, "x") && strings.ContainsRune(s, '�') {
		t.Errorf("tail has invalid text: %q", s[max(0, len(s)-10):])
	}
}
