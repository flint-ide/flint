// Package acp implements the client (editor) side of the Agent Client
// Protocol (https://agentclientprotocol.com).
//
// Flint launches a coding-agent adapter (claude-agent-acp, codex-acp, ...) as a
// subprocess and talks JSON-RPC 2.0 to it over stdio: one JSON message per
// line, UTF-8, no embedded newlines. The agent's stderr is free-form logging
// and is kept in a bounded ring buffer for diagnostics.
//
// The wire shapes follow the stable ACP v1 schema shipped with
// @agentclientprotocol/sdk (schema/v1/schema.json).
package acp

// ProtocolVersion is the latest stable ACP protocol version this client
// speaks. It is sent in `initialize`; an agent answering with any other
// version is rejected.
const ProtocolVersion = 1

// Agent methods (client → agent).
const (
	methodInitialize   = "initialize"
	methodAuthenticate = "authenticate"
	methodSessionNew   = "session/new"
	methodSetMode      = "session/set_mode"
	methodPrompt       = "session/prompt"
	methodCancel       = "session/cancel"
)

// Client methods (agent → client).
const (
	methodSessionUpdate     = "session/update"
	methodRequestPermission = "session/request_permission"
	methodReadTextFile      = "fs/read_text_file"
	methodWriteTextFile     = "fs/write_text_file"
)

// methodCancelRequest is the protocol-level `$/cancel_request` notification,
// valid in both directions.
const methodCancelRequest = "$/cancel_request"

// Stop reasons returned by Prompt (StopReason in the schema).
const (
	StopEndTurn         = "end_turn"
	StopMaxTokens       = "max_tokens"
	StopMaxTurnRequests = "max_turn_requests"
	StopRefusal         = "refusal"
	StopCancelled       = "cancelled"
)
