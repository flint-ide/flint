package remote

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	as "flint/internal/agentsession"
)

// The window the review found: a request authenticated, then its device was
// revoked (and the streams cut) before the stream registered. register must
// refuse it.
func TestRegisterRefusesARevokedDevice(t *testing.T) {
	d, _, _, _ := newTestDevices(t)
	s := New(Options{Devices: d})
	if err := s.Start(Config{Host: "127.0.0.1", Port: 0, RequestsPerMinute: 60}); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := d.BeginPairing()
	dev, _, _ := d.Pair(p.Secret, "phone")
	st, _, err := s.register(dev.ID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	s.unregister(st)
	if !d.forget(dev.ID) {
		t.Fatal("forget")
	}
	if _, _, err := s.register(dev.ID, "127.0.0.1"); !errors.Is(err, errRevoked) {
		t.Fatalf("register after revoke: %v", err)
	}
	if _, _, err := s.register("0123456789abcdef", "127.0.0.1"); err == nil {
		t.Fatal("registered for an unknown device")
	}
}

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
