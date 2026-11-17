package hostmode_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/agenthost/hostmode"
	"flint/internal/terminal"
)

// The end-to-end tests run the test binary itself as the host, the way Flint
// runs itself with --agent-host: Spawn starts os.Executable() with that flag
// and FLINT_TEST_AGENT_HOST=1 (inherited from the test) turns it into a host.
func TestMain(m *testing.M) {
	if os.Getenv("FLINT_TEST_AGENT_HOST") == "1" {
		hostmode.Main(hostmode.Options{Version: "e2e", AllowShell: true}) // never returns
		os.Exit(99)
	}
	os.Exit(m.Run())
}

// output collects terminal output by id.
type output struct {
	mu      sync.Mutex
	data    map[string]string
	changed chan struct{}
}

func newOutput() *output { return &output{data: map[string]string{}, changed: make(chan struct{}, 1)} }

func (o *output) Output(id string, b []byte) {
	o.mu.Lock()
	o.data[id] += string(b)
	o.mu.Unlock()
	select {
	case o.changed <- struct{}{}:
	default:
	}
}
func (o *output) Exited(string, int)          {}
func (o *output) Title(string, terminal.Info) {}

func (o *output) waitFor(t *testing.T, id, want string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		o.mu.Lock()
		got := o.data[id]
		o.mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		select {
		case <-o.changed:
		case <-deadline:
			t.Fatalf("terminal %s output = %q, want it to contain %q", id, got, want)
		}
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestHostEndToEnd(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "fh") // short: a socket path is limited to about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	t.Setenv("FLINT_CONFIG_DIR", cfg)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	t.Setenv("FLINT_TEST_AGENT_HOST", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Nothing runs yet.
	if _, err := hostmode.Dial(ctx, cfg, client.Config{}); !errors.Is(err, hostmode.ErrNotRunning) {
		t.Fatalf("Dial with no host = %v, want ErrNotRunning", err)
	}

	out := newOutput()
	c, err := hostmode.Spawn(ctx, cfg, client.Config{Version: "e2e", Handlers: client.Handlers{Terminal: out}})
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Hello().Pid
	if pid == 0 || pid == os.Getpid() {
		t.Fatalf("host pid = %d", pid)
	}
	t.Cleanup(func() { // never leave a host behind, whatever failed
		if alive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if got := c.Hello(); got.Version != "e2e" || len(got.Terminals) != 0 || len(got.Chats) != 0 {
		t.Errorf("first hello = %+v", got)
	}

	// A second host process loses the lock and says so with its own status.
	second := exec.Command(os.Args[0], "--agent-host")
	if err := second.Run(); err == nil {
		t.Error("a second host ran to completion")
	} else if ee := (*exec.ExitError)(nil); !errors.As(err, &ee) || ee.ExitCode() != hostmode.ExitAlreadyRunning {
		t.Errorf("second host = %v, want exit status %d", err, hostmode.ExitAlreadyRunning)
	}

	// A terminal, and its output through the client.
	if err := c.Terminals().Start("t1", terminal.Options{Cwd: cfg, Shell: "/bin/sh", Args: []string{"-c", "echo hi"}}); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "t1", "hi")

	// A window closes; the host and its session stay.
	_ = c.Close()
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("client did not close")
	}
	if !alive(pid) {
		t.Fatal("the host exited when its only client left")
	}

	// Another window dials the same host and finds the terminal.
	out2 := newOutput()
	c2, err := hostmode.Dial(ctx, cfg, client.Config{Version: "e2e", Handlers: client.Handlers{Terminal: out2}})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	hello := c2.Hello()
	if hello.Pid != pid {
		t.Errorf("second window found pid %d, want %d", hello.Pid, pid)
	}
	if len(hello.Terminals) != 1 || hello.Terminals[0] != "t1" {
		t.Fatalf("hello terminals = %v, want [t1]", hello.Terminals)
	}
	if err := c2.AttachTerminal("t1", 0); err != nil {
		t.Fatal(err)
	}
	out2.waitFor(t, "t1", "hi") // replayed from the host's ring

	// host.shutdown: the process exits and removes what it created.
	if err := c2.Shutdown(ctx, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatal("the host did not exit after host.shutdown")
	}
	run := hostmode.RunDir(cfg)
	for _, name := range []string{"host.sock", "host.token"} {
		if _, err := os.Lstat(filepath.Join(run, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived the host: %v", name, err)
		}
	}
	if _, err := hostmode.Dial(ctx, cfg, client.Config{}); !errors.Is(err, hostmode.ErrNotRunning) {
		t.Errorf("Dial after shutdown = %v, want ErrNotRunning", err)
	}
}

// Two windows starting together: both Spawn, one host wins the lock and the
// loser's Spawn still ends up connected to it.
func TestConcurrentSpawnsEndWithOneHost(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "fh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	t.Setenv("FLINT_CONFIG_DIR", cfg)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	t.Setenv("FLINT_TEST_AGENT_HOST", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type result struct {
		c   *client.Client
		err error
	}
	res := make(chan result, 2)
	for range 2 {
		go func() {
			c, err := hostmode.Spawn(ctx, cfg, client.Config{Version: "e2e"})
			res <- result{c, err}
		}()
	}
	var pids []int
	var clients []*client.Client
	for range 2 {
		r := <-res
		if r.err != nil {
			// The loser may have raced the winner's startup; that is allowed to
			// fail with a clear error, but must not leave two hosts.
			if !errors.Is(r.err, hostmode.ErrNotRunning) {
				t.Errorf("Spawn = %v", r.err)
			}
			continue
		}
		clients = append(clients, r.c)
		pids = append(pids, r.c.Hello().Pid)
	}
	for _, c := range clients {
		defer c.Close()
	}
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if len(pids) == 0 {
		t.Fatal("no Spawn succeeded")
	}
	for _, pid := range pids {
		if pid != pids[0] {
			t.Errorf("two hosts: pids %v", pids)
		}
	}
	_ = clients[0].Shutdown(ctx, true)
}
