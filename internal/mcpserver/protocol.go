package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"unicode/utf8"

	"flint/internal/redact"
)

// ProtocolVersion is the newest MCP revision the server speaks.
const ProtocolVersion = "2025-06-18"

// supportedVersions are the revisions whose tools API is the same; a client
// asking for another one gets ProtocolVersion and decides.
var supportedVersions = []string{ProtocolVersion, "2025-03-26", "2024-11-05"}

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// truncatedNote ends a tool answer that was cut at maxToolOutput.
const truncatedNote = "\n… (cut: answer too long; narrow the request)"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// handle answers one POSTed message: the response body (nil for none) and
// the HTTP status.
func (s *Server) handle(ctx context.Context, root string, body []byte) ([]byte, int) {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		// Batches were removed in 2025-06-18.
		return errorReply(nil, codeInvalidRequest, "batches are not supported"), http.StatusBadRequest
	}
	var msg message
	if err := json.Unmarshal(body, &msg); err != nil {
		return errorReply(nil, codeParseError, "parse error"), http.StatusBadRequest
	}
	if msg.JSONRPC != "2.0" {
		return errorReply(nil, codeInvalidRequest, `jsonrpc must be "2.0"`), http.StatusBadRequest
	}
	if msg.Method == "" {
		if msg.Result != nil || msg.Error != nil {
			return nil, http.StatusAccepted // a response to a request this server never sends
		}
		return errorReply(nil, codeInvalidRequest, "missing method"), http.StatusBadRequest
	}
	if msg.ID == nil {
		return nil, http.StatusAccepted // a notification (initialized, cancelled, ...)
	}
	if !validID(msg.ID) {
		return errorReply(nil, codeInvalidRequest, "id must be a string or a number"), http.StatusBadRequest
	}
	result, rerr := s.dispatch(ctx, root, msg.Method, msg.Params)
	if rerr != nil {
		return errorReply(msg.ID, rerr.Code, rerr.Message), http.StatusOK
	}
	out, err := json.Marshal(response{JSONRPC: "2.0", ID: msg.ID, Result: result})
	if err != nil {
		return errorReply(msg.ID, codeInternal, "encode result"), http.StatusOK
	}
	return out, http.StatusOK
}

func validID(id json.RawMessage) bool {
	var v any
	if json.Unmarshal(id, &v) != nil {
		return false
	}
	switch v.(type) {
	case string, float64:
		return true
	}
	return false
}

func errorReply(id json.RawMessage, code int, text string) []byte {
	if id == nil {
		id = json.RawMessage("null")
	}
	out, _ := json.Marshal(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: text}})
	return out
}

func (s *Server) dispatch(ctx context.Context, root, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return s.initialize(params)
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return s.listTools(), nil
	case "tools/call":
		return s.callTool(ctx, root, params)
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + cutName(method)}
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 && json.Unmarshal(params, &p) != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid initialize params"}
	}
	version := ProtocolVersion
	if slices.Contains(supportedVersions, p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	type serverInfo struct {
		Name    string `json:"name"`
		Title   string `json:"title"`
		Version string `json:"version"`
	}
	return struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      serverInfo     `json:"serverInfo"`
		Instructions    string         `json:"instructions,omitempty"`
	}{
		ProtocolVersion: version,
		Capabilities:    map[string]any{"tools": map[string]bool{"listChanged": false}},
		ServerInfo:      serverInfo{Name: "flint", Title: "Flint", Version: orDefault(s.opts.Version, "dev")},
		Instructions:    s.opts.Instructions,
	}, nil
}

type toolInfo struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations toolAnnotations `json:"annotations"`
}

type toolAnnotations struct {
	Title         string `json:"title,omitempty"`
	ReadOnly      bool   `json:"readOnlyHint"`
	Destructive   bool   `json:"destructiveHint"`
	Idempotent    bool   `json:"idempotentHint"`
	OpenWorldHint bool   `json:"openWorldHint"`
}

var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

func (s *Server) listTools() any {
	list := []toolInfo{}
	for _, t := range s.tools() {
		schema := t.InputSchema
		if schema == nil {
			schema = emptySchema
		}
		list = append(list, toolInfo{
			Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: schema,
			Annotations: toolAnnotations{Title: t.Title, ReadOnly: true, Idempotent: true},
		})
	}
	return map[string]any{"tools": list}
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

func (s *Server) callTool(ctx context.Context, root string, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid tools/call params"}
	}
	tools := s.tools()
	i := slices.IndexFunc(tools, func(t Tool) bool { return t.Name == p.Name })
	if i < 0 || p.Name == "" || tools[i].Call == nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + cutName(p.Name)}
	}
	tool := tools[i]
	if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
		p.Arguments = json.RawMessage("{}")
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	sem := s.sem(root) // per project: one busy project doesn't stall another
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return callResult{Content: []textContent{{Type: "text", Text: "Flint is busy; try again."}}, IsError: true}, nil
	}
	text, err := tool.Call(ctx, root, p.Arguments)
	if err != nil {
		text = err.Error()
	}
	return callResult{Content: []textContent{{Type: "text", Text: bound(redact.Secrets(text))}}, IsError: err != nil}, nil
}

// cutName bounds a tool name echoed in an error.
func cutName(name string) string {
	if len(name) > 64 {
		return name[:64] + "…"
	}
	return name
}

// bound cuts text at maxToolOutput bytes without splitting a character.
func bound(text string) string {
	if len(text) <= maxToolOutput {
		return text
	}
	cut := maxToolOutput
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + truncatedNote
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
