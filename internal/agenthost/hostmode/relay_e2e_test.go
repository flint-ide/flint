package hostmode_test

import (
	"context"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/agenthost/hostmode"
	"flint/internal/terminal"
)

type pipeConn struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (p pipeConn) Close() error {
	for _, c := range p.closers {
		_ = c.Close()
	}
	return nil
}

// runRelay runs hostmode.Relay over pipes and returns the far side of them.
func runRelay(ctx context.Context, cfg string) (conn pipeConn, done <-chan error) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	d := make(chan error, 1)
	go func() {
		err := hostmode.Relay(ctx, cfg, inR, outW)
		_ = outW.Close()
		_ = inR.Close()
		d <- err
	}()
	return pipeConn{Reader: outR, Writer: inW, closers: []io.Closer{inW, outR}}, d
}

// With no host running, the relay starts one (the test binary as the host,
// as in TestHostEndToEnd); the host outlives the relay.
func TestRelayStartsTheHostWhenNoneRuns(t *testing.T) {
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

	conn, done := runRelay(ctx, cfg)
	out := newOutput()
	c, err := client.New(conn, client.Config{Version: "e2e", Handlers: client.Handlers{Terminal: out}})
	if err != nil {
		t.Fatal(err)
	}
	pid := c.Hello().Pid
	if pid == 0 || pid == os.Getpid() {
		t.Fatalf("host pid = %d", pid)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := c.Terminals().Start("r1", terminal.Options{Cwd: cfg, Shell: "/bin/sh", Args: []string{"-c", "echo via-relay; sleep 60"}}); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "r1", "via-relay")

	_ = c.Close() // ssh went away
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Relay = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the relay did not end")
	}
	if !alive(pid) {
		t.Fatal("the host ended with the relay")
	}

	// A second relay finds the same host and terminal.
	conn2, _ := runRelay(ctx, cfg)
	out2 := newOutput()
	c2, err := client.New(conn2, client.Config{Version: "e2e", Handlers: client.Handlers{Terminal: out2}})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if c2.Hello().Pid != pid {
		t.Errorf("second relay reached pid %d, want %d", c2.Hello().Pid, pid)
	}
	if err := c2.AttachTerminal("r1", 0); err != nil {
		t.Fatal(err)
	}
	out2.waitFor(t, "r1", "via-relay")
	_ = c2.Shutdown(ctx, true)
}

// A host that cannot be started has its own exit status. Without
// FLINT_TEST_AGENT_HOST the spawned test binary rejects --agent-host as an
// unknown test flag and exits at once.
func TestRelayReportsAHostThatCannotStart(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "fh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	t.Setenv("FLINT_CONFIG_DIR", cfg)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	t.Setenv("FLINT_TEST_AGENT_HOST", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, done := runRelay(ctx, cfg)
	defer conn.Close()
	go func() {
		_, _ = io.WriteString(conn, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"version":"x","token":""}}`+"\n")
	}()
	select {
	case err := <-done:
		if code := hostmode.RelayExitCode(err); code != hostmode.ExitRelayHostFailed {
			t.Errorf("Relay = %v (exit %d), want exit %d", err, code, hostmode.ExitRelayHostFailed)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the relay did not end")
	}
}
