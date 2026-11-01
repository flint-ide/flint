package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"flint/internal/agentsession"
)

const (
	// stderrInError is how much stderr tail is quoted in errors.
	stderrInError = 4 << 10
	// pipeWaitDelay bounds how long Wait waits for stderr to close after the
	// CLI exits (a grandchild may keep it open).
	pipeWaitDelay = 2 * time.Second
	// drainGrace is how long buffered stdout may drain after the CLI exits.
	drainGrace = time.Second
	// exitGrace is how long a CLI whose stdout closed may live on before it
	// is killed.
	exitGrace = 2 * time.Second
	// killGrace is how long shutdown waits after SIGTERM before SIGKILL.
	killGrace = 3 * time.Second
)

// Errors returned by the driver.
var (
	ErrClosed         = errors.New("claude: driver closed")
	ErrUnknownSession = errors.New("claude: unknown session")
	ErrSessionStopped = errors.New("claude: session stopped")
	ErrTurnInProgress = errors.New("claude: a turn is already in progress")
	ErrUnknownRequest = errors.New("claude: unknown permission request")
)

// ExitError reports that the claude process ended (or its stdout broke)
// while the session was in use.
type ExitError struct {
	// Err is the wait error (*exec.ExitError for a non-zero exit), or a
	// stdout read error; nil for a clean exit.
	Err error
	// Stderr is the tail of the process's stderr.
	Stderr string
}

// Error implements error.
func (e *ExitError) Error() string {
	msg := "claude: process exited"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += "; stderr: " + s
	}
	return msg
}

// Unwrap exposes the underlying error.
func (e *ExitError) Unwrap() error { return e.Err }

// turn is one Prompt in flight.
type turn struct {
	done      chan struct{} // closed once KindTurnEnd has been delivered
	stop      string
	err       error
	cancelled bool
}

// pendingPerm is a can_use_tool request awaiting Respond.
type pendingPerm struct {
	toolUseID   string
	input       json.RawMessage
	suggestions []json.RawMessage
}

// session is one long-lived `claude -p` process.
type session struct {
	id, cwd string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *os.File
	stderr  *ringBuffer
	events  *eventQueue

	wmu sync.Mutex // serialises stdin writes

	mu       sync.Mutex
	model    string
	turn     *turn
	perms    map[string]*pendingPerm // by control request_id
	ctlWait  map[string]chan controlResponseBody
	nextReq  int
	stopping bool
	dead     error // set once the process is gone
	readErr  error

	tr translator // owned by the reader goroutine

	readerDone chan struct{}
	done       chan struct{} // closed when the process and all goroutines ended
	stopOnce   sync.Once

	// flintMCP: the session was given Flint's own MCP server, so calls of
	// its tools are classified as Flint's (flintTool). Set before the
	// goroutines start.
	flintMCP bool
}

func spawn(path string, args []string, cwd string, env []string, id string, flintMCP bool, sink agentsession.Sink) (*session, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.WaitDelay = pipeWaitDelay
	isolateProcessTree(cmd)

	stderr := newRingBuffer(stderrCapacity)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude: stdin pipe: %w", err)
	}
	// A plain os.Pipe (not StdoutPipe) so Wait never closes the read end
	// under the reader: buffered output is drained after the process exits.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("claude: stdout pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	if err := cmd.Start(); err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("claude: start %s: %w", path, err)
	}
	_ = stdoutW.Close()

	s := &session{
		id:         id,
		cwd:        cwd,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdoutR,
		stderr:     stderr,
		events:     newEventQueue(sink),
		perms:      map[string]*pendingPerm{},
		ctlWait:    map[string]chan controlResponseBody{},
		tr:         newTranslator(slices.Contains(args, "--resume")),
		readerDone: make(chan struct{}),
		done:       make(chan struct{}),
		flintMCP:   flintMCP,
	}
	go s.readLoop()
	go s.supervise()
	return s, nil
}

// emit queues an event for the sink.
func (s *session) emit(ev agentsession.Event) {
	ev.SessionID = s.id
	s.events.push(ev, nil)
}

// write sends one message as a single stdin line.
func (s *session) write(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil { // Encode appends the newline
		return fmt.Errorf("claude: encode message: %w", err)
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.stdin.Write(buf.Bytes()); err != nil {
		if dead := s.deadErr(); dead != nil {
			return dead
		}
		return fmt.Errorf("claude: write: %w", err)
	}
	return nil
}

func (s *session) deadErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dead
}

func (s *session) currentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

// nextRequestID allocates a host control request id.
func (s *session) nextRequestID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextReq++
	return fmt.Sprintf("flint-%d", s.nextReq)
}

// control sends a host control_request and waits for its control_response.
func (s *session) control(ctx context.Context, subtype string) (json.RawMessage, error) {
	id := s.nextRequestID()
	ch := make(chan controlResponseBody, 1)
	s.mu.Lock()
	if s.dead != nil {
		err := s.dead
		s.mu.Unlock()
		return nil, err
	}
	s.ctlWait[id] = ch
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.ctlWait, id)
		s.mu.Unlock()
	}
	if err := s.write(outControlRequest{Type: msgControlReq, RequestID: id, Request: subtypeOnly{Subtype: subtype}}); err != nil {
		forget()
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Subtype != subtypeSuccess {
			return nil, fmt.Errorf("claude: %s: %s", subtype, firstNonEmpty(r.Error, "request failed"))
		}
		return r.Response, nil
	case <-ctx.Done():
		forget()
		return nil, fmt.Errorf("claude: %s: %w", subtype, ctx.Err())
	case <-s.done:
		return nil, s.deadErr()
	}
}

// sendControl sends a host control_request without waiting for the answer.
func (s *session) sendControl(subtype string) error {
	return s.write(outControlRequest{Type: msgControlReq, RequestID: s.nextRequestID(), Request: subtypeOnly{Subtype: subtype}})
}

// readLoop decodes stdout until EOF or a read error.
func (s *session) readLoop() {
	defer close(s.readerDone)
	lr := newLineReader(s.stdout, maxMessageBytes)
	for {
		line, err := lr.next()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				s.mu.Lock()
				s.readErr = err
				s.mu.Unlock()
			}
			return
		}
		s.handleLine(line)
	}
}

// supervise waits for the process to exit (or its stdout to end), drains
// stdout and finishes the session.
func (s *session) supervise() {
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-exited:
		select {
		case <-s.readerDone:
		case <-time.After(drainGrace):
		}
	case <-s.readerDone:
		waitErr = s.stopProcess(exited)
	}
	_ = s.stdout.Close() // unblocks the reader if a grandchild holds stdout
	<-s.readerDone
	s.finish(waitErr)
}

// stopProcess terminates the process tree, escalating to SIGKILL.
func (s *session) stopProcess(exited <-chan error) error {
	_ = terminateTree(s.cmd.Process)
	select {
	case err := <-exited:
		return err
	case <-time.After(exitGrace):
		_ = killTree(s.cmd.Process)
		return <-exited
	}
}

// finish records why the session ended, settles everything still pending
// and ends the running turn.
func (s *session) finish(waitErr error) {
	s.mu.Lock()
	stopping := s.stopping
	var err error = ErrSessionStopped
	if !stopping {
		cause := waitErr
		if s.readErr != nil {
			cause = fmt.Errorf("read output: %w", s.readErr)
		}
		err = &ExitError{Err: cause, Stderr: s.stderr.Tail(stderrInError)}
	}
	s.dead = err
	t := s.turn
	cancelled := t != nil && t.cancelled
	s.turn = nil
	perms := s.perms
	s.perms = map[string]*pendingPerm{}
	waiters := s.ctlWait
	s.ctlWait = map[string]chan controlResponseBody{}
	s.mu.Unlock()

	for _, ch := range waiters {
		ch <- controlResponseBody{Subtype: subtypeError, Error: err.Error()}
	}
	s.settleAll(perms)
	switch {
	case t != nil && (stopping || cancelled):
		s.deliverTurnEnd(t, agentsession.StopCancelled, nil)
	case t != nil:
		s.emit(agentsession.Event{Kind: agentsession.KindError, Text: err.Error()})
		s.deliverTurnEnd(t, agentsession.StopError, err)
	case !stopping:
		s.emit(agentsession.Event{Kind: agentsession.KindError, Text: err.Error()})
	}
	s.events.close()
	<-s.events.drained()
	close(s.done)
}

// beginTurn registers a new turn, failing if one is running or the process
// is gone.
func (s *session) beginTurn() (*turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead != nil {
		return nil, s.dead
	}
	if s.turn != nil {
		return nil, ErrTurnInProgress
	}
	t := &turn{done: make(chan struct{})}
	s.turn = t
	return t, nil
}

// endTurn finishes t (if it is still the running turn) with stop.
func (s *session) endTurn(t *turn, stop string, err error) {
	s.mu.Lock()
	if s.turn != t {
		s.mu.Unlock()
		return
	}
	s.turn = nil
	s.mu.Unlock()
	s.deliverTurnEnd(t, stop, err)
}

// deliverTurnEnd emits KindTurnEnd and releases Prompt once it was delivered.
func (s *session) deliverTurnEnd(t *turn, stop string, err error) {
	t.stop, t.err = stop, err
	ev := agentsession.Event{SessionID: s.id, Kind: agentsession.KindTurnEnd, StopReason: stop}
	if !s.events.push(ev, func() { close(t.done) }) {
		close(t.done)
	}
}

// cancelTurn interrupts t: pending permissions are settled as cancelled and
// the CLI is sent an interrupt control request. The CLI then ends the turn
// with a result, which is reported as StopCancelled.
func (s *session) cancelTurn(t *turn) error {
	s.mu.Lock()
	if s.turn != t || t.cancelled || s.dead != nil {
		s.mu.Unlock()
		return nil
	}
	t.cancelled = true
	perms := s.perms
	s.perms = map[string]*pendingPerm{}
	s.mu.Unlock()
	s.settleAll(perms)
	return s.sendControl(subtypeInterrupt)
}

func (s *session) settleAll(perms map[string]*pendingPerm) {
	for id := range perms {
		s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: id})
	}
}

// respond answers a pending can_use_tool request.
func (s *session) respond(requestID, optionID string) error {
	var allow, always bool
	switch optionID {
	case agentsession.AllowOnce:
		allow = true
	case agentsession.AllowAlways:
		allow, always = true, true
	case agentsession.RejectOnce, agentsession.RejectAlways:
	default:
		return fmt.Errorf("claude: unknown permission option %q", optionID)
	}
	s.mu.Lock()
	p := s.perms[requestID]
	delete(s.perms, requestID)
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("%w: %s", ErrUnknownRequest, requestID)
	}

	var answer any = permissionDeny{Behavior: behaviorDeny, Message: rejectMessage}
	if allow {
		input := p.input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		a := permissionAllow{Behavior: behaviorAllow, UpdatedInput: input}
		if always {
			a.UpdatedPermissions = alwaysAllowRules(p.suggestions)
		}
		answer = a
	}
	// Queue the events before writing: the CLI cannot produce the tool's
	// result (and so its events) before it has read the answer.
	s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: requestID})
	if allow && p.toolUseID != "" {
		s.emit(agentsession.Event{Kind: agentsession.KindToolCallUpdate, ToolCall: &agentsession.ToolCall{
			ID: p.toolUseID, Status: agentsession.StatusInProgress,
		}})
	}
	return s.write(outControlResponse{Type: msgControlResp, Response: outControlResult{
		Subtype: subtypeSuccess, RequestID: requestID, Response: answer,
	}})
}

// shutdown stops the process tree and waits until the session finished.
func (s *session) shutdown() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopping = true
		s.mu.Unlock()
		_ = s.stdin.Close()
		_ = terminateTree(s.cmd.Process)
		select {
		case <-s.done:
		case <-time.After(killGrace):
			_ = killTree(s.cmd.Process)
		}
	})
	<-s.done
}
