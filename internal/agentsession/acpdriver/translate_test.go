package acpdriver

import (
	"testing"

	as "flint/internal/agentsession"
)

func TestTranslate(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want func(t *testing.T, evs []as.Event)
	}{
		{"message chunk", `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}`,
			func(t *testing.T, evs []as.Event) {
				if len(evs) != 1 || evs[0].Kind != as.KindMessageChunk || evs[0].Text != "hi" {
					t.Errorf("events = %+v", evs)
				}
			}},
		{"thought chunk", `{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"hmm"}}`,
			func(t *testing.T, evs []as.Event) {
				if len(evs) != 1 || evs[0].Kind != as.KindThoughtChunk {
					t.Errorf("events = %+v", evs)
				}
			}},
		{"image chunk ignored", `{"sessionUpdate":"agent_message_chunk","content":{"type":"image","data":"x"}}`,
			func(t *testing.T, evs []as.Event) {
				if len(evs) != 0 {
					t.Errorf("events = %+v", evs)
				}
			}},
		{"plan", `{"sessionUpdate":"plan","entries":[{"content":"write tests","status":"pending","priority":"high"}]}`,
			func(t *testing.T, evs []as.Event) {
				if len(evs) != 1 || len(evs[0].Plan) != 1 || evs[0].Plan[0].Priority != "high" {
					t.Errorf("events = %+v", evs)
				}
			}},
		{"tool call with diff and output", `{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Edit a.go","kind":"edit","status":"pending",
			"locations":[{"path":"/p/a.go","line":3}],"rawInput":{"file":"a.go"},
			"content":[{"type":"diff","path":"/p/a.go","oldText":"x","newText":"y"},{"type":"content","content":{"type":"text","text":"ok"}}]}`,
			func(t *testing.T, evs []as.Event) {
				tc := evs[0].ToolCall
				if evs[0].Kind != as.KindToolCall || tc.ID != "t1" || tc.Kind != "edit" || tc.Paths[0] != "/p/a.go" {
					t.Fatalf("tool call = %+v", tc)
				}
				if len(tc.Diffs) != 1 || tc.Diffs[0].OldText != "x" || tc.Diffs[0].NewText != "y" || tc.Output != "ok" {
					t.Errorf("diffs/output = %+v / %q", tc.Diffs, tc.Output)
				}
				if tc.Input == "" {
					t.Error("raw input not rendered")
				}
			}},
		{"new file diff", `{"sessionUpdate":"tool_call_update","toolCallId":"t2","status":"completed","content":[{"type":"diff","path":"/p/n.go","newText":"package n"}]}`,
			func(t *testing.T, evs []as.Event) {
				tc := evs[0].ToolCall
				if evs[0].Kind != as.KindToolCallUpdate || tc.Status != "completed" || tc.Diffs[0].OldText != "" {
					t.Errorf("update = %+v", tc)
				}
			}},
		{"unknown", `{"sessionUpdate":"available_commands_update","availableCommands":[]}`,
			func(t *testing.T, evs []as.Event) {
				if evs != nil {
					t.Errorf("events = %+v", evs)
				}
			}},
		{"garbage", `not json`, func(t *testing.T, evs []as.Event) {
			if evs != nil {
				t.Errorf("events = %+v", evs)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.want(t, translate([]byte(tt.raw))) })
	}
}

func TestTruncate(t *testing.T) {
	long := make([]byte, maxOutput+10)
	for i := range long {
		long[i] = 'a'
	}
	if got := truncate(string(long)); len(got) > maxOutput+20 {
		t.Errorf("not truncated: %d", len(got))
	}
}
