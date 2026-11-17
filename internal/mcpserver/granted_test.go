package mcpserver

import (
	"context"
	"sync/atomic"
	"testing"
)

// A tool covered by a per-chat grant hands its Granted check to Approve, which
// decides; the server itself never skips the question.
func TestApprovalCarriesGrantCheck(t *testing.T) {
	var ran atomic.Int32
	tool := actTool(&ran, false)
	tool.Granted = func(ctx context.Context) bool { return CallerFrom(ctx) == "chat-1" }
	var sawGrant, grantedForCaller atomic.Bool
	s := newTestServer(t, Options{
		Tools: func() []Tool { return []Tool{tool} },
		Approve: func(ctx context.Context, c Approval) error {
			sawGrant.Store(c.Granted != nil)
			grantedForCaller.Store(c.Granted != nil && c.Granted(ctx))
			return nil
		},
	})
	url, token, _, _ := s.Endpoint("/p", func() string { return "chat-1" })
	if text, isErr := toolText(t, call(t, url, token, callAct)); isErr {
		t.Fatalf("call failed: %q", text)
	}
	if !sawGrant.Load() || !grantedForCaller.Load() || ran.Load() != 1 {
		t.Errorf("grant check passed on = %v/%v, runs = %d", sawGrant.Load(), grantedForCaller.Load(), ran.Load())
	}
}
