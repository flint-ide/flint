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

func TestListAgentsReportsTerminalAgentsReadOnly(t *testing.T) {
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

	var terminals, chats []listedAgent
	for _, a := range listAgents(t, o) {
		if a.Kind == "terminal" {
			terminals = append(terminals, a)
		} else {
			chats = append(chats, a)
		}
	}
	if len(terminals) != 1 || terminals[0].ID != "term-a" || terminals[0].State != "waiting" || terminals[0].Agent != "claude" || terminals[0].Cwd != "." {
		t.Fatalf("terminal agents = %+v, want only term-a, waiting", terminals)
	}
	if len(chats) != 1 {
		t.Errorf("chats = %+v", chats)
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
