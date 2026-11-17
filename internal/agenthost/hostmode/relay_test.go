package hostmode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	"flint/internal/terminal"
)

// capture is an io.Writer that keeps everything written to it.
type capture struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(p)
}

func (c *capture) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.b.Bytes()...)
}

// relayConn is one relay running over pipes, seen from the far client's side:
// it reads what the relay wrote to stdout and writes what the relay reads as
// stdin. stdout keeps a copy of everything the relay wrote.
type relayConn struct {
	inW    *io.PipeWriter
	outR   *io.PipeReader
	stdout *capture
	done   chan error // what Relay returned
}

func startRelay(t *testing.T, configDir string) *relayConn {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	rc := &relayConn{inW: inW, outR: outR, stdout: &capture{}, done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := Relay(ctx, configDir, inR, io.MultiWriter(rc.stdout, outW))
		_ = outW.Close()
		_ = inR.Close() // like a closed stdin: the writer's next write fails
		rc.done <- err
	}()
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = outR.Close()
	})
	return rc
}

func (r *relayConn) Read(p []byte) (int, error)  { return r.outR.Read(p) }
func (r *relayConn) Write(p []byte) (int, error) { return r.inW.Write(p) }
func (r *relayConn) Close() error {
	_ = r.inW.Close()
	return r.outR.Close()
}

func (r *relayConn) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the relay did not end")
		return nil
	}
}

// termLog collects terminal output.
type termLog struct {
	mu      sync.Mutex
	out     map[string]string
	changed chan struct{}
}

func newTermLog() *termLog {
	return &termLog{out: map[string]string{}, changed: make(chan struct{}, 1)}
}

func (l *termLog) Output(id string, b []byte) {
	l.mu.Lock()
	l.out[id] += string(b)
	l.mu.Unlock()
	select {
	case l.changed <- struct{}{}:
	default:
	}
}
func (l *termLog) Exited(string, int)          {}
func (l *termLog) Title(string, terminal.Info) {}
func (l *termLog) AgentState(string, string)   {}

func (l *termLog) waitFor(t *testing.T, id, want string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		l.mu.Lock()
		got := l.out[id]
		l.mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		select {
		case <-l.changed:
		case <-deadline:
			t.Fatalf("terminal %s output = %q, want %q", id, got, want)
		}
	}
}

func relayHost(t *testing.T) (cfg string, paths Paths) {
	t.Helper()
	cfg = shortDir(t)
	paths, _ = startHostWith(t, runConfig{ConfigDir: cfg, Version: "relay-test", Idle: time.Hour, AllowShell: true})
	return cfg, paths
}

func TestRelayGivesTheFarClientAnAuthenticatedSession(t *testing.T) {
	cfg, paths := relayHost(t)
	token, err := os.ReadFile(paths.Token)
	if err != nil || len(token) < 16 {
		t.Fatalf("token: %v", err)
	}

	rc := startRelay(t, cfg)
	log := newTermLog()
	// The far client has no token: whatever it sends there is replaced.
	c, err := client.New(rc, client.Config{Token: "not-the-real-token-at-all", Version: "far", Handlers: client.Handlers{Terminal: log}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h := c.Hello(); !h.Compatible || h.Version != "relay-test" {
		t.Fatalf("hello = %+v", h)
	}
	if err := c.Terminals().Start("t1", terminal.Options{Shell: "/bin/sh", Args: []string{"-c", "echo relay-ok; sleep 60"}}); err != nil {
		t.Fatal(err)
	}
	log.waitFor(t, "t1", "relay-ok")
	if err := c.Terminals().Write("t1", []byte("x")); err != nil {
		t.Errorf("a call after hello: %v", err)
	}

	if got := rc.stdout.bytes(); bytes.Contains(got, token) || bytes.Contains(got, bytes.TrimSpace(token)) {
		t.Error("the token appeared in the relay's stdout")
	}
	if got := rc.stdout.bytes(); len(got) == 0 || got[0] != '{' {
		t.Errorf("the first byte on stdout is not protocol: %q", got[:min(len(got), 40)])
	}
	c.Terminals().CloseAll()
}

func TestRelayStdoutIsOnlyProtocol(t *testing.T) {
	cfg, _ := relayHost(t)
	rc := startRelay(t, cfg)
	c, err := client.New(rc, client.Config{Version: "far"})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	rc.wait(t)
	for i, line := range bytes.Split(bytes.TrimRight(rc.stdout.bytes(), "\n"), []byte("\n")) {
		if len(line) == 0 || line[0] != '{' || line[len(line)-1] != '}' {
			t.Errorf("stdout line %d is not a JSON object: %q", i, line)
		}
	}
}

func TestRelayEndsOnEOFButTheHostKeepsTheSession(t *testing.T) {
	cfg, paths := relayHost(t)

	rc1 := startRelay(t, cfg)
	log1 := newTermLog()
	c1, err := client.New(rc1, client.Config{Version: "far", Handlers: client.Handlers{Terminal: log1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c1.Terminals().Start("keep", terminal.Options{Shell: "/bin/sh", Args: []string{"-c", "echo first-run; sleep 60"}}); err != nil {
		t.Fatal(err)
	}
	log1.waitFor(t, "keep", "first-run")

	// ssh drops: stdin of the relay reaches EOF.
	_ = rc1.inW.Close()
	if err := rc1.wait(t); err != nil {
		t.Errorf("Relay after EOF on stdin = %v, want nil", err)
	}

	// The host and its terminal are still there for a second relay.
	rc2 := startRelay(t, cfg)
	log2 := newTermLog()
	c2, err := client.New(rc2, client.Config{Version: "far2", Handlers: client.Handlers{Terminal: log2}})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if h := c2.Hello(); len(h.Terminals) != 1 || h.Terminals[0] != "keep" {
		t.Fatalf("the second relay's hello lists %v, want [keep]", h.Terminals)
	}
	if err := c2.AttachTerminal("keep", 0); err != nil {
		t.Fatal(err)
	}
	log2.waitFor(t, "keep", "first-run") // replayed from the host

	if _, err := os.Stat(paths.Sock); err != nil {
		t.Errorf("the host's socket: %v", err)
	}
	c2.Terminals().CloseAll()
}

func TestRelayEndsWhenTheHostCloses(t *testing.T) {
	cfg, _ := relayHost(t)
	rc := startRelay(t, cfg)
	c, err := client.New(rc, client.Config{Version: "far"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// host.shutdown makes the host close every connection and exit.
	_ = c.Shutdown(context.Background(), true)
	if err := rc.wait(t); err != nil {
		t.Errorf("Relay after the host went away = %v, want nil", err)
	}
}

// A first message that is not a hello never reaches the host, and the relay
// says nothing on stdout.
func TestRelayRefusesAFirstMessageThatIsNotHello(t *testing.T) {
	cfg, _ := relayHost(t)
	tests := []struct {
		name string
		in   string
	}{
		{"another method", `{"jsonrpc":"2.0","id":1,"method":"term.list","params":{}}` + "\n"},
		{"not JSON", "hello\n"},
		{"no id", `{"jsonrpc":"2.0","method":"hello","params":{"protocol":1}}` + "\n"},
		{"object id", `{"jsonrpc":"2.0","id":{"a":1},"method":"hello","params":{"protocol":1}}` + "\n"},
		{"bad params", `{"jsonrpc":"2.0","id":1,"method":"hello","params":"x"}` + "\n"},
		{"too long", `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"version":"` + strings.Repeat("a", maxHelloLine) + `"}}` + "\n"},
		{"no newline", `{"jsonrpc":"2.0","id":1,"method":"hello"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := startRelay(t, cfg)
			go func() { _, _ = io.WriteString(rc.inW, tt.in); _ = rc.inW.Close() }()
			err := rc.wait(t)
			if got := RelayExitCode(err); got != ExitRelayBadHello {
				t.Errorf("Relay = %v (exit %d), want exit %d", err, got, ExitRelayBadHello)
			}
			if got := rc.stdout.bytes(); len(got) != 0 {
				t.Errorf("stdout = %q, want nothing", got)
			}
		})
	}
}

func TestRelayWithNothingSentEndsCleanly(t *testing.T) {
	cfg, _ := relayHost(t)
	rc := startRelay(t, cfg)
	_ = rc.inW.Close()
	if err := rc.wait(t); err != nil {
		t.Errorf("Relay = %v, want nil", err)
	}
	if got := rc.stdout.bytes(); len(got) != 0 {
		t.Errorf("stdout = %q", got)
	}
}

// A client cannot make the relay hand out the token: the token is written
// only to the socket, and the host never echoes it. These are the ways a
// hostile client could try.
func TestClientCannotReadTheTokenThroughTheRelay(t *testing.T) {
	cfg, paths := relayHost(t)
	token, _ := os.ReadFile(paths.Token)

	rc := startRelay(t, cfg)
	// A hello whose fields are crafted, then a second hello, then calls: the
	// host answers the first and closes on the second.
	msgs := `{"jsonrpc":"2.0","id":"a","method":"hello","params":{"protocol":1,"version":"v","token":"x","extra":"${TOKEN}"}}` + "\n"
	go func() { _, _ = io.WriteString(rc.inW, msgs) }()
	buf := make([]byte, 4096)
	n, _ := rc.Read(buf)
	if !strings.Contains(string(buf[:n]), `"id":"a"`) {
		t.Fatalf("no hello reply: %q", buf[:n])
	}
	_, _ = io.WriteString(rc.inW, `{"jsonrpc":"2.0","id":2,"method":"hello","params":{"protocol":1}}`+"\n")
	_, _ = io.WriteString(rc.inW, `{"jsonrpc":"2.0","id":3,"method":"host.settingsChanged","params":{}}`+"\n")
	_ = rc.inW.Close()
	rc.wait(t)
	all := rc.stdout.bytes()
	if bytes.Contains(all, token) || bytes.Contains(all, bytes.TrimSpace(token)) {
		t.Error("the token reached stdout")
	}
}

func TestWithTokenRewritesOnlyTheToken(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":7,"method":"hello","params":{"protocol":1,"version":"far","token":"mine","other":"x"}}` + "\n")
	got, err := withToken(line, "LOCAL-TOKEN-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"jsonrpc":"2.0","id":7,"method":"hello","params":{"protocol":1,"version":"far","token":"LOCAL-TOKEN-0123456789"}}` + "\n"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if strings.Contains(string(got), "mine") || strings.Contains(string(got), "other") {
		t.Error("client-supplied params survived")
	}
	long, err := withToken([]byte(`{"id":1,"method":"hello","params":{"version":"`+strings.Repeat("v", 500)+`"}}`+"\n"), "T")
	if err != nil || len(long) > 300 {
		t.Errorf("long version: %d bytes, %v", len(long), err)
	}
}

func TestRelayExitCodes(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want int
	}{
		{nil, 0},
		{&RelayError{ExitRelayNoPeerCred, errors.New("x")}, ExitRelayNoPeerCred},
		{&RelayError{ExitRelayHostFailed, errors.New("x")}, ExitRelayHostFailed},
		{errors.New("other"), ExitRelayHostFailed},
	} {
		if got := RelayExitCode(tt.err); got != tt.want {
			t.Errorf("RelayExitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
	codes := map[int]bool{0: true, ExitAlreadyRunning: true, ExitRelayHostFailed: true, ExitRelayNoPeerCred: true, ExitRelayBadHello: true}
	if len(codes) != 5 {
		t.Error("exit codes are not distinct")
	}
}

func TestRelayWithoutPeerCredentials(t *testing.T) {
	if peerCredSupported {
		t.Skip("this platform has them")
	}
	err := Relay(context.Background(), shortDir(t), strings.NewReader(""), io.Discard)
	if RelayExitCode(err) != ExitRelayNoPeerCred {
		t.Errorf("Relay = %v", err)
	}
}

// Keep the compiler honest about the protocol constant the relay relies on.
var _ = agenthost.MethodHello
