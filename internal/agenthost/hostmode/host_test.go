package hostmode

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// readyBuf is a Ready writer that tells when the host said it listens.
type readyBuf struct {
	bytes.Buffer
	closed chan struct{}
}

func newReadyBuf() *readyBuf { return &readyBuf{closed: make(chan struct{})} }

func (r *readyBuf) Close() error { close(r.closed); return nil }

// startHost runs a host in this process on a config dir of its own and waits
// until it listens. The returned func stops it and returns what run returned.
func startHost(t *testing.T, configDir string, idle time.Duration) (paths Paths, stop func() error) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", configDir) // Linux: keep the run dir inside the temp dir
	ready := newReadyBuf()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- run(ctx, runConfig{ConfigDir: configDir, Version: "test", Idle: idle, Ready: ready})
	}()
	select {
	case <-ready.closed:
	case err := <-errc:
		cancel()
		t.Fatalf("host ended before it was ready: %v", err)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("host never became ready")
	}
	if ready.String() != "ready\n" {
		t.Errorf("readiness message = %q", ready.String())
	}
	paths = pathsIn(RunDir(configDir))
	var stopped bool
	stop = func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-errc:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("host did not stop")
			return nil
		}
	}
	t.Cleanup(func() { _ = stop() })
	return paths, stop
}

func TestHostFilesWhileRunningAndAfterStopping(t *testing.T) {
	cfg := shortDir(t)
	paths, stop := startHost(t, cfg, time.Hour)

	for path, mode := range map[string]os.FileMode{paths.Lock: 0o600, paths.Token: 0o600, paths.Sock: 0o600} {
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("%s mode = %v, want %v", filepath.Base(path), fi.Mode().Perm(), mode)
		}
	}
	if fi, err := os.Stat(filepath.Dir(paths.Sock)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("run dir = %v, %v", fi, err)
	}

	if err := stop(); err != nil {
		t.Errorf("run = %v", err)
	}
	for _, path := range []string{paths.Sock, paths.Token} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", filepath.Base(path), err)
		}
	}
	// The lock is free again, and its file stays: removing it would let two
	// hosts hold "the" lock on different inodes.
	l, err := acquireLock(paths.Lock)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	l.release()
}

func TestSecondHostLosesTheLock(t *testing.T) {
	cfg := shortDir(t)
	paths, _ := startHost(t, cfg, time.Hour)
	tokenBefore, _ := os.ReadFile(paths.Token)

	err := run(context.Background(), runConfig{ConfigDir: cfg, Version: "test", Idle: time.Hour})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second host = %v, want ErrAlreadyRunning", err)
	}
	// The loser must not have touched the winner's files.
	if tokenAfter, _ := os.ReadFile(paths.Token); !bytes.Equal(tokenBefore, tokenAfter) {
		t.Error("the second host replaced the token")
	}
	if _, err := os.Lstat(paths.Sock); err != nil {
		t.Errorf("the second host removed the socket: %v", err)
	}
}

func TestIdleHostExitsOnItsOwn(t *testing.T) {
	cfg := shortDir(t)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	errc := make(chan error, 1)
	go func() {
		errc <- run(context.Background(), runConfig{ConfigDir: cfg, Version: "test", Idle: 100 * time.Millisecond})
	}()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("run = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("an idle host did not exit")
	}
	paths := pathsIn(RunDir(cfg))
	for _, path := range []string{paths.Sock, paths.Token} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", filepath.Base(path), err)
		}
	}
}

// A connection that never says hello is not a client: it must not hold the
// host up (it is closed after the hello timeout anyway, which this test sets
// far beyond the idle time so only the counting can end the host).
func TestUnauthenticatedConnectionDoesNotDelayTheIdleExit(t *testing.T) {
	cfg := shortDir(t)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	ready := newReadyBuf()
	errc := make(chan error, 1)
	go func() {
		errc <- run(context.Background(), runConfig{
			ConfigDir: cfg, Version: "test", Idle: 500 * time.Millisecond, HelloTimeout: time.Hour, Ready: ready,
		})
	}()
	select {
	case <-ready.closed:
	case err := <-errc:
		t.Fatalf("host ended before it was ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("host never became ready")
	}
	conn, err := net.Dial("unix", pathsIn(RunDir(cfg)).Sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("run = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a connection that never said hello kept the host from exiting")
	}
}

func TestHostRefusesToStartWithoutPeerCredentials(t *testing.T) {
	if peerCredSupported {
		t.Skip("this platform has them")
	}
	err := run(context.Background(), runConfig{ConfigDir: shortDir(t), Idle: time.Hour})
	if err == nil {
		t.Fatal("the host started without a way to check its peers")
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		host    bool
		readyFD int
		wantErr bool
	}{
		{"a normal start", []string{"flint"}, false, 0, false},
		{"a path to open", []string{"flint", "/home/a/proj"}, false, 0, false},
		{"host mode", []string{"flint", "--agent-host"}, true, 0, false},
		{"host mode with a ready fd", []string{"flint", "--agent-host", "--ready-fd=3"}, true, 3, false},
		{"the flag must come first", []string{"flint", "x", "--agent-host"}, false, 0, false},
		{"a bad fd", []string{"flint", "--agent-host", "--ready-fd=x"}, true, 0, true},
		{"fd 1 is not ours to write", []string{"flint", "--agent-host", "--ready-fd=1"}, true, 0, true},
		{"an unknown host flag", []string{"flint", "--agent-host", "--bogus"}, true, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, fd, err := parseArgs(tt.args)
			if host != tt.host || fd != tt.readyFD || (err != nil) != tt.wantErr {
				t.Errorf("parseArgs = %v, %d, %v", host, fd, err)
			}
		})
	}
}
