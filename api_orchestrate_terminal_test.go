package main

import (
	"encoding/json"
	"testing"

	"flint/internal/mcptools"
)

type listedAgent struct {
	ID, Agent, State, Kind, Cwd string
}

func listAgents(t *testing.T, o *orchApp) []listedAgent {
	t.Helper()
	var out struct{ Agents []listedAgent }
	if err := json.Unmarshal([]byte(o.ok(t, "list_agents", `{}`)), &out); err != nil {
		t.Fatal(err)
	}
	return out.Agents
}

func TestTerminalAgentsAreNotListedAndStayOutOfReach(t *testing.T) {
	o := newOrchApp(t)
	terms := o.mcpSources.Terminals
	terms.Open("term-a", mcptools.KindTerminal, "claude --resume", o.project)
	terms.SetAgentState("term-a", "waiting")
	terms.Open("term-plain", mcptools.KindTerminal, "npm run dev", o.project) // no agent state
	terms.Open("term-gone", mcptools.KindTerminal, "claude", o.project)
	terms.SetAgentState("term-gone", "working")
	terms.Exit("term-gone", 0)
	terms.Open("term-elsewhere", mcptools.KindTerminal, "claude", t.TempDir())
	terms.SetAgentState("term-elsewhere", "working")

	// Terminal tabs carry no parent link, so they cannot be attributed to the
	// caller's subtree: list_agents leaves them out (see orchestrator.List).
	listed := listAgents(t, o)
	if len(listed) != 1 || listed[0].Kind != "chat" {
		t.Fatalf("listed = %+v, want only the caller's own chat", listed)
	}

	// The other tools say what a terminal agent is, and do nothing.
	for _, call := range []struct{ tool, args string }{
		{"send_prompt", `{"id":"term-a","text":"hi"}`},
		{"wait_for_agent", `{"id":"term-a","until":["idle"],"timeoutSeconds":1}`},
		{"read_agent_output", `{"id":"term-a"}`},
		{"stop_agent", `{"id":"term-a"}`},
	} {
		o.fails(t, call.tool, call.args, "terminal tab")
	}
	o.fails(t, "send_prompt", `{"id":"term-plain","text":"hi"}`, "terminal tab")
}
