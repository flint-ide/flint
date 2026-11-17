package agentsession

import (
	"context"
	"testing"
)

// A chat's standing grant answers the agent's own permission request for the
// tools it covers, with a one-time allow, and for nothing else: another tool
// of Flint's, a tool that no longer acts, or a chat without the grant still
// reach the user.
func TestGrantedFlintToolsAreAllowedOnce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tool    string
		acts    bool
		granted bool
		auto    bool
	}{
		{"covered by the grant", "start_agent", true, true, true},
		{"another acting tool is not covered", "run_tests", true, false, false},
		{"the tool is no longer offered", "start_agent", false, true, false},
		{"no grant for this chat", "start_agent", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			m := NewManager(rec.emit)
			var asked []string
			m.SetActionGate(ActionGate{
				Acts: func(string) bool { return tc.acts },
				Granted: func(session, tool string) bool {
					asked = append(asked, session+"/"+tool)
					return tc.granted && tool == "start_agent"
				},
			})
			fake := newFake(ToolOther, "/p/a.go")
			fake.flintTool = tc.tool
			m.Register("fake", fake)
			info, _ := m.Start(context.Background(), "fake", "/p")
			if err := m.Prompt(info.ID, "go", nil); err != nil {
				t.Fatal(err)
			}
			if tc.auto {
				if opt := <-fake.answered; opt != "yes" {
					t.Fatalf("answered %q, want the one-time allow", opt)
				}
				if contains(rec.kinds(), KindPermission) {
					t.Fatal("the user was asked although a grant covers the call")
				}
			} else {
				waitFor(t, func() bool { return contains(rec.kinds(), KindPermission) })
				select {
				case opt := <-fake.answered:
					t.Fatalf("answered %q without the user", opt)
				default:
				}
			}
			close(fake.release)
		})
	}
}
