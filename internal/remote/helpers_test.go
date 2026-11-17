package remote

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/agenthost/hostmode"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// The test binary is also the remote Flint: the relay ("--agent-host
// --stdio") and the host it starts ("--agent-host --ready-fd=3") are this
// same binary, run by the fake ssh below.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--agent-host" {
		hostmode.Main(hostmode.Options{Version: "remote-test", AllowShell: true}) // never returns
		os.Exit(99)
	}
	os.Exit(m.Run())
}

// fakeSSHScript stands in for ssh. It ignores the ssh options, records its
// pid and arguments, and then, depending on the mode file, fails like ssh
// does, hangs, is noisy on stderr, or becomes the relay of the "remote"
// machine (this test binary) on its own stdin and stdout, as sshd would run
// "flint --agent-host --stdio".
const fakeSSHScript = `#!/bin/sh
d="$FAKE_SSH_DIR"
echo $$ >> "$d/pids"
printf '%s\n' "$@" > "$d/argv.$$"
mode=$(cat "$d/mode" 2>/dev/null)
case "$mode" in
  deny)   echo "Permission denied (publickey)." >&2; exit 255 ;;
  secret) echo "debug1: token=ghp_0123456789abcdefghijABCDEFGHIJ0123 AKIAABCDEFGHIJKLMNOP" >&2
          echo "Permission denied (publickey)." >&2; exit 255 ;;
  hang)   exec sleep 300 ;;
  noisy)  i=0; while [ $i -lt 400 ]; do
            echo "noise line $i xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" >&2
            i=$((i+1)); done ;;
esac
exec "$FLINT_TEST_BIN" --agent-host --stdio
`

// fakeBin writes an executable "ssh" with the given script into a new
// directory and returns the directory.
func fakeBin(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// env is a "remote machine" on this computer: a config directory of its own
// (so its host is its own), a fake ssh first on PATH, and a way to look at
// what the fake saw.
type env struct {
	t    *testing.T
	cfg  string // the remote's config directory
	fake string // FAKE_SSH_DIR: pids, argv.<pid>, mode
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg, err := os.MkdirTemp("/tmp", "rm") // short: socket paths are limited to about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	bin := fakeBin(t, fakeSSHScript)
	fake := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLINT_CONFIG_DIR", cfg)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	t.Setenv("FAKE_SSH_DIR", fake)
	t.Setenv("FLINT_TEST_BIN", exe)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := &env{t: t, cfg: cfg, fake: fake}
	t.Cleanup(e.stopHost)
	return e
}

func (e *env) setMode(mode string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.fake, "mode"), []byte(mode), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// pids are the processes the fake ssh ran as, oldest first.
func (e *env) pids() []int {
	data, _ := os.ReadFile(filepath.Join(e.fake, "pids"))
	var out []int
	for _, f := range strings.Fields(string(data)) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// argv is what the fake ssh with this pid was started with.
func (e *env) argv(pid int) []string {
	data, err := os.ReadFile(filepath.Join(e.fake, "argv."+strconv.Itoa(pid)))
	if err != nil {
		e.t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// killSSH kills the newest fake ssh, as a dropped network would end it.
func (e *env) killSSH() {
	e.t.Helper()
	p := e.pids()
	if len(p) == 0 {
		e.t.Fatal("no ssh was run")
	}
	_ = syscall.Kill(p[len(p)-1], syscall.SIGKILL)
}

func gone(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH }

// noSSHLeft fails when a process the fake ssh ran as is still alive.
func (e *env) noSSHLeft() {
	e.t.Helper()
	for _, pid := range e.pids() {
		deadline := time.Now().Add(5 * time.Second)
		for !gone(pid) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !gone(pid) {
			e.t.Errorf("ssh process %d is still running", pid)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// stopHost shuts the remote's host down (and kills it if it will not go).
func (e *env) stopHost() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := hostmode.Dial(ctx, e.cfg, client.Config{Version: "cleanup"})
	if err != nil {
		return // no host
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

func testMachine() settings.Machine {
	return settings.Machine{ID: "box", Target: "me@box.example", Port: 2222, Command: "flint"}
}

// eventually polls cond until it holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// statusLog records every status the Conn reports.
type statusLog struct {
	mu   sync.Mutex
	list []Status
}

func (s *statusLog) record(st Status) {
	s.mu.Lock()
	s.list = append(s.list, st)
	s.mu.Unlock()
}

func (s *statusLog) states() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]State, len(s.list))
	for i, st := range s.list {
		out[i] = st.State
	}
	return out
}

// termLog collects terminal output and exits.
type termLog struct {
	mu    sync.Mutex
	out   map[string]*bytes.Buffer
	exits map[string]int
}

func newTermLog() *termLog {
	return &termLog{out: map[string]*bytes.Buffer{}, exits: map[string]int{}}
}

func (l *termLog) Output(id string, b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.out[id] == nil {
		l.out[id] = &bytes.Buffer{}
	}
	l.out[id].Write(b)
}

func (l *termLog) Exited(id string, code int) {
	l.mu.Lock()
	l.exits[id] = code
	l.mu.Unlock()
}
func (l *termLog) Title(string, terminal.Info) {}
func (l *termLog) AgentState(string, string)   {}

func (l *termLog) text(id string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b := l.out[id]; b != nil {
		return b.String()
	}
	return ""
}

func (l *termLog) waitFor(t *testing.T, id, want string) {
	t.Helper()
	eventually(t, "output "+want+" of "+id+" (have "+l.text(id)+")", func() bool { return strings.Contains(l.text(id), want) })
}

// fakeClock is a Clock whose timers the test fires. Each After(d) announces d
// on asked and waits for fire.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []chan time.Time
	asked   chan time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0), asked: make(chan time.Duration, 64)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.mu.Lock()
	c.pending = append(c.pending, ch)
	c.mu.Unlock()
	c.asked <- d
	return ch
}

// advance moves Now forward.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// fire releases the oldest timer.
func (c *fakeClock) fire() {
	c.mu.Lock()
	ch := c.pending[0]
	c.pending = c.pending[1:]
	now := c.now
	c.mu.Unlock()
	ch <- now
}

func (c *fakeClock) nextAsked(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.asked:
		return d
	case <-time.After(20 * time.Second):
		t.Fatal("the Conn did not schedule a retry")
		return 0
	}
}

func (c *fakeClock) noneAsked(t *testing.T) {
	t.Helper()
	select {
	case d := <-c.asked:
		t.Errorf("a retry was scheduled (%v) but nothing uses the machine", d)
	case <-time.After(150 * time.Millisecond):
	}
}

// dialHost connects to the remote's host directly, as a third party would.
func dialHost(ctx context.Context, cfg string) (*client.Client, error) {
	return hostmode.Dial(ctx, cfg, client.Config{Version: "probe"})
}
