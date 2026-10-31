package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"flint/internal/dap"
	"flint/internal/runconfig"
	"flint/internal/shellenv"
	"flint/internal/terminal"
	"flint/internal/testrun"
)

// eventDebug streams a debug session's adapter events ("stopped", "output", …)
// and finally "ended", numbered so the UI can restore their order.
const eventDebug = "debug:event"

// beforeTimeout bounds a configuration's "before" command run ahead of debugging.
const beforeTimeout = 10 * time.Minute

// DebugEvent is one adapter event of a session.
type DebugEvent struct {
	ID   string          `json:"id"`
	Seq  uint64          `json:"seq"` // per session, from 1
	Name string          `json:"name"`
	Body json.RawMessage `json:"body,omitempty"`
}

// Program output is merged into fewer events: a chatty program would otherwise
// cost one JSON encode and one webview call per line.
const (
	outputDelay    = 16 * time.Millisecond
	maxOutputChunk = 64 << 10
)

// debugEvents forwards adapter events to the frontend.
type debugEvents struct {
	send func(DebugEvent) // the webview event; replaced in tests

	mu      sync.Mutex
	seq     map[string]uint64
	pending map[string]*pendingOutput
}

// pendingOutput is output of one category not yet sent.
type pendingOutput struct {
	category string
	text     strings.Builder
	timer    *time.Timer
}

func newDebugEvents(app *App) *debugEvents {
	return &debugEvents{
		send:    func(ev DebugEvent) { app.emit(eventDebug, ev) },
		seq:     map[string]uint64{},
		pending: map[string]*pendingOutput{},
	}
}

func (e *debugEvents) Event(id string, ev dap.Event) {
	if ev.Name == "output" {
		var o dap.OutputEvent
		if json.Unmarshal(ev.Body, &o) == nil && o.Category != "telemetry" {
			e.output(id, o.Category, o.Output)
		}
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flushLocked(id) // output stays ahead of what followed it
	e.emitLocked(DebugEvent{ID: id, Name: ev.Name, Body: ev.Body})
}

func (e *debugEvents) Ended(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flushLocked(id)
	e.emitLocked(DebugEvent{ID: id, Name: "ended"})
	delete(e.seq, id)
}

// finish sends what is pending and forgets a session that never started.
func (e *debugEvents) finish(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flushLocked(id)
	delete(e.seq, id)
}

// output queues console text (the program's, or Flint's own such as the
// "before" command's), merged with queued text of the same category.
func (e *debugEvents) output(id, category, text string) {
	if text == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.pending[id]
	if p != nil && p.category != category {
		e.flushLocked(id)
		p = nil
	}
	if p == nil {
		p = &pendingOutput{category: category}
		e.pending[id] = p
		p.timer = time.AfterFunc(outputDelay, func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.pending[id] == p {
				e.flushLocked(id)
			}
		})
	}
	p.text.WriteString(text)
	if p.text.Len() >= maxOutputChunk {
		e.flushLocked(id)
	}
}

func (e *debugEvents) flushLocked(id string) {
	p := e.pending[id]
	if p == nil {
		return
	}
	p.timer.Stop()
	delete(e.pending, id)
	body, _ := json.Marshal(dap.OutputEvent{Category: p.category, Output: p.text.String()})
	e.emitLocked(DebugEvent{ID: id, Name: "output", Body: body})
}

// emitLocked stamps and emits under e.mu, so sequence order equals send order.
func (e *debugEvents) emitLocked(ev DebugEvent) {
	e.seq[ev.ID]++
	ev.Seq = e.seq[ev.ID]
	e.send(ev)
}

// Breakpoints are the breakpoints to set at launch, by absolute file path.
type Breakpoints map[string][]dap.SourceBreakpoint

// DebugAPI debugs Go run configurations and tests with Delve (`dlv dap`).
type DebugAPI struct {
	app *App
}

// Start debugs a configuration under id (chosen by the frontend). Returns once
// the program runs, i.e. after Delve built it — that can take a while.
func (d *DebugAPI) Start(id, root string, cfg runconfig.Config, breakpoints Breakpoints) error {
	if err := checkRoot(root); err != nil {
		return err
	}
	launch, err := runconfig.Resolve(root, cfg)
	if err != nil {
		return err
	}
	if launch.Go == nil {
		return fmt.Errorf("%s configurations can't be debugged yet (only Go)", cfg.Type)
	}
	// Steps are [before?] build run: Delve builds and runs the program itself.
	for _, step := range launch.Steps[:len(launch.Steps)-2] {
		if err := d.runBefore(id, launch, step); err != nil {
			d.app.debugEvents.finish(id)
			return err
		}
	}
	lc := dap.LaunchConfig{
		Mode:       "debug",
		Program:    launch.Go.Package,
		Args:       launch.Go.Args,
		Cwd:        launch.Cwd,
		BuildFlags: strings.Join(launch.Go.BuildFlags, " "),
	}
	if cfg.Debug != nil {
		lc.StopOnEntry = cfg.Debug.StopOnEntry
	}
	return d.start(id, launch.Env, lc, breakpoints)
}

// StartTest debugs Go tests: those named in tests (all when empty) of the
// package in dir.
func (d *DebugAPI) StartTest(id, dir string, tests []string, breakpoints Breakpoints) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("test directory %q is not absolute", dir)
	}
	var args []string
	if len(tests) > 0 {
		args = []string{"-test.run", testrun.GoRunPattern(tests)}
	}
	return d.start(id, nil, dap.LaunchConfig{Mode: "test", Program: dir, Cwd: dir, Args: args}, breakpoints)
}

func (d *DebugAPI) start(id string, env []string, lc dap.LaunchConfig, breakpoints Breakpoints) error {
	shellenv.Wait()
	err := d.app.debugs.StartGo(context.Background(), id, append(terminal.Environ(), env...), lc, breakpoints)
	if err != nil {
		d.app.debugEvents.finish(id)
	}
	return err
}

// runBefore runs a configuration's "before" command, its output streaming to
// the debug console. It runs in its own process group, killed as a whole on timeout.
func (d *DebugAPI) runBefore(id string, launch runconfig.Launch, step runconfig.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), beforeTimeout)
	defer cancel()
	d.app.debugEvents.output(id, "console", "$ "+step.Label+"\n")
	cmd := exec.CommandContext(ctx, step.Program, step.Args...)
	cmd.Dir = launch.Cwd
	cmd.Env = append(os.Environ(), launch.Env...)
	out := consoleWriter{events: d.app.debugEvents, id: id}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second // a leftover child holding the pipe must not block
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", step.Label, err)
	}
	return nil
}

// consoleWriter sends a process's output to a session's debug console.
type consoleWriter struct {
	events *debugEvents
	id     string
}

func (w consoleWriter) Write(p []byte) (int, error) {
	w.events.output(w.id, "stdout", string(p))
	return len(p), nil
}

// Stop ends a session and its program.
func (d *DebugAPI) Stop(id string) { d.app.debugs.Stop(id) }

// SetBreakpoints replaces the breakpoints of one file in a running session.
func (d *DebugAPI) SetBreakpoints(id, path string, bps []dap.SourceBreakpoint) ([]dap.Breakpoint, error) {
	return withSession(id, d, func(ctx context.Context, s *dap.Session) ([]dap.Breakpoint, error) {
		return s.SetBreakpoints(ctx, path, bps)
	})
}

// Step runs one of continue, next, stepIn, stepOut or pause on a thread.
func (d *DebugAPI) Step(id, action string, threadID int) error {
	_, err := withSession(id, d, func(ctx context.Context, s *dap.Session) (struct{}, error) {
		switch action {
		case "continue":
			return struct{}{}, s.Continue(ctx, threadID)
		case "next":
			return struct{}{}, s.Next(ctx, threadID)
		case "stepIn":
			return struct{}{}, s.StepIn(ctx, threadID)
		case "stepOut":
			return struct{}{}, s.StepOut(ctx, threadID)
		case "pause":
			return struct{}{}, s.Pause(ctx, threadID)
		}
		return struct{}{}, fmt.Errorf("unknown debug action %q", action)
	})
	return err
}

func (d *DebugAPI) Threads(id string) ([]dap.Thread, error) {
	return withSession(id, d, func(ctx context.Context, s *dap.Session) ([]dap.Thread, error) { return s.Threads(ctx) })
}

// StackTrace returns the innermost frames of a thread.
func (d *DebugAPI) StackTrace(id string, threadID int) ([]dap.StackFrame, error) {
	return withSession(id, d, func(ctx context.Context, s *dap.Session) ([]dap.StackFrame, error) {
		return s.StackTrace(ctx, threadID, 50)
	})
}

func (d *DebugAPI) Scopes(id string, frameID int) ([]dap.Scope, error) {
	return withSession(id, d, func(ctx context.Context, s *dap.Session) ([]dap.Scope, error) { return s.Scopes(ctx, frameID) })
}

func (d *DebugAPI) Variables(id string, ref int) ([]dap.Variable, error) {
	return withSession(id, d, func(ctx context.Context, s *dap.Session) ([]dap.Variable, error) { return s.Variables(ctx, ref) })
}

// Evaluate evaluates an expression in a frame (0: no frame).
func (d *DebugAPI) Evaluate(id, expr string, frameID int) (dap.EvaluateResult, error) {
	return withSession(id, d, func(ctx context.Context, s *dap.Session) (dap.EvaluateResult, error) {
		return s.Evaluate(ctx, expr, frameID)
	})
}

// requestTimeout bounds one request; Delve answers in milliseconds unless hung.
const requestTimeout = 30 * time.Second

func withSession[T any](id string, d *DebugAPI, fn func(context.Context, *dap.Session) (T, error)) (T, error) {
	var zero T
	s, err := d.app.debugs.Get(id)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	out, err := fn(ctx, s)
	if errors.Is(err, dap.ErrClosed) {
		return zero, dap.ErrNoSession
	}
	return out, err
}
