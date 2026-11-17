package main

// eventTermAgent tells the UI that an agent CLI running in a terminal changed
// state: the payload is TermAgent.
const eventTermAgent = "term:agent"

// TermAgent is the state an agent CLI in a terminal reported through its escape
// sequence (internal/terminal/agentstate.go): working, waiting, done or idle.
type TermAgent struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// AgentState forwards a terminal's agent state to the UI and to list_agents.
func (e *terminalEvents) AgentState(id, state string) {
	e.app.mcpSources.Terminals.SetAgentState(id, state)
	e.app.emit(eventTermAgent, TermAgent{ID: id, State: state})
}
