package hostmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"flint/internal/agenthost/client"
)

const (
	dialTimeout = 5 * time.Second
	// spawnTimeout bounds how long Spawn waits for a new host to say ready.
	spawnTimeout = 20 * time.Second
	// exitWait bounds the wait for a host that closed the readiness pipe
	// without saying ready to finish exiting.
	exitWait = 5 * time.Second
)

// spawned is the pid of the host this process started last (Spawned).
var spawned atomic.Int64

// Spawned is the pid of the background host this process started (0 when
// it started none): the command line recognises the host by it, never by
// a command line a process can make up (clisock.Facts.HostPID).
func Spawned() int { return int(spawned.Load()) }

// Dial connects to the running host of configDir and performs hello. cfg
// carries the version and the handlers; Dial fills in the token. A host that
// is not there is ErrNotRunning (wrapped), which is the cue to Spawn one. A
// protocol mismatch returns the client with a *client.ProtocolError, as
// client.New does.
func Dial(ctx context.Context, configDir string, cfg client.Config) (*client.Client, error) {
	s, err := dialSocket(ctx, configDir)
	if err != nil {
		return nil, err
	}
	cfg.Token = s.token
	return client.New(s.conn, cfg)
}

// hostSocket is a connection to the host's socket that has not said hello,
// with the token that hello needs.
type hostSocket struct {
	conn  net.Conn
	token string
}

// dialSocket connects to the host's socket without saying hello. The token is
// read after the run directory was checked.
func dialSocket(ctx context.Context, configDir string) (hostSocket, error) {
	paths := pathsIn(RunDir(configDir))
	fi, err := checkRunDir(paths.Dir)
	if err != nil {
		return hostSocket{}, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return hostSocket{}, fmt.Errorf("%s is accessible to others (mode %v)", paths.Dir, fi.Mode().Perm())
	}
	token, err := readToken(paths.Token)
	if err != nil {
		return hostSocket{}, err
	}
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "unix", paths.Sock)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			// A socket nobody listens on: a host that died, or one still starting.
			return hostSocket{}, fmt.Errorf("%w (%v)", ErrNotRunning, err)
		}
		return hostSocket{}, fmt.Errorf("connect to the host: %w", err)
	}
	return hostSocket{conn, token}, nil
}

// Spawn starts a host for configDir as a detached process of its own (a new
// session, stdio on /dev/null, so it outlives the caller and its terminal),
// waits until it says it listens and connects. The host tells readiness
// through a pipe, so there is no polling; the wait is bounded by
// spawnTimeout and ctx. When another host won the race for the lock, Spawn
// connects to that one.
func Spawn(ctx context.Context, configDir string, cfg client.Config) (*client.Client, error) {
	return spawnThen(ctx, configDir, func(ctx context.Context) (*client.Client, error) {
		return Dial(ctx, configDir, cfg)
	})
}

// spawnSocket is Spawn for a caller that wants the raw connection (the
// --stdio relay) instead of a client.
func spawnSocket(ctx context.Context, configDir string) (hostSocket, error) {
	return spawnThen(ctx, configDir, func(ctx context.Context) (hostSocket, error) {
		return dialSocket(ctx, configDir)
	})
}

// spawnThen starts a host and, once it listens (or another host won the
// lock), calls connect.
func spawnThen[T any](ctx context.Context, configDir string, connect func(context.Context) (T, error)) (T, error) {
	var zero T
	exe, err := os.Executable()
	if err != nil {
		return zero, fmt.Errorf("find the Flint executable: %w", err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		return zero, err
	}
	defer r.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		_ = w.Close()
		return zero, err
	}
	defer null.Close()

	// ExtraFiles[0] is descriptor 3 in the child.
	cmd := exec.Command(exe, agentHostFlag, readyFDFlag+"3")
	cmd.Env = append(os.Environ(), "FLINT_CONFIG_DIR="+configDir) // the host must serve the same directory we looked at
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.ExtraFiles = []*os.File{w}
	detach(cmd)
	err = cmd.Start()
	_ = w.Close() // the child's copy is the only writer now: EOF means it ended
	if err != nil {
		return zero, fmt.Errorf("start the host: %w", err)
	}
	spawned.Store(int64(cmd.Process.Pid))
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }() // reaps it, whenever it ends

	ctx, cancel := context.WithTimeout(ctx, spawnTimeout)
	defer cancel()
	said := make(chan error, 1)
	go func() {
		buf := make([]byte, len("ready\n"))
		_, err := io.ReadFull(r, buf)
		if err == nil && string(buf) != "ready\n" {
			err = errors.New("unexpected readiness message")
		}
		said <- err
	}()

	select {
	case err := <-said:
		if err == nil {
			return connect(ctx)
		}
		return afterNoReady(ctx, connect, exited)
	case <-ctx.Done():
		_ = cmd.Process.Kill() // a host that never got ready is of no use
		return zero, fmt.Errorf("the host did not become ready: %w", ctx.Err())
	}
}

// afterNoReady explains a host that ended without saying ready. Losing the
// lock means another host serves the directory: connect to it.
func afterNoReady[T any](ctx context.Context, connect func(context.Context) (T, error), exited <-chan error) (T, error) {
	var zero T
	var werr error
	select {
	case werr = <-exited:
	case <-time.After(exitWait):
		return zero, errors.New("the host closed its readiness pipe but did not exit")
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) && ee.ExitCode() == ExitAlreadyRunning {
		c, err := connect(ctx)
		if err != nil {
			// The winner took the lock but does not listen yet; waiting for it
			// would mean polling, so the caller is told to try again.
			return zero, fmt.Errorf("%w: another host holds the lock but is not listening yet (%v)", ErrNotRunning, err)
		}
		return c, nil
	}
	return zero, fmt.Errorf("the host exited before it was ready: %v", werr)
}
