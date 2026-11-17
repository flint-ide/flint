package mcptools

import "testing"

func TestTerminalAgentState(t *testing.T) {
	ts := NewTerminals()
	ts.Open("t1", KindTerminal, "claude", "/p")
	ts.SetAgentState("t1", "waiting")
	ts.SetAgentState("unknown", "working") // ignored, no panic

	got, ok := ts.Get("t1")
	if !ok || got.AgentState != "waiting" || got.Title != "claude" {
		t.Fatalf("Get = %+v, %v", got, ok)
	}
	if list := ts.List("/p"); len(list) != 1 || list[0].AgentState != "waiting" {
		t.Errorf("List = %+v", list)
	}
	if _, ok := ts.Get("unknown"); ok {
		t.Error("an unknown id was found")
	}
	// Reopening an id starts over, state included.
	ts.Open("t1", KindTerminal, "", "/p")
	if got, _ := ts.Get("t1"); got.AgentState != "" {
		t.Errorf("state %q survived a reopen", got.AgentState)
	}
}
