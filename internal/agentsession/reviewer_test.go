package agentsession

import (
	"context"
	"testing"
)

// A reviewer chat's edit inside its own folder, which the default policy
// (and even "do everything") approves for any other chat, reaches the user;
// a chat started without the mark is still auto-approved.
func TestReviewerChatAsksForEveryEdit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		policy   Policy
		kind     string
		reviewer bool
		asks     bool
	}{
		{"edit, default policy", DefaultPolicy(), ToolEdit, false, false},
		{"reviewer edit, default policy", DefaultPolicy(), ToolEdit, true, true},
		{"reviewer read, default policy", DefaultPolicy(), ToolRead, true, true},
		{"reviewer command, do everything", Policy{AutoApproveAll: true}, ToolExecute, true, true},
		{"command, do everything", Policy{AutoApproveAll: true}, ToolExecute, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			m := NewManager(rec.emit)
			m.SetPolicy(tc.policy)
			fake := newFake(tc.kind, "/p/src/a.go")
			m.Register("fake", fake)
			ctx := context.Background()
			if tc.reviewer {
				ctx = WithReviewer(ctx)
			}
			info, err := m.Start(ctx, "fake", "/p")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prompt(info.ID, "review", nil); err != nil {
				t.Fatal(err)
			}
			if tc.asks {
				waitFor(t, func() bool { return contains(rec.kinds(), KindPermission) })
				select {
				case opt := <-fake.answered:
					t.Fatalf("a reviewer chat's request was answered %q without the user", opt)
				default:
				}
			} else if opt := <-fake.answered; opt != "yes" {
				t.Fatalf("auto-approval answered %q", opt)
			}
			close(fake.release)
			waitFor(t, func() bool { return !m.Busy(info.ID) })
		})
	}
	if Reviewer(context.Background()) || !Reviewer(WithReviewer(context.Background())) {
		t.Error("Reviewer does not read WithReviewer's mark")
	}
}
