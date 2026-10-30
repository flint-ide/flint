package dap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	startTimeout = 30 * time.Second // dlv builds the program during launch
	stopTimeout  = 3 * time.Second
)

// Session is one debug session: a running adapter and the program it debugs.
// Events reach the callback given to StartDelve one at a time, in order, from
// the read loop — the callback must not block or make requests itself.
type Session struct {
	cmd  *exec.Cmd
	conn *conn
	net  net.Conn

	initialized chan struct{}
	initOnce    sync.Once

	stderrMu sync.Mutex
	stderr   bytes.Buffer

	exited chan struct{} // closed when the adapter process ends

	stopOnce sync.Once
	stopped  chan struct{} // closed when Stop finished
}

// StartDelve debugs cfg with `dlv dap` from dlvPath (env is the environment for
// both dlv and the program). Breakpoints are set before the program runs.
func StartDelve(ctx context.Context, dlvPath string, env []string, cfg LaunchConfig, breakpoints map[string][]SourceBreakpoint, onEvent func(Event)) (*Session, error) {
	// dlv dials us (--client-addr) instead of us parsing its "listening at" line.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for dlv: %w", err)
	}
	defer ln.Close()

	s := &Session{initialized: make(chan struct{}), exited: make(chan struct{}), stopped: make(chan struct{})}
	s.cmd = exec.Command(dlvPath, "dap", "--client-addr="+ln.Addr().String())
	s.cmd.Dir = cfg.Cwd
	s.cmd.Env = env
	s.cmd.Stderr = (*tailWriter)(s)
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // the debuggee dies with dlv
	s.cmd.WaitDelay = time.Second                           // a child holding stderr must not keep Wait (and exited) open
	if err := s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start dlv: %w", err)
	}
	go func() {
		_ = s.cmd.Wait()
		close(s.exited)
	}()

	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	nc, err := accept(ctx, ln, s.exited)
	if err != nil {
		s.kill()
		return nil, fmt.Errorf("dlv did not connect: %w%s", err, s.stderrTail())
	}
	s.net = nc
	if cfg.OutputMode == "" {
		cfg.OutputMode = "remote" // program output as "output" events, not dlv's stdout
	}
	if cfg.Output == "" && cfg.Mode != "exec" {
		// Delve builds into the working directory by default, which litters the
		// project (and its file tree) with __debug_bin files.
		dir := filepath.Join(os.TempDir(), "flint-debug")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			cfg.Output = filepath.Join(dir, fmt.Sprintf("__debug_bin%d", time.Now().UnixNano()))
		}
	}
	var launched atomic.Bool
	s.conn = newConn(nc, nc, func(name string, body json.RawMessage) {
		switch name {
		case "initialized":
			s.initOnce.Do(func() { close(s.initialized) })
		case "terminated":
			// Delve stays up after the program ends until it is told to disconnect;
			// end the session so dlv, its connection and the debug binary go away.
			go s.Stop()
		case "output":
			// Build errors arrive as console output before launch fails; keep
			// them for the error message.
			var o OutputEvent
			if !launched.Load() && json.Unmarshal(body, &o) == nil && o.Category != "stdout" {
				_, _ = (*tailWriter)(s).Write([]byte(o.Output))
			}
		}
		onEvent(Event{Name: name, Body: body})
	})
	err = s.launch(ctx, cfg, breakpoints)
	launched.Store(true)
	if err != nil {
		s.kill()
		return nil, fmt.Errorf("%w%s", err, s.stderrTail())
	}
	return s, nil
}

func accept(ctx context.Context, ln net.Listener, exited <-chan struct{}) (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		ch <- result{c, err}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-exited:
		return nil, errors.New("dlv exited")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// launch runs the DAP handshake: initialize → launch → (initialized event) →
// breakpoints → configurationDone.
func (s *Session) launch(ctx context.Context, cfg LaunchConfig, breakpoints map[string][]SourceBreakpoint) error {
	init := map[string]any{
		"clientID":                     "flint",
		"clientName":                   "Flint",
		"adapterID":                    "go",
		"linesStartAt1":                true,
		"columnsStartAt1":              true,
		"pathFormat":                   "path",
		"supportsVariableType":         true,
		"supportsRunInTerminalRequest": false,
	}
	if err := s.conn.Request(ctx, "initialize", init, nil); err != nil {
		return err
	}
	if err := s.conn.Request(ctx, "launch", cfg, nil); err != nil {
		return err
	}
	select {
	case <-s.initialized:
	case <-ctx.Done():
		return fmt.Errorf("waiting for initialized: %w", ctx.Err())
	case <-s.conn.Done():
		return ErrClosed
	}
	for path, bps := range breakpoints {
		if _, err := s.SetBreakpoints(ctx, path, bps); err != nil {
			return err
		}
	}
	return s.conn.Request(ctx, "configurationDone", nil, nil)
}

// SetBreakpoints replaces all breakpoints in path; the result matches bps by index.
func (s *Session) SetBreakpoints(ctx context.Context, path string, bps []SourceBreakpoint) ([]Breakpoint, error) {
	if bps == nil {
		bps = []SourceBreakpoint{}
	}
	var out struct {
		Breakpoints []Breakpoint `json:"breakpoints"`
	}
	err := s.conn.Request(ctx, "setBreakpoints", map[string]any{
		"source":      Source{Path: path},
		"breakpoints": bps,
	}, &out)
	return out.Breakpoints, err
}

func (s *Session) Continue(ctx context.Context, threadID int) error {
	return s.conn.Request(ctx, "continue", map[string]any{"threadId": threadID}, nil)
}

func (s *Session) Next(ctx context.Context, threadID int) error {
	return s.conn.Request(ctx, "next", map[string]any{"threadId": threadID}, nil)
}

func (s *Session) StepIn(ctx context.Context, threadID int) error {
	return s.conn.Request(ctx, "stepIn", map[string]any{"threadId": threadID}, nil)
}

func (s *Session) StepOut(ctx context.Context, threadID int) error {
	return s.conn.Request(ctx, "stepOut", map[string]any{"threadId": threadID}, nil)
}

func (s *Session) Pause(ctx context.Context, threadID int) error {
	return s.conn.Request(ctx, "pause", map[string]any{"threadId": threadID}, nil)
}

func (s *Session) Threads(ctx context.Context) ([]Thread, error) {
	var out struct {
		Threads []Thread `json:"threads"`
	}
	err := s.conn.Request(ctx, "threads", nil, &out)
	return out.Threads, err
}

// StackTrace returns up to levels frames of threadID, innermost first.
func (s *Session) StackTrace(ctx context.Context, threadID, levels int) ([]StackFrame, error) {
	var out struct {
		StackFrames []StackFrame `json:"stackFrames"`
	}
	err := s.conn.Request(ctx, "stackTrace", map[string]any{"threadId": threadID, "startFrame": 0, "levels": levels}, &out)
	return out.StackFrames, err
}

func (s *Session) Scopes(ctx context.Context, frameID int) ([]Scope, error) {
	var out struct {
		Scopes []Scope `json:"scopes"`
	}
	err := s.conn.Request(ctx, "scopes", map[string]any{"frameId": frameID}, &out)
	return out.Scopes, err
}

func (s *Session) Variables(ctx context.Context, ref int) ([]Variable, error) {
	var out struct {
		Variables []Variable `json:"variables"`
	}
	err := s.conn.Request(ctx, "variables", map[string]any{"variablesReference": ref}, &out)
	return out.Variables, err
}

// Evaluate evaluates expr in frameID ("repl" context: also accepts dlv commands).
func (s *Session) Evaluate(ctx context.Context, expr string, frameID int) (EvaluateResult, error) {
	var out EvaluateResult
	args := map[string]any{"expression": expr, "context": "repl"}
	if frameID > 0 {
		args["frameId"] = frameID
	}
	err := s.conn.Request(ctx, "evaluate", args, &out)
	return out, err
}

// Stop ends the session and the debugged program; it never blocks for long.
// Safe to call more than once.
func (s *Session) Stop() {
	s.stopOnce.Do(s.stop)
	<-s.stopped
}

func (s *Session) stop() {
	defer close(s.stopped)
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	_ = s.conn.Request(ctx, "disconnect", map[string]any{"terminateDebuggee": true}, nil)
	select {
	case <-s.exited:
	case <-ctx.Done():
	}
	s.kill()
}

// Done is closed when the adapter process has exited.
func (s *Session) Done() <-chan struct{} { return s.exited }

func (s *Session) kill() {
	if s.net != nil {
		_ = s.net.Close()
	}
	select {
	case <-s.exited:
		return // reaped: its process group id may already belong to someone else
	default:
	}
	if s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// tailWriter keeps the last few KB of the adapter's stderr for error messages.
type tailWriter Session

func (t *tailWriter) Write(p []byte) (int, error) {
	s := (*Session)(t)
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	s.stderr.Write(p)
	if over := s.stderr.Len() - 4096; over > 0 {
		s.stderr.Next(over)
	}
	return len(p), nil
}

func (s *Session) stderrTail() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	if out := bytes.TrimSpace(s.stderr.Bytes()); len(out) > 0 {
		return ": " + string(out)
	}
	return ""
}
