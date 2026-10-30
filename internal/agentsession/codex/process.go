package codex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// pipeWaitDelay bounds how long Wait waits for stderr to close after the
	// server exits (a grandchild may keep it open).
	pipeWaitDelay = 2 * time.Second
	// drainGrace is how long buffered stdout may drain after the server exits.
	drainGrace = time.Second
	// exitGrace is how long a server whose stdout closed may live on.
	exitGrace = 2 * time.Second
	// killGrace is how long Close waits after SIGTERM before SIGKILL.
	killGrace = 3 * time.Second
	// stderrInError is how much stderr tail is quoted in ExitError.
	stderrInError = 4 << 10
)

// ErrClosed is returned by calls made on, or pending during, Stop/Close.
var ErrClosed = errors.New("codex: session closed")

// ExitError reports that the app-server ended (or its stdout closed) while
// the session was in use.
type ExitError struct {
	// Err is the process wait error (nil for a clean exit).
	Err error
	// Stderr is the tail of the server's stderr.
	Stderr string
}

// Error implements error.
func (e *ExitError) Error() string {
	msg := "codex: app-server exited"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += "; stderr: " + s
	}
	return msg
}

// Unwrap exposes the wait error.
func (e *ExitError) Unwrap() error { return e.Err }

// process is one running `codex app-server` with its JSON-RPC connection.
type process struct {
	cmd    *exec.Cmd
	stdin  io.Closer
	stdout *os.File
	conn   *conn
	stderr *ringBuffer
	onExit func(error)

	closing   atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	err       error // written once before done is closed
}

type spawnSpec struct {
	path string
	args []string
	dir  string
	env  []string
}

// startProcess spawns the server in its own process group and starts the
// connection. onExit runs once, after every message has been dispatched.
func startProcess(spec spawnSpec, onRequest requestFunc, onNotify notificationFunc, onExit func(error)) (*process, error) {
	cmd := exec.Command(spec.path, spec.args...)
	cmd.Dir = spec.dir
	cmd.Env = spec.env
	cmd.WaitDelay = pipeWaitDelay
	isolateProcessTree(cmd)

	stderr := newRingBuffer(stderrCapacity)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}
	// A plain os.Pipe (not StdoutPipe) so Wait never closes the read end
	// under the reader: buffered output is drained after the server exits.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	if err := cmd.Start(); err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("codex: start %s: %w", spec.path, err)
	}
	_ = stdoutW.Close()

	p := &process{cmd: cmd, stdin: stdin, stdout: stdoutR, stderr: stderr, onExit: onExit, done: make(chan struct{})}
	p.conn = newConn(stdin, onRequest, onNotify)
	p.conn.start(stdoutR)
	go p.supervise()
	return p, nil
}

// supervise waits for the process to exit (or its stdout to end), drains the
// connection, fails pending calls, runs onExit and closes done.
func (p *process) supervise() {
	exited := make(chan error, 1)
	go func() { exited <- p.cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-exited:
		select {
		case <-p.conn.Drained():
		case <-time.After(drainGrace):
		}
	case <-p.conn.Drained():
		waitErr = p.stopProcess(exited)
	}
	_ = p.stdout.Close()
	<-p.conn.Drained()

	p.err = p.finalError(waitErr)
	p.conn.shutdown(p.err)
	if p.onExit != nil {
		p.onExit(p.err)
	}
	close(p.done)
}

func (p *process) stopProcess(exited <-chan error) error {
	_ = terminateTree(p.cmd.Process)
	select {
	case err := <-exited:
		return err
	case <-time.After(exitGrace):
		_ = killTree(p.cmd.Process)
		return <-exited
	}
}

func (p *process) finalError(waitErr error) error {
	if p.closing.Load() {
		return ErrClosed
	}
	exitErr := &ExitError{Err: waitErr, Stderr: p.stderr.Tail(stderrInError)}
	if readErr := p.conn.ReadErr(); readErr != nil {
		return fmt.Errorf("codex: read app-server output: %w; %w", readErr, exitErr)
	}
	return exitErr
}

// Close terminates the process tree (SIGTERM, then SIGKILL) and waits until
// every goroutine has exited. Safe to call more than once.
func (p *process) Close() {
	p.closeOnce.Do(func() {
		p.closing.Store(true)
		p.conn.shutdown(ErrClosed)
		_ = p.stdin.Close()
		_ = terminateTree(p.cmd.Process)
		select {
		case <-p.done:
		case <-time.After(killGrace):
			_ = killTree(p.cmd.Process)
		}
	})
	<-p.done
}

// buildEnv returns Flint's environment with extra appended (later entries win).
func buildEnv(extra []string) []string {
	return slices.Concat(os.Environ(), extra)
}

func lookupEnv(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], prefix); ok {
			return v
		}
	}
	return ""
}

// resolveCommand finds name using the PATH of the server's environment.
// Relative PATH entries are ignored, mirroring os/exec's ErrDot protection.
func resolveCommand(name string, env []string) (string, error) {
	if name == "" {
		return "", errors.New("codex: empty binary path")
	}
	if strings.ContainsAny(name, `/\`) || runtime.GOOS == "windows" {
		return lookPath(name)
	}
	for _, dir := range filepath.SplitList(lookupEnv(env, "PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return lookPath(name)
}

func lookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("codex: find %q: %w", name, err)
	}
	return path, nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
