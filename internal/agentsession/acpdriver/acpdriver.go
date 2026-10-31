// Package acpdriver adapts agents that speak the Agent Client Protocol to
// Flint's agent-neutral session model. Each custom agent in settings
// ("customAgents") is one Driver; nothing in here is specific to an agent.
package acpdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"flint/internal/acp"
	as "flint/internal/agentsession"
	"flint/internal/shellenv"
	"flint/internal/workspace"
)

// clientVersion is sent as clientInfo.version.
const clientVersion = "0.1"

// ErrRestricted: the folder isn't trusted. An ACP agent reads the project's
// configuration (its own settings, MCP servers, hooks) and runs commands
// there, so it is held back like a custom language server.
var ErrRestricted = errors.New("Restricted Mode")

// Options configure how the ACP agent process is launched.
type Options struct {
	Name    string // the agent's id in Flint ("gemini"); SessionInfo.Agent
	Command string
	Args    []string
	// Env holds extra KEY=VALUE entries. Settings have none (no secrets in
	// settings.json); tests use it to start the fake agent.
	Env []string
	// Untrusted reports a folder the user hasn't trusted; sessions there are
	// refused (ErrRestricted). Nil trusts every folder (tests).
	Untrusted func(cwd string) bool
}

// Driver starts one ACP agent process per session.
type Driver struct {
	opts Options

	mu       sync.Mutex
	sessions map[string]*session
}

var _ as.Driver = (*Driver)(nil)

type session struct {
	cwd    string
	client *acp.Client
	sink   as.Sink

	sinkMu sync.Mutex // serialises sink calls (updates and permissions arrive on different goroutines)
	id     string     // set once session/new answers; guarded by sinkMu

	permMu  sync.Mutex
	pending map[string]chan string // permission request id → chosen option id
	nextReq atomic.Int64
}

// New creates a driver for an ACP agent binary.
func New(opts Options) *Driver {
	return &Driver{opts: opts, sessions: map[string]*session{}}
}

// CheckTrust refuses a folder the user hasn't trusted.
func (o Options) CheckTrust(cwd string) error {
	if o.Untrusted != nil && o.Untrusted(cwd) {
		return fmt.Errorf("%w: trust this folder to use %s (custom agents read the project's configuration and run commands in it)", ErrRestricted, o.Name)
	}
	return nil
}

// Start launches the agent and opens a session in cwd.
func (d *Driver) Start(ctx context.Context, cwd string, sink as.Sink) (as.SessionInfo, error) {
	if err := d.opts.CheckTrust(cwd); err != nil {
		return as.SessionInfo{}, err
	}
	s := &session{cwd: cwd, sink: sink, pending: map[string]chan string{}}
	client, err := acp.Start(ctx, d.opts.acpOptions(cwd), &handler{s: s})
	if err != nil {
		return as.SessionInfo{}, err
	}
	res, err := client.NewSession(ctx, cwd, []json.RawMessage{})
	if err != nil {
		_ = client.Close()
		return as.SessionInfo{}, err
	}
	s.client = client
	s.setID(res.SessionID)
	d.mu.Lock()
	d.sessions[res.SessionID] = s
	d.mu.Unlock()
	return as.SessionInfo{ID: res.SessionID, Agent: d.opts.Name, Cwd: cwd}, nil
}

// waitShellEnv blocks until the login shell's PATH was imported, so a bare
// command resolves as in the user's terminal. A variable for tests.
var waitShellEnv = shellenv.Wait

// acpOptions launches the agent in dir with Flint's environment (the login
// shell's PATH), like the native agents.
func (o Options) acpOptions(dir string) acp.Options {
	waitShellEnv()
	return acp.Options{Command: o.Command, Args: o.Args, Env: o.Env, Dir: dir, ClientName: "flint", ClientVersion: clientVersion}
}

// Prompt sends text plus attachments and waits for the turn to end.
func (d *Driver) Prompt(ctx context.Context, sessionID, text string, attachments []as.Attachment) (string, error) {
	s, err := d.get(sessionID)
	if err != nil {
		return "", err
	}
	embed := s.client.Info().AgentCapabilities.PromptCapabilities.EmbeddedContext
	stop, err := s.client.Prompt(ctx, sessionID, promptBlocks(text, attachments, embed))
	if err != nil {
		s.emit(as.Event{Kind: as.KindError, Text: err.Error()})
		s.emit(as.Event{Kind: as.KindTurnEnd, StopReason: as.StopError})
		return as.StopError, err
	}
	s.emit(as.Event{Kind: as.KindTurnEnd, StopReason: stop})
	return stop, nil
}

// promptBlocks builds the prompt. Attached text (a selection) is embedded as
// a resource when the agent accepts those, else added as plain text; files
// without text are links the agent reads itself.
func promptBlocks(text string, attachments []as.Attachment, embed bool) []acp.ContentBlock {
	blocks := []acp.ContentBlock{acp.TextBlock(text)}
	for _, a := range attachments {
		uri := "file://" + a.Path
		switch {
		case a.Text == "":
			blocks = append(blocks, acp.ResourceLinkBlock(uri, a.Path))
		case embed:
			blocks = append(blocks, acp.TextResourceBlock(uri, "text/plain", a.Text))
		default:
			blocks = append(blocks, acp.TextBlock(attachmentText(a)))
		}
	}
	return blocks
}

func attachmentText(a as.Attachment) string {
	where := a.Path
	if a.StartLine > 0 {
		where += fmt.Sprintf(" (lines %d–%d)", a.StartLine, max(a.StartLine, a.EndLine))
	}
	return where + ":\n" + a.Text
}

// Cancel asks the agent to stop the running turn.
func (d *Driver) Cancel(sessionID string) error {
	s, err := d.get(sessionID)
	if err != nil {
		return err
	}
	return s.client.Cancel(sessionID)
}

// Respond answers a pending permission request.
func (d *Driver) Respond(sessionID, requestID, optionID string) error {
	s, err := d.get(sessionID)
	if err != nil {
		return err
	}
	s.permMu.Lock()
	ch := s.pending[requestID]
	delete(s.pending, requestID)
	s.permMu.Unlock()
	if ch == nil {
		return fmt.Errorf("no pending permission request %q", requestID)
	}
	ch <- optionID
	return nil
}

// Stop ends one session and kills its agent's process group.
func (d *Driver) Stop(sessionID string) error {
	d.mu.Lock()
	s := d.sessions[sessionID]
	delete(d.sessions, sessionID)
	d.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.client.Close()
}

// Close stops every session.
func (d *Driver) Close() error {
	d.mu.Lock()
	all := d.sessions
	d.sessions = map[string]*session{}
	d.mu.Unlock()
	var errs []error
	for _, s := range all {
		errs = append(errs, s.client.Close())
	}
	return errors.Join(errs...)
}

func (d *Driver) get(id string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("unknown session %q", id)
	}
	return s, nil
}

func (s *session) setID(id string) {
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	s.id = id
}

func (s *session) sessionID() string {
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	return s.id
}

func (s *session) emit(e as.Event) {
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	if e.SessionID == "" {
		e.SessionID = s.id
	}
	s.sink(e)
}

// handler implements acp.Handler for one chat session.
type handler struct{ s *session }

func (h *handler) SessionUpdate(sessionID string, update json.RawMessage) {
	for _, e := range translate(update) {
		e.SessionID = sessionID
		h.s.emit(e)
	}
}

// RequestPermission hands the question to Flint's approval flow: the
// session manager answers what the user's policy allows (the same policy as
// for the native agents) and shows the rest in the chat.
func (h *handler) RequestPermission(ctx context.Context, req acp.PermissionRequest) (acp.PermissionOutcome, error) {
	s := h.s
	id := strconv.FormatInt(s.nextReq.Add(1), 10)
	ch := make(chan string, 1)
	s.permMu.Lock()
	s.pending[id] = ch
	s.permMu.Unlock()

	opts := make([]as.PermissionOption, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, as.PermissionOption{ID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	tc := toolCallFromRaw(req.ToolCall.Raw)
	s.emit(as.Event{SessionID: req.SessionID, Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: id, ToolCall: tc, Options: opts}})

	select {
	case opt := <-ch:
		s.emit(as.Event{SessionID: req.SessionID, Kind: as.KindPermissionSettled, RequestID: id})
		return acp.Selected(opt), nil
	case <-ctx.Done():
		s.permMu.Lock()
		delete(s.pending, id)
		s.permMu.Unlock()
		s.emit(as.Event{SessionID: req.SessionID, Kind: as.KindPermissionSettled, RequestID: id})
		return acp.Cancelled(), nil
	}
}

func (h *handler) ReadTextFile(_ context.Context, req acp.ReadTextFileRequest) (string, error) {
	if req.SessionID != h.s.sessionID() {
		return "", fmt.Errorf("unknown session %q", req.SessionID)
	}
	return readConfined(h.s.cwd, req)
}

func (h *handler) WriteTextFile(_ context.Context, req acp.WriteTextFileRequest) error {
	if req.SessionID != h.s.sessionID() {
		return fmt.Errorf("unknown session %q", req.SessionID)
	}
	path, err := confine(h.s.cwd, req.Path)
	if err != nil {
		return err
	}
	if inGitDir(h.s.cwd, path) {
		// A hook or a config there runs code on the next git command; no
		// edit an agent is asked for needs to write git's own files.
		return fmt.Errorf("writing inside .git is not allowed: %s", path)
	}
	// Same save path as the editor: follows symlinks, keeps permissions.
	return workspace.WriteFile(path, req.Content)
}

// readConfined serves fs/read_text_file inside root.
func readConfined(root string, req acp.ReadTextFileRequest) (string, error) {
	path, err := confine(root, req.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return acp.ApplyLineWindow(string(data), req), nil
}

// confine accepts only absolute paths that stay inside root (symlinks
// resolved), so a misbehaving agent can't read or write elsewhere through
// Flint.
func confine(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %q", path)
	}
	clean := filepath.Clean(path)
	if !as.Inside(clean, root) {
		return "", fmt.Errorf("access outside the project denied: %s", clean)
	}
	return clean, nil
}

// inGitDir reports a path inside root's .git directory, symlinks resolved
// and in any letter case (macOS file systems ignore case).
func inGitDir(root, path string) bool {
	rel, err := filepath.Rel(as.Resolve(root), as.Resolve(path))
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return strings.EqualFold(first, ".git")
}
