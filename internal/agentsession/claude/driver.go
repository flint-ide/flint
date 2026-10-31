// Package claude drives the native Claude Code CLI (`claude`) through its
// headless stream-json mode and translates it into agentsession events.
//
// One long-lived process runs per session:
//
//	claude -p --input-format stream-json --output-format stream-json --verbose
//	       --include-partial-messages --permission-prompt-tool stdio
//	       --permission-mode <mode> --session-id <uuid> [--model <m>] [extra...]
//
// Resume replaces --session-id with --resume <uuid> to continue a saved
// conversation.
//
// Each Prompt writes one stream-json user message to stdin; the turn ends
// with a "result" line. Permission prompts arrive as control_request
// can_use_tool and are answered with control_response; Cancel sends an
// interrupt control_request. See wire.go for the observed message shapes.
package claude

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"flint/internal/agentsession"
	"flint/internal/shellenv"
)

// AgentName is SessionInfo.Agent for this driver.
const AgentName = "claude"

const (
	defaultBinary         = "claude"
	defaultPermissionMode = "default"
	// startTimeout bounds the initialize handshake when Start's ctx has no
	// deadline of its own.
	startTimeout = 60 * time.Second
)

// cancelGrace is how long Prompt waits, after its ctx is cancelled and the
// interrupt was sent, for the CLI to end the turn before the session is
// stopped. A variable for tests.
var cancelGrace = 15 * time.Second

// waitShellEnv blocks until the login-shell PATH was imported. A variable
// for tests.
var waitShellEnv = shellenv.Wait

// defaultEnv is set for the CLI unless Options.Env overrides it. Headless
// Claude Code ships without a planning tool; this enables its task-list
// tools (TaskCreate/TaskUpdate), which are shown as the session plan.
var defaultEnv = []string{"CLAUDE_CODE_ENABLE_TODO_TOOLS=1"}

// inheritedEnvDrop lists variables that would make the child believe it runs
// inside another Claude Code session (when Flint itself was started from one).
var inheritedEnvDrop = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT"}

// Options configure the driver.
type Options struct {
	// Binary is the claude executable; empty means "claude" resolved via the
	// login-shell PATH (then common install locations).
	Binary string
	// Model is passed as --model (an alias like "sonnet" or a full name).
	Model string
	// PermissionMode is passed as --permission-mode ("default" when empty).
	PermissionMode string
	// ExtraArgs are appended to the command line.
	ExtraArgs []string
	// Env holds extra KEY=VALUE entries; later entries win.
	Env []string
	// Untrusted reports a folder the user hasn't trusted: sessions there load
	// only the user's settings, so the project's .claude/settings.json hooks
	// and permission rules don't apply (-p mode skips Claude Code's own
	// workspace-trust prompt). Nil trusts every folder.
	Untrusted func(cwd string) bool
}

// Driver runs Claude Code sessions. It is safe for concurrent use.
type Driver struct {
	opts Options

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
}

var _ agentsession.Driver = (*Driver)(nil)

// New returns a driver; no process is started until Start.
func New(opts Options) *Driver {
	return &Driver{opts: opts, sessions: map[string]*session{}}
}

// Start spawns a claude process for a new conversation in cwd and performs
// the initialize handshake. ctx bounds the handshake only.
func (d *Driver) Start(ctx context.Context, cwd string, sink agentsession.Sink) (agentsession.SessionInfo, error) {
	id, err := newSessionID()
	if err != nil {
		return agentsession.SessionInfo{}, err
	}
	return d.start(ctx, cwd, id, false, sink)
}

// Resume reopens an earlier conversation (claude --resume) under the same id.
func (d *Driver) Resume(ctx context.Context, cwd, sessionID string, sink agentsession.Sink) (agentsession.SessionInfo, error) {
	if !isSessionID(sessionID) {
		return agentsession.SessionInfo{}, fmt.Errorf("claude: invalid session id %q", sessionID)
	}
	return d.start(ctx, cwd, sessionID, true, sink)
}

func (d *Driver) start(ctx context.Context, cwd, id string, resume bool, sink agentsession.Sink) (agentsession.SessionInfo, error) {
	if sink == nil {
		return agentsession.SessionInfo{}, errors.New("claude: nil sink")
	}
	if !filepath.IsAbs(cwd) {
		return agentsession.SessionInfo{}, fmt.Errorf("claude: cwd %q is not absolute", cwd)
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return agentsession.SessionInfo{}, fmt.Errorf("claude: cwd %q is not a directory", cwd)
	}
	if d.isClosed() {
		return agentsession.SessionInfo{}, ErrClosed
	}
	path, err := resolveBinary(d.opts.Binary)
	if err != nil {
		return agentsession.SessionInfo{}, err
	}
	d.mu.Lock()
	_, taken := d.sessions[id]
	d.mu.Unlock()
	if taken {
		return agentsession.SessionInfo{}, fmt.Errorf("claude: session %s is already open", id)
	}
	s, err := spawn(path, buildArgs(id, resume, d.opts, d.opts.Untrusted != nil && d.opts.Untrusted(cwd)), cwd, buildEnv(d.opts.Env, cwd), id, sink)
	if err != nil {
		return agentsession.SessionInfo{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, startTimeout)
		defer cancel()
	}
	raw, err := s.control(ctx, subtypeInitialize)
	if err != nil {
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			if tail := strings.TrimSpace(s.stderr.Tail(stderrInError)); tail != "" {
				err = fmt.Errorf("%w; stderr: %s", err, tail)
			}
		}
		s.shutdown()
		return agentsession.SessionInfo{}, fmt.Errorf("claude: start session: %w", err)
	}
	model := resolveModel(raw, d.opts.Model)
	s.mu.Lock()
	s.model = model
	s.mu.Unlock()

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		s.shutdown()
		return agentsession.SessionInfo{}, ErrClosed
	}
	d.sessions[id] = s
	d.mu.Unlock()
	return agentsession.SessionInfo{ID: id, Agent: AgentName, Model: model, Cwd: cwd}, nil
}

// Prompt sends a user message and blocks until the turn ends. Cancelling ctx
// interrupts the turn; Prompt then still waits (up to cancelGrace) for the
// CLI to wind down and returns StopCancelled.
func (d *Driver) Prompt(ctx context.Context, sessionID, text string, attachments []agentsession.Attachment) (string, error) {
	s, err := d.session(sessionID)
	if err != nil {
		return "", err
	}
	msg, err := buildUserMessage(text, attachments)
	if err != nil {
		return "", err
	}
	t, err := s.beginTurn()
	if err != nil {
		return "", err
	}
	if err := s.write(msg); err != nil {
		// The pipe broke: the process is going away and supervise will end
		// the turn with its exit error.
		go s.shutdownAsFailure()
	}
	select {
	case <-t.done:
		return t.stop, t.err
	case <-ctx.Done():
	}
	_ = s.cancelTurn(t)
	timer := time.NewTimer(cancelGrace)
	defer timer.Stop()
	select {
	case <-t.done:
	case <-timer.C:
		s.shutdown()
		<-t.done
	}
	return t.stop, t.err
}

// Cancel interrupts the running turn, if any.
func (d *Driver) Cancel(sessionID string) error {
	s, err := d.session(sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	t := s.turn
	s.mu.Unlock()
	if t == nil {
		return nil
	}
	return s.cancelTurn(t)
}

// Respond answers a pending permission request with an option ID
// (agentsession.AllowOnce, AllowAlways or RejectOnce).
func (d *Driver) Respond(sessionID, requestID, optionID string) error {
	s, err := d.session(sessionID)
	if err != nil {
		return err
	}
	return s.respond(requestID, optionID)
}

// Stop ends a session and its process tree. A running turn ends with
// StopCancelled. It must not be called from the sink.
func (d *Driver) Stop(sessionID string) error {
	d.mu.Lock()
	s := d.sessions[sessionID]
	delete(d.sessions, sessionID)
	d.mu.Unlock()
	if s == nil {
		return fmt.Errorf("%w: %s", ErrUnknownSession, sessionID)
	}
	s.shutdown()
	return nil
}

// Close stops every session and rejects further Starts. It is safe to call
// more than once and must not be called from a sink.
func (d *Driver) Close() error {
	d.mu.Lock()
	d.closed = true
	sessions := d.sessions
	d.sessions = map[string]*session{}
	d.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.shutdown()
		}()
	}
	wg.Wait()
	return nil
}

// Model reports the model the session last reported (system/init), or the
// one resolved at Start.
func (d *Driver) Model(sessionID string) (string, error) {
	s, err := d.session(sessionID)
	if err != nil {
		return "", err
	}
	return s.currentModel(), nil
}

func (d *Driver) session(id string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, ErrClosed
	}
	s := d.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSession, id)
	}
	return s, nil
}

func (d *Driver) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// shutdownAsFailure kills the process without marking the stop as intended,
// so the running turn ends with the exit error.
func (s *session) shutdownAsFailure() {
	_ = terminateTree(s.cmd.Process)
	select {
	case <-s.done:
	case <-time.After(killGrace):
		_ = killTree(s.cmd.Process)
	}
}

// buildArgs assembles the CLI command line. resume continues the saved
// conversation sessionID instead of starting a new one under that id.
func buildArgs(sessionID string, resume bool, opts Options, untrusted bool) []string {
	mode := opts.PermissionMode
	if mode == "" {
		mode = defaultPermissionMode
	}
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose", // required by stream-json output
		"--include-partial-messages",
		"--permission-prompt-tool", "stdio",
		"--permission-mode", mode,
	}
	if resume {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if untrusted {
		args = append(args, "--setting-sources", "user")
	}
	return append(args, opts.ExtraArgs...)
}

// buildEnv returns the CLI environment: Flint's (minus variables of an
// enclosing Claude Code session), the defaults, the caller's extras and PWD.
func buildEnv(extra []string, cwd string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return slices.Contains(inheritedEnvDrop, key)
	})
	return slices.Concat(env, defaultEnv, extra, []string{"PWD=" + cwd})
}

// resolveBinary finds the claude executable. Bare names are looked up on the
// login-shell PATH (a GUI app starts with a minimal one), then in the
// locations the native installer and package managers use.
func resolveBinary(name string) (string, error) {
	if name == "" {
		name = defaultBinary
	}
	if strings.ContainsAny(name, `/\`) {
		if !isExecutable(name) {
			return "", fmt.Errorf("claude: %s is not an executable file", name)
		}
		return name, nil
	}
	waitShellEnv()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, name); isExecutable(p) {
			return p, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil && runtime.GOOS != "windows" {
		for _, p := range []string{
			filepath.Join(home, ".local", "bin", name),
			filepath.Join(home, ".claude", "local", name),
			filepath.Join("/opt/homebrew/bin", name),
			filepath.Join("/usr/local/bin", name),
		} {
			if isExecutable(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("claude: %q not found on PATH; install Claude Code (https://claude.com/claude-code) or set its path in settings", name)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// resolveModel maps the requested model (or "default") to the full model
// name using the initialize response's model list.
func resolveModel(initResp json.RawMessage, want string) string {
	var r struct {
		Models []struct {
			Value         string `json:"value"`
			ResolvedModel string `json:"resolvedModel"`
		} `json:"models"`
	}
	_ = json.Unmarshal(initResp, &r)
	key := want
	if key == "" {
		key = "default"
	}
	for _, m := range r.Models {
		if m.Value == key && m.ResolvedModel != "" {
			return m.ResolvedModel
		}
	}
	return want
}

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isSessionID reports whether id is a lowercase UUID, the only form Flint
// hands to --resume (it comes back from the saved session file).
func isSessionID(id string) bool { return sessionIDPattern.MatchString(id) }

// newSessionID returns a random RFC 4122 version 4 UUID (--session-id
// requires one).
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("claude: session id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
