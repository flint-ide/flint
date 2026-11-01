// Package acpdriver adapts agents that speak the Agent Client Protocol to
// Flint's agent-neutral session model. Each custom agent in settings
// ("customAgents") is one Driver; nothing in here is specific to an agent.
package acpdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"flint/internal/acp"
	as "flint/internal/agentsession"
	"flint/internal/config"
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
	// MCP offers Flint's own read-only MCP server in session/new, to agents
	// that advertise mcpCapabilities.http. Nil offers none.
	MCP as.MCPProvider
}

// mcpServers is session/new's mcpServers: Flint's server as an HTTP McpServer
// when the agent can connect to one, else none. The token travels over the
// agent's stdin, never its command line.
func mcpServers(caps acp.MCPCapabilities, provider as.MCPProvider, cwd string) []json.RawMessage {
	if !caps.HTTP {
		return []json.RawMessage{}
	}
	srv, ok := provider.Offer(cwd)
	if !ok {
		return []json.RawMessage{}
	}
	type header struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	raw, err := json.Marshal(struct {
		Type    string   `json:"type"`
		Name    string   `json:"name"`
		URL     string   `json:"url"`
		Headers []header `json:"headers"`
	}{"http", as.MCPServerName, srv.URL, []header{{"Authorization", "Bearer " + srv.Token}}})
	if err != nil {
		return []json.RawMessage{}
	}
	return []json.RawMessage{raw}
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
	writes  atomic.Int64 // numbers the writes Flint asks about itself
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
	aopts, err := d.opts.acpOptions(ctx, cwd)
	if err != nil {
		return as.SessionInfo{}, err
	}
	client, err := acp.Start(ctx, aopts, &handler{s: s})
	if err != nil {
		return as.SessionInfo{}, err
	}
	res, err := client.NewSession(ctx, cwd, mcpServers(client.Info().AgentCapabilities.MCPCapabilities, d.opts.MCP, cwd))
	if err != nil {
		_ = client.Close()
		return as.SessionInfo{}, err
	}
	d.mu.Lock()
	if d.sessions[res.SessionID] != nil {
		// The id is the agent's choice; replacing the open chat under it
		// would orphan that chat's process.
		d.mu.Unlock()
		_ = client.Close()
		return as.SessionInfo{}, fmt.Errorf("%s reused the id of an open chat (%s)", d.opts.Name, res.SessionID)
	}
	s.client = client
	s.setID(res.SessionID)
	d.sessions[res.SessionID] = s
	d.mu.Unlock()
	return as.SessionInfo{ID: res.SessionID, Agent: d.opts.Name, Cwd: cwd}, nil
}

// waitShellEnv blocks until the login shell's PATH was imported, so a bare
// command resolves as in the user's terminal. A variable for tests.
var waitShellEnv = shellenv.Wait

// acpOptions launches the agent in dir with Flint's environment (the login
// shell's PATH), like the native agents. Waiting for that PATH counts
// against ctx, so a slow login shell can't stretch a caller's timeout.
func (o Options) acpOptions(ctx context.Context, dir string) (acp.Options, error) {
	ready, wait := make(chan struct{}), waitShellEnv
	go func() {
		wait()
		close(ready)
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		return acp.Options{}, fmt.Errorf("waiting for the login shell's PATH: %w", ctx.Err())
	}
	return acp.Options{Command: o.Command, Args: o.Args, Env: o.Env, Dir: dir, ClientName: "flint", ClientVersion: clientVersion}, nil
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
	if id := h.s.sessionID(); id != "" && sessionID != id {
		return // each process serves one chat; another id is a confused agent
	}
	for _, e := range translate(update) {
		e.SessionID = sessionID
		h.s.emit(e)
	}
}

// RequestPermission hands the question to Flint's approval flow: the
// session manager answers what the user's policy allows (the same policy as
// for the native agents) and shows the rest in the chat.
func (h *handler) RequestPermission(ctx context.Context, req acp.PermissionRequest) (acp.PermissionOutcome, error) {
	if req.SessionID != h.s.sessionID() {
		return acp.Cancelled(), nil // approvals go to this chat only
	}
	opts := make([]as.PermissionOption, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, as.PermissionOption{ID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	opt, ok := h.s.ask(ctx, toolCallFromRaw(req.ToolCall.Raw), opts)
	if !ok {
		return acp.Cancelled(), nil
	}
	return acp.Selected(opt), nil
}

// ask shows a permission request through the session's sink and waits for
// the answer (Driver.Respond); false when ctx ends first.
func (s *session) ask(ctx context.Context, tc as.ToolCall, opts []as.PermissionOption) (string, bool) {
	id := strconv.FormatInt(s.nextReq.Add(1), 10)
	ch := make(chan string, 1)
	s.permMu.Lock()
	s.pending[id] = ch
	s.permMu.Unlock()
	s.emit(as.Event{Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: id, ToolCall: tc, Options: opts}})

	select {
	case opt := <-ch:
		s.emit(as.Event{Kind: as.KindPermissionSettled, RequestID: id})
		return opt, true
	case <-ctx.Done():
		s.permMu.Lock()
		delete(s.pending, id)
		s.permMu.Unlock()
		s.emit(as.Event{Kind: as.KindPermissionSettled, RequestID: id})
		return "", false
	}
}

func (h *handler) ReadTextFile(_ context.Context, req acp.ReadTextFileRequest) (string, error) {
	if req.SessionID != h.s.sessionID() {
		return "", fmt.Errorf("unknown session %q", req.SessionID)
	}
	return readConfined(h.s.cwd, req)
}

func (h *handler) WriteTextFile(ctx context.Context, req acp.WriteTextFileRequest) error {
	if req.SessionID != h.s.sessionID() {
		return fmt.Errorf("unknown session %q", req.SessionID)
	}
	path, err := confine(h.s.cwd, req.Path)
	if err != nil {
		return err
	}
	switch dir := as.ProtectedDir(path, h.s.cwd); dir {
	case "":
	case ".git":
		// A hook or a config there (or a new .git that makes a repository
		// with one) runs code on the next git command; no edit an agent is
		// asked for needs to write git's own files.
		return fmt.Errorf("writing inside .git is not allowed: %s", path)
	default:
		// Agent and editor configuration can run code too (hooks, MCP
		// servers, tasks): the user decides, in the chat. The default policy
		// never answers this on its own (only "approve everything" does).
		if !h.s.approveWrite(ctx, path, req.Content) {
			return fmt.Errorf("writing %s was not approved (files in %s need your approval)", path, dir)
		}
	}
	return writeConfined(h.s.cwd, path, req.Content)
}

// writeOptions answer a write Flint asks the user about itself.
var writeOptions = []as.PermissionOption{
	{ID: "allow", Name: "Allow", Kind: as.AllowOnce},
	{ID: "reject", Name: "Reject", Kind: as.RejectOnce},
}

// approveWrite asks the user about an fs/write_text_file into a protected
// folder, showing the change as an edit of that path.
func (s *session) approveWrite(ctx context.Context, path, content string) bool {
	old, _ := readConfined(s.cwd, acp.ReadTextFileRequest{Path: path})
	tc := as.ToolCall{
		ID: "flint-write-" + strconv.FormatInt(s.writes.Add(1), 10), Title: "Write " + path,
		Kind: as.ToolEdit, Status: as.StatusPending, Paths: []string{path},
		Diffs: []as.Diff{{Path: path, OldText: old, NewText: content}},
	}
	opt, ok := s.ask(ctx, tc, writeOptions)
	return ok && opt == writeOptions[0].ID
}

// maxReadBytes caps fs/read_text_file at the editor's limit, so a request
// for a multi-GB file doesn't load it into memory.
const maxReadBytes = workspace.MaxFileSize

// readConfined serves fs/read_text_file inside root.
func readConfined(root string, req acp.ReadTextFileRequest) (string, error) {
	path, err := confine(root, req.Path)
	if err != nil {
		return "", err
	}
	data, err := readRegular(root, path)
	if err != nil {
		return "", err
	}
	return acp.ApplyLineWindow(string(data), req), nil
}

// readRegular reads a regular file of at most maxReadBytes. The path is
// resolved and checked against root again right before it is opened (a link
// may have changed since confine), and the opened file is what's checked: a
// FIFO or a device would block the handler, and with it Close and quitting.
func readRegular(root, path string) ([]byte, error) {
	target, err := resolveInside(root, path)
	if err != nil {
		return nil, err
	}
	// O_NONBLOCK: opening a FIFO doesn't wait for a writer. O_NOFOLLOW: the
	// last component was just resolved, a link there now is a swap.
	f, err := os.OpenFile(target, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReadBytes {
		return nil, fmt.Errorf("%s is larger than %d MB, too large to read through Flint", path, maxReadBytes>>20)
	}
	return data, nil
}

// writeConfined saves an agent's fs/write_text_file. Like the editor's save
// it follows symlinks and keeps the file's permissions, but the target is
// checked against root again right before writing, only regular files are
// replaced, and the new content always goes to a temporary file that is
// renamed into place: writing in place through a hard link would change the
// file's other names too, which may be outside the project.
func writeConfined(root, path, content string) error {
	target, err := resolveInside(root, path)
	if err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	info, err := os.Lstat(target)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return fmt.Errorf("not a regular file: %s", path)
	case err == nil:
		perm = info.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	return config.WriteFileAtomic(target, []byte(content), perm)
}

// resolveInside resolves the symlinks in path and refuses a result outside
// root. What remains between this check and the use is a race with the agent
// itself swapping a directory for a link, which an agent that can do that
// could exploit with its own tools anyway.
func resolveInside(root, path string) (string, error) {
	target := as.Resolve(path)
	if !as.Inside(target, root) {
		return "", fmt.Errorf("access outside the project denied: %s", path)
	}
	return target, nil
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
