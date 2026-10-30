package acp

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC / ACP error codes (ErrorCode in the schema).
const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
	CodeAuthRequired     = -32000
	CodeResourceNotFound = -32002
)

const jsonrpcVersion = "2.0"

// RPCError is a JSON-RPC error object, either received from the agent or
// returned by this client to the agent.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements error.
func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("rpc error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// newRPCError builds an error with optional data; data that fails to
// marshal is dropped rather than failing the reply.
func newRPCError(code int, msg string, data any) *RPCError {
	e := &RPCError{Code: code, Message: msg}
	if data != nil {
		if raw, err := json.Marshal(data); err == nil {
			e.Data = raw
		}
	}
	return e
}

func errMethodNotFound(method string) *RPCError {
	return newRPCError(CodeMethodNotFound, "Method not found", map[string]string{"method": method})
}

func errInvalidParams(reason string) *RPCError {
	return newRPCError(CodeInvalidParams, "Invalid params: "+reason, nil)
}

// inboundMessage is any message read from the agent. Which fields are set
// determines its kind (request, notification or response).
type inboundMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// hasID reports whether the message carries a usable (non-null) id.
func (m *inboundMessage) hasID() bool {
	return len(m.ID) > 0 && string(m.ID) != "null"
}

type outboundRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outboundNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outboundResult struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type outboundError struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *RPCError       `json:"error"`
}
