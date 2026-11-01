package remote

import (
	"encoding/json"
	"strings"
	"testing"

	as "flint/internal/agentsession"
)

func TestParseSince(t *testing.T) {
	got, err := parseSince([]string{"a:1,b:22", "urn:uuid:x:3", ""})
	if err != nil || got["a"] != 1 || got["b"] != 22 || got["urn:uuid:x"] != 3 {
		t.Fatalf("since = %v, %v", got, err)
	}
	for _, bad := range []string{"nocolon", ":5", "a:", "a:-1", "a:x", "a\x00b:1", strings.Repeat("a", 300) + ":1"} {
		if _, err := parseSince([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	many := make([]string, maxSince+1)
	for i := range many {
		many[i] = "c" + strings.Repeat("x", i) + ":1"
	}
	if _, err := parseSince([]string{strings.Join(many, ",")}); err == nil {
		t.Error("more chats than the limit accepted")
	}
}

func TestRedactEventLeavesTheOriginalAlone(t *testing.T) {
	tc := &as.ToolCall{Input: "curl -H 'Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345'", Paths: []string{"/x"}}
	e := as.Event{Kind: as.KindPermission, Text: "AKIAABCDEFGHIJKLMNOP", ToolCall: tc, Permission: &as.PermissionRequest{ToolCall: *tc}, Plan: []as.PlanEntry{{Content: "AKIAABCDEFGHIJKLMNOP"}}}
	r := redactEvent(e)
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "AKIAABCDEFGHIJKLMNOP") || strings.Contains(string(b), "abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("not redacted: %s", b)
	}
	if e.Text != "AKIAABCDEFGHIJKLMNOP" || !strings.Contains(e.ToolCall.Input, "abcdefghijklmnopqrstuvwxyz012345") || e.Plan[0].Content != "AKIAABCDEFGHIJKLMNOP" {
		t.Fatal("redaction changed the desktop's event")
	}
}

func TestOversizedEventsAreShrunk(t *testing.T) {
	big := strings.Repeat("x", maxOutMessage)
	e := as.Event{SessionID: "s", Seq: 3, Kind: as.KindToolCall, ToolCall: &as.ToolCall{Output: big, Diffs: []as.Diff{{Path: "a", OldText: big, NewText: big}}}}
	s := shrinkEvent(e)
	b, _ := json.Marshal(s)
	if len(b) > maxOutMessage || s.Seq != 3 || s.ToolCall.Diffs[0].Path != "a" {
		t.Fatalf("shrunk to %d bytes: %+v", len(b), s.ToolCall.Diffs)
	}
}
