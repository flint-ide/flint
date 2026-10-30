// Package codex drives the Codex CLI through its app-server: newline-delimited
// JSON-RPC 2.0 (without the "jsonrpc" member) over the stdio of
// `codex app-server`, the protocol used by the official IDE extension.
//
// Each Flint session owns one app-server process and one Codex thread. A
// prompt is a turn (turn/start ... turn/completed); streamed item
// notifications become agentsession events and approval requests
// (item/commandExecution/requestApproval, item/fileChange/requestApproval)
// become permission requests answered through Respond.
package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"flint/internal/agentsession"
	"flint/internal/shellenv"
)

// Options configure the driver.
type Options struct {
	// Binary is the codex executable; empty means "codex" on the login PATH.
	Binary string
	// Model overrides the configured model (thread/start "model").
	Model string
	// Config holds extra `-c key=value` overrides (value parsed as TOML).
	Config []string
	// Env holds extra KEY=VALUE entries appended to Flint's environment.
	Env []string
	// ApprovalPolicy is "untrusted", "on-request" or "never" (default
	// DefaultApprovalPolicy).
	ApprovalPolicy string
	// Sandbox is "read-only", "workspace-write" or "danger-full-access"
	// (default DefaultSandbox).
	Sandbox string
	// ClientName and ClientVersion are sent as clientInfo.
	ClientName, ClientVersion string
}

// TurnError reports a turn that Codex ended with status "failed".
type TurnError struct{ Message string }

// Error implements error.
func (e *TurnError) Error() string { return "codex: turn failed: " + e.Message }

// Driver implements agentsession.Driver for Codex.
type Driver struct {
	opts Options

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
}

var _ agentsession.Driver = (*Driver)(nil)

// New returns a Driver.
func New(opts Options) *Driver {
	return &Driver{opts: opts, sessions: map[string]*session{}}
}

// Start spawns `codex app-server` in cwd, performs the handshake and opens a
// thread. ctx bounds the handshake only. Sink must not call Stop, Close or
// Prompt synchronously (Respond and Cancel are fine).
func (d *Driver) Start(ctx context.Context, cwd string, sink agentsession.Sink) (agentsession.SessionInfo, error) {
	return d.start(ctx, cwd, "", sink)
}

// Resume spawns `codex app-server` and reopens the stored thread threadID.
func (d *Driver) Resume(ctx context.Context, cwd, threadID string, sink agentsession.Sink) (agentsession.SessionInfo, error) {
	if threadID == "" {
		return agentsession.SessionInfo{}, errors.New("codex: empty thread id")
	}
	d.mu.Lock()
	_, taken := d.sessions[threadID]
	d.mu.Unlock()
	if taken {
		return agentsession.SessionInfo{}, fmt.Errorf("codex: thread %s is already open", threadID)
	}
	return d.start(ctx, cwd, threadID, sink)
}

func (d *Driver) start(ctx context.Context, cwd, resumeID string, sink agentsession.Sink) (agentsession.SessionInfo, error) {
	if sink == nil {
		return agentsession.SessionInfo{}, errors.New("codex: nil sink")
	}
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return agentsession.SessionInfo{}, ErrClosed
	}
	cwd, err := checkDir(cwd)
	if err != nil {
		return agentsession.SessionInfo{}, err
	}
	shellenv.Wait()
	env := buildEnv(d.opts.Env)
	path, err := resolveCommand(orDefault(d.opts.Binary, "codex"), env)
	if err != nil {
		return agentsession.SessionInfo{}, err
	}
	s := newSession(cwd, sink)
	proc, err := startProcess(spawnSpec{path: path, args: d.args(), dir: cwd, env: env}, s.handleRequest, s.handleNotification, s.onExit)
	if err != nil {
		s.events.close()
		return agentsession.SessionInfo{}, err
	}
	s.mu.Lock()
	s.proc = proc
	s.mu.Unlock()

	info, err := s.handshake(ctx, d.opts, resumeID)
	if err == nil {
		err = d.register(info.ID, s)
	}
	if err != nil {
		s.close()
		return agentsession.SessionInfo{}, err
	}
	return info, nil
}

func (d *Driver) args() []string {
	args := []string{"app-server"}
	for _, kv := range d.opts.Config {
		args = append(args, "-c", kv)
	}
	return args
}

func (d *Driver) register(id string, s *session) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	d.sessions[id] = s
	return nil
}

func checkDir(cwd string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("codex: cwd %q: %w", cwd, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("codex: cwd: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("codex: cwd %q is not a directory", abs)
	}
	return abs, nil
}

func (d *Driver) session(id string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("codex: unknown session %q", id)
	}
	return s, nil
}

// Prompt runs one turn and blocks until it ends. Cancelling ctx interrupts
// the turn. A failed turn returns StopError with a *TurnError; a dead
// app-server returns StopError with an *ExitError carrying its stderr tail.
func (d *Driver) Prompt(ctx context.Context, sessionID, text string, attachments []agentsession.Attachment) (string, error) {
	s, err := d.session(sessionID)
	if err != nil {
		return "", err
	}
	return s.prompt(ctx, text, attachments)
}

// Cancel interrupts the running turn (turn/interrupt) and withdraws its
// pending approvals. It is a no-op when no turn is running.
func (d *Driver) Cancel(sessionID string) error {
	s, err := d.session(sessionID)
	if err != nil {
		return err
	}
	s.cancel()
	return nil
}

// Respond answers a pending approval with one of its option ids.
func (d *Driver) Respond(sessionID, requestID, optionID string) error {
	s, err := d.session(sessionID)
	if err != nil {
		return err
	}
	return s.respond(requestID, optionID)
}

// Stop ends the session and its app-server, after delivering every event.
func (d *Driver) Stop(sessionID string) error {
	d.mu.Lock()
	s := d.sessions[sessionID]
	delete(d.sessions, sessionID)
	d.mu.Unlock()
	if s == nil {
		return fmt.Errorf("codex: unknown session %q", sessionID)
	}
	s.close()
	return nil
}

// Close stops every session. Later Start calls fail with ErrClosed.
func (d *Driver) Close() error {
	d.mu.Lock()
	d.closed = true
	sessions := d.sessions
	d.sessions = map[string]*session{}
	d.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Go(s.close)
	}
	wg.Wait()
	return nil
}
