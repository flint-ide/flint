package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"flint/internal/agentsession"
)

// Wire shapes of `claude -p --input-format stream-json --output-format
// stream-json`, as observed from Claude Code 2.1.x (see testdata/*.jsonl).
// Every stdout line is one JSON object with a "type":
//
//	system           subtype "init" (once per turn: model, tools, cwd, ...),
//	                 "status", "task_summary", "post_turn_summary", ...
//	stream_event     raw Messages API stream events (--include-partial-messages)
//	assistant        one complete content block of an assistant message
//	user             tool results (and interruption markers)
//	result           end of a turn: stop_reason, usage, cost, errors
//	control_request  the CLI asks the host (subtype "can_use_tool")
//	control_response the CLI answers a host control_request
//	control_cancel_request  the CLI withdraws one of its control_requests
//	rate_limit_event and others, ignored here
const (
	msgSystem        = "system"
	msgStreamEvent   = "stream_event"
	msgAssistant     = "assistant"
	msgUser          = "user"
	msgResult        = "result"
	msgControlReq    = "control_request"
	msgControlResp   = "control_response"
	msgControlCancel = "control_cancel_request"

	subtypeInit        = "init"
	subtypeCanUseTool  = "can_use_tool"
	subtypeInitialize  = "initialize"
	subtypeInterrupt   = "interrupt"
	subtypeSuccess     = "success"
	subtypeError       = "error"
	behaviorAllow      = "allow"
	behaviorDeny       = "deny"
	suggestionAddRules = "addRules"
)

// inbound is the envelope of every stdout line. Type-specific payloads stay
// raw and are decoded by the handler for that type, so an unexpected shape in
// one field never drops the whole line.
type inbound struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Model           string          `json:"model"`          // system/init
	APIMessageID    string          `json:"api_message_id"` // stream_event
	Event           json.RawMessage `json:"event"`          // stream_event
	Message         json.RawMessage `json:"message"`        // assistant, user
	ToolUseResult   json.RawMessage `json:"tool_use_result"`
	RequestID       string          `json:"request_id"` // control_request, control_cancel_request
	Request         json.RawMessage `json:"request"`
	Response        json.RawMessage `json:"response"` // control_response
}

// fromSubagent reports whether the line belongs to a subagent (Task/Agent
// tool) rather than the main conversation.
func (m *inbound) fromSubagent() bool {
	return m.ParentToolUseID != nil && *m.ParentToolUseID != ""
}

// streamEvent is the subset of a Messages API stream event used here.
type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID string `json:"id"`
	} `json:"message"`
	Delta *struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
}

// apiMessage is the "message" of assistant and user lines.
type apiMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []contentBlock
}

// blocks decodes Content when it is an array of blocks.
func (m *apiMessage) blocks() []contentBlock {
	var out []contentBlock
	if len(m.Content) == 0 || m.Content[0] != '[' {
		return nil
	}
	if err := json.Unmarshal(m.Content, &out); err != nil {
		return nil
	}
	return out
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`    // tool_use
	Name      string          `json:"name"`  // tool_use
	Input     json.RawMessage `json:"input"` // tool_use
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // tool_result: string or []block
	IsError   bool            `json:"is_error"`
	ToolName  string          `json:"tool_name"` // tool_reference
}

// resultMsg is a "result" line.
type resultMsg struct {
	Subtype      string          `json:"subtype"`
	IsError      bool            `json:"is_error"`
	StopReason   *string         `json:"stop_reason"`
	Result       json.RawMessage `json:"result"`
	Errors       json.RawMessage `json:"errors"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	Usage        *struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		// Iterations has one entry per model call of the turn; the last one's
		// input is the context fill (the totals above sum all calls).
		Iterations []struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"iterations"`
	} `json:"usage"`
	ModelUsage                map[string]modelUsage `json:"modelUsage"`
	ModelUsageSinceLastResult map[string]modelUsage `json:"model_usage_since_last_result"`
}

// modelUsage is one model's entry in a result's modelUsage maps.
type modelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
}

// errorText summarises why a failed result failed.
func (r *resultMsg) errorText() string {
	var errs []string
	if json.Unmarshal(r.Errors, &errs) == nil && len(errs) > 0 {
		return strings.Join(errs, "; ")
	}
	var s string
	if json.Unmarshal(r.Result, &s) == nil && strings.TrimSpace(s) != "" {
		return s
	}
	return "turn failed (" + r.Subtype + ")"
}

// controlRequestBody is the "request" of a CLI control_request.
type controlRequestBody struct {
	Subtype              string            `json:"subtype"`
	ToolName             string            `json:"tool_name"`
	DisplayName          string            `json:"display_name"`
	Input                json.RawMessage   `json:"input"`
	Description          string            `json:"description"`
	PermissionSuggestion []json.RawMessage `json:"permission_suggestions"`
	BlockedPath          string            `json:"blocked_path"`
	ToolUseID            string            `json:"tool_use_id"`
}

// controlResponseBody is the "response" of a control_response, in both
// directions.
type controlResponseBody struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// Outbound messages (stdin).

type outUserMessage struct {
	Type            string      `json:"type"` // "user"
	Message         userContent `json:"message"`
	ParentToolUseID *string     `json:"parent_tool_use_id"`
	SessionID       string      `json:"session_id"`
}

type userContent struct {
	Role    string      `json:"role"` // "user"
	Content []textBlock `json:"content"`
}

type textBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

type outControlRequest struct {
	Type      string `json:"type"` // "control_request"
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

type subtypeOnly struct {
	Subtype string `json:"subtype"`
}

type outControlResponse struct {
	Type     string `json:"type"` // "control_response"
	Response any    `json:"response"`
}

type outControlResult struct {
	Subtype   string `json:"subtype"` // "success" | "error"
	RequestID string `json:"request_id"`
	Response  any    `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

// permissionAllow / permissionDeny are can_use_tool answers.
type permissionAllow struct {
	Behavior           string            `json:"behavior"` // "allow"
	UpdatedInput       json.RawMessage   `json:"updatedInput"`
	UpdatedPermissions []json.RawMessage `json:"updatedPermissions,omitempty"`
}

type permissionDeny struct {
	Behavior string `json:"behavior"` // "deny"
	Message  string `json:"message"`
}

// rejectMessage is what the model is told when the user rejects a tool call.
const rejectMessage = "The user rejected this tool call."

// buildUserMessage renders a prompt and its attachments as a stream-json user
// message: the prompt first, then one text block per attachment.
func buildUserMessage(text string, attachments []agentsession.Attachment) (outUserMessage, error) {
	var blocks []textBlock
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, textBlock{Type: "text", Text: text})
	}
	for _, a := range attachments {
		if a.Path == "" && a.Text == "" {
			continue
		}
		blocks = append(blocks, textBlock{Type: "text", Text: formatAttachment(a)})
	}
	if len(blocks) == 0 {
		return outUserMessage{}, fmt.Errorf("claude: empty prompt")
	}
	return outUserMessage{
		Type:    msgUser,
		Message: userContent{Role: "user", Content: blocks},
	}, nil
}

func formatAttachment(a agentsession.Attachment) string {
	if a.Text == "" {
		return "Relevant file: " + a.Path
	}
	header := a.Path
	if a.StartLine > 0 {
		end := max(a.EndLine, a.StartLine)
		header = fmt.Sprintf("%s:%d-%d", a.Path, a.StartLine, end)
	}
	fence := codeFence(a.Text)
	return header + "\n" + fence + "\n" + strings.TrimSuffix(a.Text, "\n") + "\n" + fence
}

// codeFence returns a backtick fence longer than any backtick run in text.
func codeFence(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	return strings.Repeat("`", max(3, longest+1))
}

// alwaysAllowRules picks the permission updates sent with "Always allow":
// the CLI's own rule suggestions (e.g. Bash(cmd) for localSettings) when it
// offers any, otherwise all of its suggestions (e.g. setMode acceptEdits for
// file edits, addDirectories for paths outside the workspace).
func alwaysAllowRules(suggestions []json.RawMessage) []json.RawMessage {
	var rules []json.RawMessage
	for _, s := range suggestions {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(s, &head) == nil && head.Type == suggestionAddRules {
			rules = append(rules, s)
		}
	}
	if len(rules) > 0 {
		return rules
	}
	return suggestions
}
