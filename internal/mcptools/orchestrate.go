package mcptools

import "slices"

// Orchestration tools: they let an agent chat start and direct other agents in
// the same project. They are off by default (settings "orchestration") and, once
// on, covered by a per-chat grant the user gives in one native dialog
// (mcpserver.Tool.Granted), not by a question per call.
const (
	ToolListAgents = "list_agents"
	ToolStartAgent = "start_agent"
	ToolSendPrompt = "send_prompt"
	ToolWaitAgent  = "wait_for_agent"
	ToolReadAgent  = "read_agent_output"
	ToolStopAgent  = "stop_agent"
)

// OrchestrationNames lists the orchestration tools. Nothing else may be
// allowed on the strength of a grant (agentsession.ActionGate.Granted).
var OrchestrationNames = []string{ToolListAgents, ToolStartAgent, ToolSendPrompt, ToolWaitAgent, ToolReadAgent, ToolStopAgent}

// IsOrchestrationTool reports whether name is one of the orchestration tools.
func IsOrchestrationTool(name string) bool { return slices.Contains(OrchestrationNames, name) }
