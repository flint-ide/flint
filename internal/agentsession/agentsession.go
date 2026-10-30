// Package agentsession is the agent-neutral model of a coding-agent conversation.
// Drivers (Claude Code stream-json, Codex app-server, ACP agents) translate their
// native protocols into these events so Flint has one agent panel for all of them.
// The shapes deliberately mirror ACP's session/update so ACP maps 1:1.
package agentsession

import "context"

// Event kinds.
const (
	KindMessageChunk      = "message_chunk"      // assistant text (streamed)
	KindThoughtChunk      = "thought_chunk"      // reasoning / thinking text
	KindUserMessage       = "user_message"       // echo of what was sent (e.g. replayed history)
	KindToolCall          = "tool_call"          // a tool call started
	KindToolCallUpdate    = "tool_call_update"   // status/output/diff of an existing tool call
	KindPlan              = "plan"               // the agent's current plan (full replacement)
	KindPermission        = "permission_request" // the agent asks before doing something
	KindPermissionSettled = "permission_settled" // a pending request was answered or withdrawn
	KindTurnEnd           = "turn_end"           // the prompt turn finished
	KindError             = "error"              // non-fatal problem worth showing
	KindUsage             = "usage"              // token / cost accounting, if the agent reports it
)

// Tool kinds (ACP vocabulary) drive the icon and how the UI renders details.
const (
	ToolRead    = "read"
	ToolEdit    = "edit"
	ToolDelete  = "delete"
	ToolMove    = "move"
	ToolSearch  = "search"
	ToolExecute = "execute"
	ToolThink   = "think"
	ToolFetch   = "fetch"
	ToolOther   = "other"
)

// Tool call statuses.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

// Permission option kinds.
const (
	AllowOnce    = "allow_once"
	AllowAlways  = "allow_always"
	RejectOnce   = "reject_once"
	RejectAlways = "reject_always"
)

// Stop reasons for KindTurnEnd.
const (
	StopEndTurn   = "end_turn"
	StopCancelled = "cancelled"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
	StopError     = "error"
)

// Event is one update streamed to the UI. Only the fields relevant to Kind are set.
type Event struct {
	SessionID string `json:"sessionId"`
	// Seq numbers events per session (from 1) so the UI can restore order when the
	// transport delivers them out of order (Wails' dev server does).
	Seq        uint64             `json:"seq"`
	Kind       string             `json:"kind"`
	Text       string             `json:"text,omitempty"`
	ToolCall   *ToolCall          `json:"toolCall,omitempty"`
	Plan       []PlanEntry        `json:"plan,omitempty"`
	Permission *PermissionRequest `json:"permission,omitempty"`
	// RequestID identifies the permission request a KindPermissionSettled refers to.
	RequestID  string `json:"requestId,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	Usage      *Usage `json:"usage,omitempty"`
}

// ToolCall describes something the agent does. Updates carry the ID plus changed fields.
type ToolCall struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status,omitempty"`
	// Paths touched by the call (absolute when the agent provides them).
	Paths []string `json:"paths,omitempty"`
	// Input is the raw tool input, pretty JSON or a command line, for the details view.
	Input string `json:"input,omitempty"`
	// Output is (possibly truncated) textual output, e.g. a command's stdout.
	Output string `json:"output,omitempty"`
	// Diffs are file edits proposed or made by the call.
	Diffs []Diff `json:"diffs,omitempty"`
}

// Diff is a file edit. OldText is empty for new files.
type Diff struct {
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// PlanEntry is one item of the agent's plan / todo list.
type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`   // pending | in_progress | completed
	Priority string `json:"priority"` // high | medium | low (optional)
}

// PermissionRequest asks the user (or an auto-approve policy) to decide.
type PermissionRequest struct {
	ID       string             `json:"id"`
	ToolCall ToolCall           `json:"toolCall"`
	Options  []PermissionOption `json:"options"`
}

// PermissionOption is one answer to a PermissionRequest.
type PermissionOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // AllowOnce | AllowAlways | RejectOnce | RejectAlways
}

// Usage reports consumption, when known.
type Usage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
}

// Attachment is extra context sent with a prompt (a file or a selection).
type Attachment struct {
	Path string `json:"path"`
	// Text, when set, is the attached content (e.g. the selected lines); otherwise
	// the agent is told about the file and reads it itself.
	Text      string `json:"text,omitempty"`
	StartLine int    `json:"startLine,omitempty"` // 1-based, for selections
	EndLine   int    `json:"endLine,omitempty"`
}

// SessionInfo is returned when a session starts.
type SessionInfo struct {
	ID    string `json:"id"`
	Agent string `json:"agent"` // e.g. "claude", "codex"
	Model string `json:"model,omitempty"`
	Cwd   string `json:"cwd"`
}

// Sink receives events; drivers must call it sequentially per session (in order).
type Sink func(Event)

// Driver runs one agent backend. Implementations must be safe for concurrent use.
type Driver interface {
	// Start opens a new conversation in cwd. Events for it are delivered to sink.
	Start(ctx context.Context, cwd string, sink Sink) (SessionInfo, error)
	// Prompt sends a user message and blocks until the turn ends (a KindTurnEnd event
	// is emitted before it returns). Cancelling ctx cancels the turn.
	Prompt(ctx context.Context, sessionID, text string, attachments []Attachment) (stopReason string, err error)
	// Cancel interrupts the running turn, if any.
	Cancel(sessionID string) error
	// Respond answers a pending permission request with one of its option IDs.
	Respond(sessionID, requestID, optionID string) error
	// Stop ends a session and its agent process.
	Stop(sessionID string) error
	// Close stops every session.
	Close() error
}

// Resumer is implemented by drivers that can reopen a conversation from an
// earlier run (so a restarted Flint continues where it left off).
type Resumer interface {
	// Resume reopens session sessionID in cwd. The returned ID may differ when
	// the agent forks the conversation.
	Resume(ctx context.Context, cwd, sessionID string, sink Sink) (SessionInfo, error)
}
