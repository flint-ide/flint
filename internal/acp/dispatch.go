package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// errTurnCancelled is the context cause used when a prompt turn is
// cancelled while a permission request is pending.
var errTurnCancelled = errors.New("acp: prompt turn cancelled")

// serveRequest routes agent → client requests. Methods we do not advertise
// (terminal/*, elicitation/*, extensions) get -32601.
func (c *Client) serveRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case methodRequestPermission:
		return c.servePermission(ctx, params)
	case methodReadTextFile:
		return c.serveReadTextFile(ctx, params)
	case methodWriteTextFile:
		return c.serveWriteTextFile(ctx, params)
	default:
		return nil, errMethodNotFound(method)
	}
}

// serveNotification forwards `session/update`; other notifications
// (including unknown extension ones) are ignored, as JSON-RPC requires.
func (c *Client) serveNotification(method string, params json.RawMessage) {
	if method != methodSessionUpdate {
		return
	}
	var n sessionNotification
	if err := json.Unmarshal(params, &n); err != nil || n.SessionID == "" || len(n.Update) == 0 {
		return
	}
	c.h.SessionUpdate(n.SessionID, n.Update)
}

func (c *Client) servePermission(ctx context.Context, params json.RawMessage) (any, error) {
	var req PermissionRequest
	if err := decodeParams(params, &req); err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, errInvalidParams(err.Error())
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	release, alreadyCancelled := c.turns.track(req.SessionID, cancel)
	defer release()
	if alreadyCancelled {
		return permissionResponse{Outcome: Cancelled()}, nil
	}

	outcome, err := c.h.RequestPermission(ctx, req)
	if errors.Is(context.Cause(ctx), errTurnCancelled) {
		return permissionResponse{Outcome: Cancelled()}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("request permission: %w", err)
	}
	if err := req.checkOutcome(outcome); err != nil {
		return nil, err
	}
	return permissionResponse{Outcome: outcome}, nil
}

func (c *Client) serveReadTextFile(ctx context.Context, params json.RawMessage) (any, error) {
	var req ReadTextFileRequest
	if err := decodeParams(params, &req); err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, errInvalidParams(err.Error())
	}
	content, err := c.h.ReadTextFile(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", req.Path, err)
	}
	return readTextFileResponse{Content: content}, nil
}

func (c *Client) serveWriteTextFile(ctx context.Context, params json.RawMessage) (any, error) {
	var req WriteTextFileRequest
	if err := decodeParams(params, &req); err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, errInvalidParams(err.Error())
	}
	if err := c.h.WriteTextFile(ctx, req); err != nil {
		return nil, fmt.Errorf("write %s: %w", req.Path, err)
	}
	return writeTextFileResponse{}, nil
}

func decodeParams(params json.RawMessage, v any) error {
	if len(params) == 0 || string(params) == "null" {
		return errInvalidParams("missing params")
	}
	if err := json.Unmarshal(params, v); err != nil {
		return errInvalidParams(err.Error())
	}
	return nil
}

// turnRegistry tracks, per session, whether the current prompt turn was
// cancelled and which permission requests are waiting on the user.
type turnRegistry struct {
	mu       sync.Mutex
	nextID   uint64
	sessions map[string]*turnState
}

type turnState struct {
	cancelled bool
	pending   map[uint64]context.CancelCauseFunc
}

func newTurnRegistry() *turnRegistry {
	return &turnRegistry{sessions: map[string]*turnState{}}
}

func (r *turnRegistry) state(sessionID string) *turnState {
	s := r.sessions[sessionID]
	if s == nil {
		s = &turnState{pending: map[uint64]context.CancelCauseFunc{}}
		r.sessions[sessionID] = s
	}
	return s
}

// begin starts a new turn, clearing any previous cancellation.
func (r *turnRegistry) begin(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state(sessionID).cancelled = false
}

// end finishes a turn and forgets idle sessions.
func (r *turnRegistry) end(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state(sessionID)
	s.cancelled = false
	if len(s.pending) == 0 {
		delete(r.sessions, sessionID)
	}
}

// cancel marks the turn cancelled and returns the cancel funcs of its
// pending permission requests (invoked by the caller after notifying).
func (r *turnRegistry) cancel(sessionID string) []context.CancelCauseFunc {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state(sessionID)
	s.cancelled = true
	out := make([]context.CancelCauseFunc, 0, len(s.pending))
	for _, fn := range s.pending {
		out = append(out, fn)
	}
	return out
}

// track registers a pending permission request. It reports whether the
// turn is already cancelled (the request should be answered "cancelled"
// without asking the user); release must be called when it completes.
func (r *turnRegistry) track(sessionID string, cancel context.CancelCauseFunc) (release func(), cancelled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state(sessionID)
	r.nextID++
	id := r.nextID
	s.pending[id] = cancel
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(s.pending, id)
		if len(s.pending) == 0 && !s.cancelled && r.sessions[sessionID] == s {
			delete(r.sessions, sessionID)
		}
	}, s.cancelled
}
