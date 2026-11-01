package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// pipeWaitDelay bounds how long Wait waits for stderr to close after
	// the agent exits (a grandchild may keep it open).
	pipeWaitDelay = 2 * time.Second
	// drainGrace is how long buffered stdout may drain after the agent
	// exits before the pipe is force-closed.
	drainGrace = time.Second
	// exitGrace is how long an agent whose stdout closed may live on
	// before it is killed.
	exitGrace = 2 * time.Second
	// killGrace is how long Close waits after SIGTERM before SIGKILL.
	killGrace = 3 * time.Second
	// handlerGrace is how long the shutdown waits for request handlers that
	// were cancelled but haven't returned (one stuck in a system call).
	handlerGrace = 2 * time.Second
)

const (
	defaultClientName    = "flint"
	defaultClientVersion = "0.0.0"
)

// Handler is implemented by Flint's app layer. Methods may be called
// concurrently from the connection's goroutines.
//
// SessionUpdate calls are serialised in wire order and delivered before the
// response that follows them (so every update of a turn has been delivered
// when Prompt returns); a slow SessionUpdate delays later messages but never
// stalls reading the agent's stdout. Request methods each run on their own
// goroutine and must return promptly once ctx is cancelled (turn
// cancellation, `$/cancel_request` or Close). No method may call Close
// synchronously.
type Handler interface {
	// SessionUpdate forwards a `session/update` notification. update is the
	// raw SessionUpdate object; its "sessionUpdate" field names the variant
	// (agent_message_chunk, agent_thought_chunk, user_message_chunk,
	// tool_call, tool_call_update, plan, available_commands_update,
	// current_mode_update, ...).
	SessionUpdate(sessionID string, update json.RawMessage)
	// RequestPermission answers `session/request_permission`. It blocks
	// until the user (or an auto-approve policy) decides or ctx is
	// cancelled; when the turn is cancelled the client replies with the
	// "cancelled" outcome regardless of what is returned.
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionOutcome, error)
	// ReadTextFile serves `fs/read_text_file`, including unsaved editor
	// state. It must honour req.Line/req.Limit (see ApplyLineWindow).
	ReadTextFile(ctx context.Context, req ReadTextFileRequest) (string, error)
	// WriteTextFile serves `fs/write_text_file`.
	WriteTextFile(ctx context.Context, req WriteTextFileRequest) error
}

// Options configure the agent subprocess.
type Options struct {
	// Command is the adapter executable (e.g. "claude-agent-acp"). A bare
	// name is resolved against the PATH in the agent's environment.
	Command string
	Args    []string
	// Dir is the working directory; empty means Flint's own.
	Dir string
	// Env holds extra KEY=VALUE entries appended to Flint's environment;
	// later entries win.
	Env []string
	// ClientName and ClientVersion are sent as clientInfo (defaults
	// "flint" / "0.0.0").
	ClientName, ClientVersion string
}

// Client is a connection to one agent subprocess.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.Closer
	stdout *os.File
	conn   *conn
	h      Handler
	stderr *ringBuffer
	info   InitializeResult
	turns  *turnRegistry

	closing   atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	err       error // written once before done is closed
}

// Start spawns the agent and performs the `initialize` handshake. ctx bounds
// the handshake only; the process lives until Close or until it exits.
func Start(ctx context.Context, opts Options, h Handler) (*Client, error) {
	if h == nil {
		return nil, errors.New("acp: nil handler")
	}
	env := buildEnv(opts.Env)
	path, err := resolveCommand(opts.Command, env)
	if err != nil {
		return nil, err
	}
	c, err := spawn(path, opts, env, h)
	if err != nil {
		return nil, err
	}
	if err := c.initialize(ctx, opts); err != nil {
		// The agent has done nothing worth a graceful stop, and a hung one
		// (it may ignore SIGTERM) must not add the grace period to the
		// caller's timeout: kill the whole tree at once.
		c.kill()
		return nil, &HandshakeError{Err: err, Stderr: c.stderr.Tail(stderrInError)}
	}
	return c, nil
}

// HandshakeError is returned by Start when the `initialize` handshake fails.
// Stderr is the tail of what the agent printed, which often says why (a
// login prompt, an unknown flag); Error leaves it out so callers decide how
// much of it to show.
type HandshakeError struct {
	Err    error
	Stderr string
}

// Error implements error.
func (e *HandshakeError) Error() string { return e.Err.Error() }

// Unwrap exposes the cause (context.DeadlineExceeded for a timeout).
func (e *HandshakeError) Unwrap() error { return e.Err }

func spawn(path string, opts Options, env []string, h Handler) (*Client, error) {
	cmd := exec.Command(path, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = env
	cmd.WaitDelay = pipeWaitDelay
	isolateProcessTree(cmd)

	stderr := newRingBuffer(stderrCapacity)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	// A plain os.Pipe (not StdoutPipe) so Wait never closes the read end
	// under the reader: buffered output is drained after the agent exits.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	if err := cmd.Start(); err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("acp: start agent %s: %w", path, err)
	}
	_ = stdoutW.Close()

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdoutR,
		h:      h,
		stderr: stderr,
		turns:  newTurnRegistry(),
		done:   make(chan struct{}),
	}
	c.conn = newConn(stdin, c.serveRequest, c.serveNotification)
	c.conn.start(stdoutR)
	go c.supervise()
	return c, nil
}

func (c *Client) initialize(ctx context.Context, opts Options) error {
	params := initializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientCapabilities: clientCapabilities{
			FS:       fsCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: false,
		},
		ClientInfo: Implementation{
			Name:    orDefault(opts.ClientName, defaultClientName),
			Version: orDefault(opts.ClientVersion, defaultClientVersion),
		},
	}
	var raw json.RawMessage
	if err := c.conn.call(ctx, methodInitialize, params, &raw, nil); err != nil {
		return err
	}
	var res InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("acp: initialize: decode result: %w", err)
	}
	if res.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("acp: initialize: agent chose unsupported protocol version %d (client supports %d)",
			res.ProtocolVersion, ProtocolVersion)
	}
	res.Raw = raw
	c.info = res
	return nil
}

// supervise waits for the process to exit (or its stdout to end), drains
// the connection, fails pending calls and closes Done.
func (c *Client) supervise() {
	exited := make(chan error, 1)
	go func() { exited <- c.cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-exited:
		select {
		case <-c.conn.Drained():
		case <-time.After(drainGrace):
		}
	case <-c.conn.Drained():
		// stdout ended (or the stream broke) while the process lives on.
		waitErr = c.stopProcess(exited)
	}
	_ = c.stdout.Close() // unblocks the reader if a grandchild holds stdout
	<-c.conn.Drained()

	c.err = c.finalError(waitErr)
	c.conn.shutdown(c.err)
	// Handlers are cancelled now. One that ignores that (blocked reading a
	// FIFO, say) must not keep Close, a one-shot run or quitting Flint
	// waiting; its late reply goes to a closed pipe.
	handlersDone := make(chan struct{})
	go func() {
		c.conn.waitHandlers()
		close(handlersDone)
	}()
	select {
	case <-handlersDone:
	case <-time.After(handlerGrace):
	}
	close(c.done)
}

// stopProcess terminates the process tree, escalating to SIGKILL, and
// returns the wait error.
func (c *Client) stopProcess(exited <-chan error) error {
	_ = terminateTree(c.cmd.Process)
	select {
	case err := <-exited:
		return err
	case <-time.After(exitGrace):
		_ = killTree(c.cmd.Process)
		return <-exited
	}
}

func (c *Client) finalError(waitErr error) error {
	if c.closing.Load() {
		return ErrClosed
	}
	exitErr := &AgentExitedError{Err: waitErr, Stderr: c.stderr.Tail(stderrInError)}
	if readErr := c.conn.ReadErr(); readErr != nil {
		return fmt.Errorf("acp: read agent output: %w; %w", readErr, exitErr)
	}
	return exitErr
}

// Info returns the agent's InitializeResponse.
func (c *Client) Info() InitializeResult { return c.info.clone() }

// Stderr returns the retained tail of the agent's stderr.
func (c *Client) Stderr() string { return c.stderr.Tail(stderrCapacity) }

// Authenticate runs `authenticate` with one of Info().AuthMethods.
func (c *Client) Authenticate(ctx context.Context, methodID string) error {
	return c.conn.call(ctx, methodAuthenticate, authenticateParams{MethodID: methodID}, nil, nil)
}

// NewSession runs `session/new`. cwd must be absolute; mcpServers are
// McpServer objects passed through verbatim (nil means none).
func (c *Client) NewSession(ctx context.Context, cwd string, mcpServers []json.RawMessage) (NewSessionResult, error) {
	if !filepath.IsAbs(cwd) {
		return NewSessionResult{}, fmt.Errorf("acp: %s: cwd %q is not absolute", methodSessionNew, cwd)
	}
	if mcpServers == nil {
		mcpServers = []json.RawMessage{}
	}
	var raw json.RawMessage
	if err := c.conn.call(ctx, methodSessionNew, newSessionParams{Cwd: cwd, MCPServers: mcpServers}, &raw, nil); err != nil {
		return NewSessionResult{}, err
	}
	var res NewSessionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return NewSessionResult{}, fmt.Errorf("acp: %s: decode result: %w", methodSessionNew, err)
	}
	if res.SessionID == "" {
		return NewSessionResult{}, fmt.Errorf("acp: %s: agent returned no sessionId", methodSessionNew)
	}
	res.Raw = raw
	return res, nil
}

// Prompt runs one `session/prompt` turn and returns its stop reason
// (StopEndTurn, StopCancelled, ...). Cancelling ctx sends `session/cancel`,
// answers pending permission requests with "cancelled", and keeps waiting
// for the agent's response (normally StopCancelled).
func (c *Client) Prompt(ctx context.Context, sessionID string, prompt []ContentBlock) (string, error) {
	if prompt == nil {
		prompt = []ContentBlock{}
	}
	c.turns.begin(sessionID)
	defer c.turns.end(sessionID)
	var res promptResult
	onCancel := func() { _ = c.Cancel(sessionID) }
	if err := c.conn.call(ctx, methodPrompt, promptParams{SessionID: sessionID, Prompt: prompt}, &res, onCancel); err != nil {
		return "", err
	}
	return res.StopReason, nil
}

// Cancel sends the `session/cancel` notification and resolves the
// session's pending permission requests with the "cancelled" outcome.
func (c *Client) Cancel(sessionID string) error {
	cancels := c.turns.cancel(sessionID)
	err := c.conn.notify(methodCancel, sessionParams{SessionID: sessionID})
	for _, cancel := range cancels {
		cancel(errTurnCancelled)
	}
	return err
}

// SetMode runs `session/set_mode`.
func (c *Client) SetMode(ctx context.Context, sessionID, modeID string) error {
	return c.conn.call(ctx, methodSetMode, setModeParams{SessionID: sessionID, ModeID: modeID}, nil, nil)
}

// Done is closed once the agent process has exited and every connection
// goroutine has finished.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended: nil while running, ErrClosed after
// Close, or an *AgentExitedError (with stderr tail) if the agent died.
func (c *Client) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

// Close fails pending calls with ErrClosed, terminates the agent's process
// tree (SIGTERM, then SIGKILL after a grace period) and waits until every
// goroutine has exited. It is safe to call more than once.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.beginClose()
		_ = terminateTree(c.cmd.Process)
		select {
		case <-c.done:
		case <-time.After(killGrace):
			_ = killTree(c.cmd.Process)
		}
	})
	<-c.done
	return nil
}

// kill is Close without the grace period: SIGKILL for the whole tree.
func (c *Client) kill() {
	c.closeOnce.Do(func() {
		c.beginClose()
		_ = killTree(c.cmd.Process)
	})
	<-c.done
}

// beginClose fails pending calls and closes the agent's stdin.
func (c *Client) beginClose() {
	c.closing.Store(true)
	c.conn.shutdown(ErrClosed)
	_ = c.stdin.Close()
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
