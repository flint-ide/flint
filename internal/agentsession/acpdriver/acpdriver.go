// Package acpdriver adapts agents that speak the Agent Client Protocol natively
// (no npm adapters involved) to Flint's agent-neutral session model.
package acpdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"flint/internal/acp"
	as "flint/internal/agentsession"
	"flint/internal/workspace"
)

// Options configure how the ACP agent process is launched.
type Options struct {
	Name    string // e.g. "gemini"; shown as SessionInfo.Agent
	Command string
	Args    []string
	Env     []string
}

// Driver starts one ACP agent process per session.
type Driver struct {
	opts Options

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	id     string
	cwd    string
	client *acp.Client
	sink   as.Sink

	sinkMu sync.Mutex // serialises sink calls (updates and permissions arrive on different goroutines)

	permMu  sync.Mutex
	pending map[string]chan string // permission request id → chosen option id
	nextReq atomic.Int64
}

// New creates a driver for an ACP agent binary.
func New(opts Options) *Driver {
	return &Driver{opts: opts, sessions: map[string]*session{}}
}

// Start launches the agent and opens a session in cwd.
func (d *Driver) Start(ctx context.Context, cwd string, sink as.Sink) (as.SessionInfo, error) {
	s := &session{cwd: cwd, sink: sink, pending: map[string]chan string{}}
	client, err := acp.Start(ctx, acp.Options{
		Command: d.opts.Command, Args: d.opts.Args, Dir: cwd, Env: d.opts.Env,
		ClientName: "flint", ClientVersion: "0.1",
	}, &handler{s: s})
	if err != nil {
		return as.SessionInfo{}, err
	}
	res, err := client.NewSession(ctx, cwd, []json.RawMessage{})
	if err != nil {
		_ = client.Close()
		return as.SessionInfo{}, err
	}
	s.id, s.client = res.SessionID, client
	d.mu.Lock()
	d.sessions[s.id] = s
	d.mu.Unlock()
	info := as.SessionInfo{ID: s.id, Agent: d.opts.Name, Cwd: cwd}
	if ai := client.Info().AgentInfo; ai != nil && info.Agent == "" {
		info.Agent = ai.Name
	}
	return info, nil
}

// Prompt sends text plus attachments and waits for the turn to end.
func (d *Driver) Prompt(ctx context.Context, sessionID, text string, attachments []as.Attachment) (string, error) {
	s, err := d.get(sessionID)
	if err != nil {
		return "", err
	}
	blocks := []acp.ContentBlock{acp.TextBlock(text)}
	for _, a := range attachments {
		uri := "file://" + a.Path
		if a.Text != "" {
			blocks = append(blocks, acp.TextResourceBlock(uri, "text/plain", a.Text))
		} else {
			blocks = append(blocks, acp.ResourceLinkBlock(uri, a.Path))
		}
	}
	stop, err := s.client.Prompt(ctx, sessionID, blocks)
	if err != nil {
		s.emit(as.Event{Kind: as.KindError, Text: err.Error()})
		s.emit(as.Event{Kind: as.KindTurnEnd, StopReason: as.StopError})
		return as.StopError, err
	}
	s.emit(as.Event{Kind: as.KindTurnEnd, StopReason: stop})
	return stop, nil
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

// Stop ends one session and its agent process.
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

func (s *session) emit(e as.Event) {
	if e.SessionID == "" {
		e.SessionID = s.id
	}
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	s.sink(e)
}

// handler implements acp.Handler for one session.
type handler struct{ s *session }

func (h *handler) SessionUpdate(sessionID string, update json.RawMessage) {
	for _, e := range translate(update) {
		e.SessionID = sessionID
		h.s.emit(e)
	}
}

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
	path, err := h.confine(req.SessionID, req.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return acp.ApplyLineWindow(string(data), req), nil
}

func (h *handler) WriteTextFile(_ context.Context, req acp.WriteTextFileRequest) error {
	path, err := h.confine(req.SessionID, req.Path)
	if err != nil {
		return err
	}
	// Same save path as the editor: follows symlinks, keeps permissions.
	return workspace.WriteFile(path, req.Content)
}

// confine accepts only absolute paths of this session that stay inside its cwd
// (symlinks resolved), so a misbehaving agent can't read or write elsewhere.
func (h *handler) confine(sessionID, path string) (string, error) {
	if sessionID != h.s.id {
		return "", fmt.Errorf("unknown session %q", sessionID)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %q", path)
	}
	clean := filepath.Clean(path)
	if !as.Inside(clean, h.s.cwd) {
		return "", fmt.Errorf("access outside the project denied: %s", clean)
	}
	return clean, nil
}
