package acp

import (
	"encoding/json"
	"slices"
)

// Implementation identifies a client or agent (clientInfo / agentInfo).
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// PromptCapabilities lists the content types an agent accepts in a prompt
// beyond the always-supported text and resource_link.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// MCPCapabilities lists the MCP transports the agent can connect to besides
// stdio.
type MCPCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AgentCapabilities is the agent's half of capability negotiation.
type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
	MCPCapabilities    MCPCapabilities    `json:"mcpCapabilities"`
	// SessionCapabilities is kept raw (list/delete/resume/close/...); this
	// client does not use those methods yet.
	SessionCapabilities json.RawMessage `json:"sessionCapabilities,omitempty"`
}

// AuthMethod is one way to authenticate with the agent. Type is empty for
// agent-handled methods and "terminal" for methods that need an interactive
// terminal; Raw keeps the full object (args/env for terminal methods).
type AuthMethod struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the raw object alongside the parsed fields.
func (a *AuthMethod) UnmarshalJSON(data []byte) error {
	type plain AuthMethod
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	p.Raw = slices.Clone(json.RawMessage(data))
	*a = AuthMethod(p)
	return nil
}

// InitializeResult is the agent's `initialize` response.
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
	AgentInfo         *Implementation   `json:"agentInfo,omitempty"`
	// Raw is the complete response, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// clone returns a deep copy so callers cannot mutate the client's copy.
func (r InitializeResult) clone() InitializeResult {
	out := r
	out.AgentCapabilities.SessionCapabilities = slices.Clone(r.AgentCapabilities.SessionCapabilities)
	out.AuthMethods = slices.Clone(r.AuthMethods)
	for i := range out.AuthMethods {
		out.AuthMethods[i].Raw = slices.Clone(out.AuthMethods[i].Raw)
	}
	if r.AgentInfo != nil {
		info := *r.AgentInfo
		out.AgentInfo = &info
	}
	out.Raw = slices.Clone(r.Raw)
	return out
}

// SessionMode is one operating mode an agent offers (e.g. "default", "plan").
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionModeState is the current mode plus the modes available.
type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// NewSessionResult is the agent's `session/new` response.
type NewSessionResult struct {
	SessionID string            `json:"sessionId"`
	Modes     *SessionModeState `json:"modes,omitempty"`
	// Raw is the complete response (configOptions etc.).
	Raw json.RawMessage `json:"-"`
}

type clientCapabilities struct {
	FS       fsCapabilities `json:"fs"`
	Terminal bool           `json:"terminal"`
}

type fsCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type initializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
	ClientInfo         Implementation     `json:"clientInfo"`
}

type authenticateParams struct {
	MethodID string `json:"methodId"`
}

type newSessionParams struct {
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

type promptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

type sessionParams struct {
	SessionID string `json:"sessionId"`
}

type setModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type sessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}
