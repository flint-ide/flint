package agentsession

import (
	"encoding/json"
	"slices"
)

// Tools of Flint's own MCP server that act (run tests, a run configuration,
// a request, a query) need the user's yes for every call. Flint's server asks
// by itself before it runs one; when the agent already asked in the chat (a
// permission card the user answered with "allow"), that answer counts, so the
// user isn't asked twice. This file is that hand-over: a driver marks the
// permission request of a Flint tool (PermissionRequest.FlintTool), the
// manager keeps such a request out of every auto-approval and "always allow",
// and tells the gate when the user allowed it.

// ActionGate connects the manager to Flint's MCP server.
type ActionGate struct {
	// Acts reports whether tool (a tool of Flint's server, by its own name)
	// acts right now.
	Acts func(tool string) bool
	// Approved is called when the user allowed one call of an acting tool in
	// the chat of a session in cwd, before the agent is told.
	Approved func(cwd, tool string, args json.RawMessage)
}

// maxPendingActions bounds the remembered requests (unanswered ones of
// sessions that never settle them).
const maxPendingActions = 64

type pendingAction struct {
	session, request string
	cwd, tool        string
	args             json.RawMessage
	allow            []string // option ids that allow the call
}

// SetActionGate installs the gate (before sessions start).
func (m *Manager) SetActionGate(g ActionGate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = g
}

// gateAction handles the permission request of a Flint tool that acts: it
// is never answered automatically, offers no "always allow", and is
// remembered until the user answers. ok is false for any other request.
func (m *Manager) gateAction(sessionID, cwd string, req PermissionRequest) (PermissionRequest, bool) {
	if req.FlintTool == "" {
		return req, false
	}
	m.mu.Lock()
	acts := m.gate.Acts
	m.mu.Unlock()
	if acts == nil || !acts(req.FlintTool) { // asked without the lock: it reads Flint's settings
		return req, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	req.Options = slices.DeleteFunc(slices.Clone(req.Options), func(o PermissionOption) bool { return o.Kind == AllowAlways })
	p := pendingAction{session: sessionID, request: req.ID, cwd: cwd, tool: req.FlintTool, args: slices.Clone(req.FlintArgs)}
	for _, o := range req.Options {
		if o.Kind == AllowOnce {
			p.allow = append(p.allow, o.ID)
		}
	}
	m.actions = slices.DeleteFunc(m.actions, func(a pendingAction) bool { return a.session == sessionID && a.request == req.ID })
	if len(m.actions) >= maxPendingActions {
		m.actions = slices.Delete(m.actions, 0, len(m.actions)-maxPendingActions+1)
	}
	m.actions = append(m.actions, p)
	return req, true
}

// settleAction forgets a remembered request; with the option the user chose
// it tells the gate about an allowed call.
func (m *Manager) settleAction(sessionID, requestID, optionID string) {
	m.mu.Lock()
	i := slices.IndexFunc(m.actions, func(a pendingAction) bool { return a.session == sessionID && a.request == requestID })
	if i < 0 {
		m.mu.Unlock()
		return
	}
	p := m.actions[i]
	m.actions = slices.Delete(m.actions, i, i+1)
	approved := m.gate.Approved
	m.mu.Unlock()
	if optionID != "" && approved != nil && slices.Contains(p.allow, optionID) {
		approved(p.cwd, p.tool, p.args)
	}
}

// dropActions forgets the remembered requests of a session that ended.
func (m *Manager) dropActions(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = slices.DeleteFunc(m.actions, func(a pendingAction) bool { return a.session == sessionID })
}
