package codex

import (
	"encoding/json"
	"strings"

	"flint/internal/agentsession"
)

// pendingRequest is an approval waiting for Respond.
type pendingRequest struct {
	id       json.RawMessage
	key      string
	onCancel any                      // reply sent when the turn is cancelled
	answer   func(string) (any, bool) // reply for an option id
}

// decisionOptions are offered for command and file-change approvals. The
// option ids are the protocol's decision values. "cancel" (deny and abort
// the turn) is reached through Cancel instead.
var decisionOptions = []agentsession.PermissionOption{
	{ID: decisionAccept, Name: "Allow", Kind: agentsession.AllowOnce},
	{ID: decisionAcceptForSession, Name: "Allow for this session", Kind: agentsession.AllowAlways},
	{ID: decisionDecline, Name: "Deny", Kind: agentsession.RejectOnce},
}

// permissionOptions are offered for additional sandbox permissions.
var permissionOptions = []agentsession.PermissionOption{
	{ID: "turn", Name: "Allow for this turn", Kind: agentsession.AllowOnce},
	{ID: "session", Name: "Allow for this session", Kind: agentsession.AllowAlways},
	{ID: "deny", Name: "Deny", Kind: agentsession.RejectOnce},
}

// handleRequest serves a server → client request on the dispatcher goroutine.
func (s *session) handleRequest(id json.RawMessage, method string, params json.RawMessage) {
	c := s.conn()
	if c == nil {
		return
	}
	switch method {
	case reqCommandApproval:
		s.commandApproval(id, params)
	case reqFileApproval:
		s.fileApproval(id, params)
	case reqPermissions:
		s.permissionsApproval(id, params)
	case reqUserInput:
		_ = c.reply(id, map[string]any{"answers": map[string]any{}})
	case reqElicitation:
		_ = c.reply(id, map[string]any{"action": "decline", "content": nil, "_meta": nil})
	case reqDynamicTool:
		_ = c.reply(id, map[string]any{"contentItems": []any{}, "success": false})
	default:
		_ = c.replyError(id, codeMethodNotFound, "Method not found: "+method)
	}
}

func (s *session) commandApproval(id, params json.RawMessage) {
	var p commandApprovalParams
	if !decode(params, &p) {
		_ = s.conn().replyError(id, codeInvalidParams, "Invalid params")
		return
	}
	fallback := agentsession.ToolCall{ID: p.ItemID, Kind: agentsession.ToolExecute}
	if p.Command != "" {
		cmd := displayCommand(p.Command)
		fallback.Title, fallback.Input = "Run: "+cmd, cmd
	}
	tc := s.knownCall(p.ItemID, fallback)
	// The request type decides the kind, never the item it points at: a command
	// approval must not inherit "edit"/"read" and slip through auto-approval.
	tc.Kind = agentsession.ToolExecute
	if p.Reason != "" {
		tc.Input = joinNonEmpty(tc.Input, "Reason: "+p.Reason)
	}
	s.ask(id, tc, decisionOptions, decisionResponse{Decision: decisionCancel}, decisionAnswer)
}

func (s *session) fileApproval(id, params json.RawMessage) {
	var p fileApprovalParams
	if !decode(params, &p) {
		_ = s.conn().replyError(id, codeInvalidParams, "Invalid params")
		return
	}
	tc := s.knownCall(p.ItemID, agentsession.ToolCall{ID: p.ItemID, Kind: agentsession.ToolEdit, Title: "Edit files"})
	tc.Kind = agentsession.ToolEdit
	if p.Reason != "" {
		tc.Input = joinNonEmpty(tc.Input, "Reason: "+p.Reason)
	}
	if p.GrantRoot != "" {
		// Widening the sandbox lasts for the whole session: always ask the user.
		tc.Kind = agentsession.ToolOther
		tc.Title = "Allow writes under " + p.GrantRoot
		tc.Input = joinNonEmpty(tc.Input, "Allow writes under: "+p.GrantRoot)
	}
	s.ask(id, tc, decisionOptions, decisionResponse{Decision: decisionCancel}, decisionAnswer)
}

func (s *session) permissionsApproval(id, params json.RawMessage) {
	var p permissionsApprovalParams
	if !decode(params, &p) {
		_ = s.conn().replyError(id, codeInvalidParams, "Invalid params")
		return
	}
	granted := map[string]json.RawMessage{}
	for k, v := range p.Permissions {
		if len(v) > 0 && string(v) != "null" {
			granted[k] = v
		}
	}
	raw, _ := json.Marshal(granted)
	tc := agentsession.ToolCall{
		ID:    p.ItemID,
		Kind:  agentsession.ToolOther,
		Title: orDefault(p.Reason, "Grant additional permissions"),
		Input: prettyJSON(raw),
	}
	deny := permissionsResponse{Permissions: map[string]json.RawMessage{}, Scope: "turn"}
	answer := func(option string) (any, bool) {
		switch option {
		case "turn", "session":
			return permissionsResponse{Permissions: granted, Scope: option}, true
		case "deny":
			return deny, true
		}
		return nil, false
	}
	s.ask(id, tc, permissionOptions, deny, answer)
}

func decisionAnswer(option string) (any, bool) {
	switch option {
	case decisionAccept, decisionAcceptForSession, decisionDecline, decisionCancel:
		return decisionResponse{Decision: option}, true
	}
	return nil, false
}

// knownCall returns the tool call announced for itemID (so the permission
// shows the same title/diffs), or fallback.
func (s *session) knownCall(itemID string, fallback agentsession.ToolCall) agentsession.ToolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.items[itemID]; st != nil && st.announce {
		tc := st.call
		tc.Diffs = append([]agentsession.Diff(nil), st.call.Diffs...)
		tc.Paths = append([]string(nil), st.call.Paths...)
		return tc
	}
	return fallback
}

// ask registers a pending approval and emits KindPermission, unless the
// turn is already being cancelled.
func (s *session) ask(id json.RawMessage, tc agentsession.ToolCall, options []agentsession.PermissionOption, onCancel any, answer func(string) (any, bool)) {
	key := requestKey(id)
	s.mu.Lock()
	if t := s.turn; t != nil && t.cancelRequested {
		s.mu.Unlock()
		_ = s.conn().reply(id, onCancel)
		return
	}
	s.pending[key] = &pendingRequest{id: id, key: key, onCancel: onCancel, answer: answer}
	s.mu.Unlock()
	opts := append([]agentsession.PermissionOption(nil), options...)
	s.emit(agentsession.Event{
		Kind:       agentsession.KindPermission,
		Permission: &agentsession.PermissionRequest{ID: key, ToolCall: tc, Options: opts},
	})
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}
