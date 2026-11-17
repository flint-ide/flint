package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"flint/internal/mcptools"
)

// scopeTree is lead -> a, b (siblings), a -> a1, plus a chat the user opened in
// the same project. Calls are made as the chat in o.caller.
type scopeTree struct {
	*orchApp
	a, b, a1, user string
}

func newScopeTree(t *testing.T) scopeTree {
	t.Helper()
	o := newOrchApp(t)
	s := scopeTree{orchApp: o}
	s.a = o.start(t, `{"task":"a"}`)
	s.b = o.start(t, `{"task":"b"}`)
	o.caller.Store(s.a)
	s.a1 = o.start(t, `{"task":"a1"}`)
	u, err := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: o.project})
	if err != nil {
		t.Fatal(err)
	}
	s.user = u.ID
	return s
}

func TestReadAndWaitReachOnlyTheCallerAndItsDescendants(t *testing.T) {
	s := newScopeTree(t)
	s.caller.Store(s.a)
	for _, id := range []string{s.b, s.user, s.chat} { // sibling, user-opened chat, parent
		s.fails(t, "read_agent_output", fmt.Sprintf(`{"id":%q}`, id), "not you or your descendant")
		s.fails(t, "wait_for_agent", fmt.Sprintf(`{"id":%q,"until":["idle"],"timeoutSeconds":1}`, id), "not you or your descendant")
	}
	// Itself and a descendant stay readable.
	s.ok(t, "read_agent_output", fmt.Sprintf(`{"id":%q}`, s.a))
	s.ok(t, "read_agent_output", fmt.Sprintf(`{"id":%q}`, s.a1))
	s.ok(t, "wait_for_agent", fmt.Sprintf(`{"id":%q,"until":["idle"],"timeoutSeconds":1}`, s.a1))
}

func TestListAgentsShowsOnlyTheCallerAndItsDescendants(t *testing.T) {
	s := newScopeTree(t)
	ids := func() string {
		var out []string
		for _, a := range listAgents(t, s.orchApp) {
			out = append(out, a.ID)
		}
		return strings.Join(out, ",")
	}
	s.caller.Store(s.a)
	if got := ids(); got != s.a+","+s.a1 {
		t.Errorf("a sees %q, want itself and a1", got)
	}
	s.caller.Store(s.chat)
	if got := ids(); got != s.chat+","+s.a+","+s.b+","+s.a1 {
		t.Errorf("lead sees %q, want itself and its subtree but not the user's chat %s", got, s.user)
	}
}

func TestListAgentsLeavesTerminalAgentsOut(t *testing.T) {
	s := newScopeTree(t)
	terms := s.mcpSources.Terminals
	terms.Open("term-a", mcptools.KindTerminal, "claude", s.project)
	terms.SetAgentState("term-a", "working")
	var r struct{ Agents []mcptools.AgentInfo }
	if err := json.Unmarshal([]byte(s.ok(t, "list_agents", `{}`)), &r); err != nil {
		t.Fatal(err)
	}
	for _, a := range r.Agents {
		if a.Kind == "terminal" {
			t.Errorf("terminal agent listed: %+v", a)
		}
	}
}
