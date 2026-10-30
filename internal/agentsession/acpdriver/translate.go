package acpdriver

import (
	"encoding/json"
	"strings"

	as "flint/internal/agentsession"
)

const maxOutput = 16 << 10

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolCallContent is ToolCallContent: {type:"content",content} | {type:"diff",path,oldText,newText}.
type toolCallContent struct {
	Type    string        `json:"type"`
	Content *contentBlock `json:"content"`
	Path    string        `json:"path"`
	OldText *string       `json:"oldText"`
	NewText string        `json:"newText"`
}

type wireToolCall struct {
	ToolCallID string            `json:"toolCallId"`
	Title      string            `json:"title"`
	Kind       string            `json:"kind"`
	Status     string            `json:"status"`
	Content    []toolCallContent `json:"content"`
	Locations  []struct {
		Path string `json:"path"`
	} `json:"locations"`
	RawInput json.RawMessage `json:"rawInput"`
}

// translate maps one ACP session/update to Flint events (empty for unknown variants).
// The "content" field is a ContentBlock for chunks but an array for tool calls, so
// the discriminator is decoded first.
func translate(raw json.RawMessage) []as.Event {
	var head struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil
	}
	switch head.SessionUpdate {
	case "agent_message_chunk", "agent_thought_chunk", "user_message_chunk":
		var chunk struct {
			Content *contentBlock `json:"content"`
		}
		_ = json.Unmarshal(raw, &chunk)
		kind := map[string]string{
			"agent_message_chunk": as.KindMessageChunk,
			"agent_thought_chunk": as.KindThoughtChunk,
			"user_message_chunk":  as.KindUserMessage,
		}[head.SessionUpdate]
		return textEvent(kind, chunk.Content)
	case "plan":
		var p struct {
			Entries []as.PlanEntry `json:"entries"`
		}
		_ = json.Unmarshal(raw, &p)
		return []as.Event{{Kind: as.KindPlan, Plan: p.Entries}}
	case "tool_call":
		tc := toolCallFromRaw(raw)
		return []as.Event{{Kind: as.KindToolCall, ToolCall: &tc}}
	case "tool_call_update":
		tc := toolCallFromRaw(raw)
		return []as.Event{{Kind: as.KindToolCallUpdate, ToolCall: &tc}}
	}
	return nil
}

func textEvent(kind string, c *contentBlock) []as.Event {
	if c == nil || c.Type != "text" || c.Text == "" {
		return nil
	}
	return []as.Event{{Kind: kind, Text: c.Text}}
}

// toolCallFromRaw converts an ACP ToolCall / ToolCallUpdate object.
func toolCallFromRaw(raw json.RawMessage) as.ToolCall {
	var w wireToolCall
	_ = json.Unmarshal(raw, &w)
	tc := as.ToolCall{ID: w.ToolCallID, Title: w.Title, Kind: w.Kind, Status: w.Status}
	for _, l := range w.Locations {
		if l.Path != "" {
			tc.Paths = append(tc.Paths, l.Path)
		}
	}
	if len(w.RawInput) > 0 && string(w.RawInput) != "null" {
		tc.Input = prettyJSON(w.RawInput)
	}
	var out strings.Builder
	for _, c := range w.Content {
		switch c.Type {
		case "diff":
			d := as.Diff{Path: c.Path, NewText: c.NewText}
			if c.OldText != nil {
				d.OldText = *c.OldText
			}
			tc.Diffs = append(tc.Diffs, d)
		case "content":
			if c.Content != nil && c.Content.Type == "text" {
				out.WriteString(c.Content.Text)
			}
		}
	}
	tc.Output = truncate(out.String())
	return tc
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return truncate(string(b))
}

func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n…(truncated)"
}
