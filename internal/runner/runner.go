// Package runner starts, stops and restarts the processes of run configurations
// and test runs. A run is a sequence of steps (e.g. build, then the program); a
// failing step stops the rest. Output is streamed as events, in a PTY (colors,
// interactive input) or through pipes (when a parser reads the output, e.g.
// `go test -json`). Each step runs in its own process group, so stopping also
// ends the children (`go run`, `npm run` and make spawn the real program).
package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"flint/internal/runconfig"
	"flint/internal/terminal"
)

// How long Stop waits after SIGINT before SIGKILL, and after SIGKILL at most.
const (
	interruptGrace = 3 * time.Second
	killGrace      = 2 * time.Second
	// ioGrace bounds waiting for output after a step exited while a background
	// child still holds the PTY or a pipe open.
	ioGrace = 500 * time.Millisecond
	// maxLine is flushed through the filter even without a newline.
	maxLine = 1 << 20
)

// ANSI dim/reset, used for the command line header and the exit footer.
const (
	dim   = "\x1b[2m"
	reset = "\x1b[0m"
)

// Events receives run output and the final result.
type Events interface {
	// Output delivers console bytes (CRLF line ends). Calls for one run never
	// overlap and Exited comes after the last one.
	Output(id string, data []byte)
	Exited(id string, res Result)
}

// Result is how a run ended.
type Result struct {
	// Code is the exit code of the last step that ran (-1: killed by a signal or not started).
	Code   int    `json:"code"`
	Signal string `json:"signal,omitempty"`
	// Stopped: the user stopped the run.
	Stopped   bool  `json:"stopped"`
	ElapsedMs int64 `json:"elapsedMs"`
	// Error is set when a step could not be started.
	Error string `json:"error,omitempty"`
}

// Spec describes a run.
type Spec struct {
	Steps []runconfig.Command
	Cwd   string
	// Env holds KEY=VALUE pairs added to the inherited environment.
	Env []string
	// PTY runs steps in a pseudo-terminal; otherwise stdout/stderr are pipes and
	// stdin is empty.
	PTY  bool
	Cols int
	Rows int
	// Filter (pipe mode, last step only) sees each stdout line, newline included,
	// and returns what the console shows. Returning a non-nil empty slice shows
	// nothing but still produces an Output call, so the caller can flush state it
	// collected (parsed test results) promptly.
	Filter func(line []byte) []byte
}

type run struct {
	spec Spec
	done chan struct{}

	mu      sync.Mutex
	cmd     *exec.Cmd // current step, nil between steps
	pty     *os.File
	stopped bool
}

// Manager owns the running runs.
type Manager struct {
	events Events

	mu   sync.Mutex
	runs map[string]*run
}

// NewManager creates a manager reporting through events.
func NewManager(events Events) *Manager {
	return &Manager{events: events, runs: map[string]*run{}}
}

// Start begins a run under id (chosen by the caller, unique while it runs).
func (m *Manager) Start(id string, spec Spec) error {
	if len(spec.Steps) == 0 {
		return errors.New("nothing to run")
	}
	r := &run{spec: spec, done: make(chan struct{})}
	m.mu.Lock()
	if _, exists := m.runs[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("run %q already running", id)
	}
	m.runs[id] = r
	m.mu.Unlock()
	go m.execute(id, r)
	return nil
}

// Running reports whether id is still running.
func (m *Manager) Running(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[id]
	return ok
}

// Stop interrupts the run (SIGINT to its process group, SIGKILL after a grace
// period) and returns once it has ended. Unknown ids are ignored.
func (m *Manager) Stop(id string) {
	r := m.get(id)
	if r == nil {
		return
	}
	r.signal(syscall.SIGINT)
	select {
	case <-r.done:
		return
	case <-time.After(interruptGrace):
	}
	r.signal(syscall.SIGKILL)
	select {
	case <-r.done:
	case <-time.After(killGrace):
	}
}

// Write sends input to a PTY run.
func (m *Manager) Write(id string, data []byte) error {
	r := m.get(id)
	if r == nil {
		return fmt.Errorf("run %q not running", id)
	}
	r.mu.Lock()
	f := r.pty
	r.mu.Unlock()
	if f == nil {
		return nil // pipe mode or between steps: no input
	}
	_, err := f.Write(data)
	return err
}

// Resize updates the PTY size of a run.
func (m *Manager) Resize(id string, cols, rows int) error {
	r := m.get(id)
	if r == nil {
		return fmt.Errorf("run %q not running", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spec.Cols, r.spec.Rows = cols, rows
	if r.pty == nil {
		return nil
	}
	return pty.Setsize(r.pty, winsize(cols, rows))
}

// CloseAll kills every run immediately (app shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*run, 0, len(m.runs))
	for _, r := range m.runs {
		all = append(all, r)
	}
	m.mu.Unlock()
	for _, r := range all {
		r.signal(syscall.SIGKILL)
	}
}

func (m *Manager) get(id string) *run {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[id]
}

// signal marks the run stopped and signals the current step's process group.
func (r *run) signal(sig syscall.Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	if r.cmd != nil && r.cmd.Process != nil {
		_ = syscall.Kill(-r.cmd.Process.Pid, sig)
	}
}

// execute runs the steps in order and reports the result.
func (m *Manager) execute(id string, r *run) {
	started := time.Now()
	chunks := make(chan []byte, 256)
	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		terminal.Coalesce(chunks, func(data []byte) { m.events.Output(id, data) })
	}()
	out := func(s string) { chunks <- []byte(s) }

	res := Result{}
	for i, step := range r.spec.Steps {
		if r.isStopped() {
			break
		}
		out(dim + "$ " + step.Label + reset + "\r\n")
		last := i == len(r.spec.Steps)-1
		res = r.runStep(step, chunks, last)
		if res.Error != "" {
			out("\x1b[31m" + res.Error + reset + "\r\n")
		}
		if res.Code != 0 || res.Error != "" {
			break
		}
	}
	res.Stopped = r.isStopped()
	res.ElapsedMs = time.Since(started).Milliseconds()
	out(footer(res))
	close(chunks)
	<-pumped

	m.mu.Lock()
	if m.runs[id] == r {
		delete(m.runs, id)
	}
	m.mu.Unlock()
	close(r.done)
	m.events.Exited(id, res)
}

func (r *run) isStopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

func footer(res Result) string {
	switch {
	case res.Stopped:
		return "\r\n" + dim + "Process stopped" + reset + "\r\n"
	case res.Signal != "":
		return "\r\n" + dim + "Process killed by " + res.Signal + reset + "\r\n"
	case res.Error != "":
		return ""
	default:
		return fmt.Sprintf("\r\n%sProcess finished with exit code %d%s\r\n", dim, res.Code, reset)
	}
}

// runStep starts one process and waits for it and its output.
func (r *run) runStep(step runconfig.Command, chunks chan<- []byte, last bool) Result {
	cmd := exec.Command(step.Program, step.Args...)
	cmd.Dir = r.spec.Cwd
	cmd.Env = append(terminal.Environ(), r.spec.Env...)

	var (
		ptmx     *os.File
		lines    *lineWriter
		readDone = make(chan struct{})
		err      error
	)
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return Result{}
	}
	if r.spec.PTY {
		ptmx, err = pty.StartWithSize(cmd, winsize(r.spec.Cols, r.spec.Rows)) // own session = own process group
		if err == nil {
			go func() {
				defer close(readDone)
				copyChunks(ptmx, chunks)
			}()
		}
	} else {
		raw := &chunkWriter{chunks: chunks}
		cmd.Stdout, cmd.Stderr = raw, raw
		if last && r.spec.Filter != nil {
			lines = &lineWriter{filter: r.spec.Filter, chunks: chunks}
			cmd.Stdout = lines
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.WaitDelay = ioGrace
		err = cmd.Start()
		close(readDone)
	}
	if err != nil {
		r.mu.Unlock()
		return Result{Code: -1, Error: fmt.Sprintf("cannot start %s: %v", step.Program, err)}
	}
	r.cmd, r.pty = cmd, ptmx
	r.mu.Unlock()

	waitErr := cmd.Wait()
	if ptmx != nil {
		select { // drain what the program printed last, unless a child keeps the PTY open
		case <-readDone:
		case <-time.After(ioGrace):
		}
		_ = ptmx.Close()
		<-readDone
	}
	if lines != nil {
		lines.flush()
	}
	r.mu.Lock()
	r.cmd, r.pty = nil, nil
	r.mu.Unlock()
	return exitResult(waitErr)
}

func exitResult(err error) Result {
	if err == nil {
		return Result{}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res := Result{Code: exitErr.ExitCode()}
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			res.Code = -1
			res.Signal = ws.Signal().String()
		}
		return res
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return Result{} // exited fine; a background child kept the output open
	}
	return Result{Code: -1, Error: err.Error()}
}

// copyChunks forwards PTY output until the PTY is closed.
func copyChunks(src io.Reader, chunks chan<- []byte) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			chunks <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

// chunkWriter forwards pipe output, turning \n into \r\n for the terminal
// emulator (a PTY would do that itself). Safe for concurrent writers.
type chunkWriter struct {
	chunks chan<- []byte
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.chunks <- toCRLF(p)
	}
	return len(p), nil
}

// lineWriter passes complete stdout lines through a filter.
type lineWriter struct {
	filter  func([]byte) []byte
	chunks  chan<- []byte
	partial []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if bytes.IndexByte(p, '\n') < 0 {
		// No line ends here: grow the partial line in place, so a long line
		// arriving in pieces isn't recopied on every write.
		w.partial = append(w.partial, p...)
		if len(w.partial) > maxLine {
			w.flush()
		}
		return len(p), nil
	}
	data := append(w.partial, p...)
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		w.emit(data[:nl+1])
		data = data[nl+1:]
	}
	w.partial = append([]byte(nil), data...)
	if len(w.partial) > maxLine {
		w.flush()
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if len(w.partial) > 0 {
		w.emit(w.partial)
		w.partial = nil
	}
}

func (w *lineWriter) emit(line []byte) {
	shown := w.filter(line)
	if shown != nil {
		w.chunks <- toCRLF(shown)
	}
}

// toCRLF returns a copy of p with bare \n replaced by \r\n.
func toCRLF(p []byte) []byte {
	// Room for p; the few added \r grow it through append (no size arithmetic to overflow).
	out := make([]byte, 0, len(p))
	for i, b := range p {
		if b == '\n' && (i == 0 || p[i-1] != '\r') {
			out = append(out, '\r')
		}
		out = append(out, b)
	}
	return out
}

func winsize(cols, rows int) *pty.Winsize {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	return &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
}
