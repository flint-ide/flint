package settings

import "fmt"

// Orchestration configures agents that start and direct other agents through
// Flint's MCP server (internal/orchestrate, internal/mcptools/orchestrate.go).
// It is off by default: until Enabled, the tools are not even listed.
type Orchestration struct {
	// Enabled offers the orchestration tools to agents. Each chat still needs
	// its own grant, which the user gives in a native dialog.
	Enabled bool `json:"enabled"`
	// MaxChildren is how many agents one chat may have started at a time.
	MaxChildren int `json:"maxChildren"`
	// MaxDepth is how many levels of agents may start agents: 1 lets the
	// user's chat start agents that cannot start any.
	MaxDepth int `json:"maxDepth"`
	// MaxWaitSeconds caps one wait_for_agent call.
	MaxWaitSeconds int `json:"maxWaitSeconds"`
	// NotifyParent tells a chat when an agent it started waits, fails or
	// finishes, so it need not poll.
	NotifyParent bool `json:"notifyParent"`
	// OutputLines is the most lines read_agent_output returns.
	OutputLines int `json:"outputLines"`
	// MaxAutoTurns is how many turns agents may start in each other (send_prompt,
	// a child's first prompt, a delivered notice) in one group of chats before
	// the user writes in it again; the next ones are refused.
	MaxAutoTurns int `json:"maxAutoTurns"`
}

// Bounds of the orchestration settings: every child is an agent process
// spending the user's budget, and waits hold a tool slot of the MCP server.
const (
	MaxOrchestrationChildren    = 16
	MaxOrchestrationDepth       = 4
	MaxOrchestrationWait        = 600 // the MCP server's own cap on one call
	MaxOrchestrationOutputLines = 2000
	MaxOrchestrationAutoTurns   = 400
)

func defaultOrchestration() Orchestration {
	return Orchestration{MaxChildren: 4, MaxDepth: 2, MaxWaitSeconds: 600, NotifyParent: true, OutputLines: 200, MaxAutoTurns: 40}
}

// CheckOrchestration reports the first value out of range.
func CheckOrchestration(o Orchestration) error {
	for _, f := range []struct {
		key   string
		v, hi int
	}{
		{"maxChildren", o.MaxChildren, MaxOrchestrationChildren},
		{"maxDepth", o.MaxDepth, MaxOrchestrationDepth},
		{"maxWaitSeconds", o.MaxWaitSeconds, MaxOrchestrationWait},
		{"outputLines", o.OutputLines, MaxOrchestrationOutputLines},
		{"maxAutoTurns", o.MaxAutoTurns, MaxOrchestrationAutoTurns},
	} {
		if f.v < 1 || f.v > f.hi {
			return fmt.Errorf("orchestration.%s %d out of range 1–%d", f.key, f.v, f.hi)
		}
	}
	return nil
}

// normalizeOrchestration clamps each number into its range; 0 (a missing
// key) is the default.
func normalizeOrchestration(o Orchestration) Orchestration {
	def := defaultOrchestration()
	clamp := func(v, hi, d int) int {
		if v == 0 {
			return d
		}
		return max(1, min(hi, v))
	}
	o.MaxChildren = clamp(o.MaxChildren, MaxOrchestrationChildren, def.MaxChildren)
	o.MaxDepth = clamp(o.MaxDepth, MaxOrchestrationDepth, def.MaxDepth)
	o.MaxWaitSeconds = clamp(o.MaxWaitSeconds, MaxOrchestrationWait, def.MaxWaitSeconds)
	o.OutputLines = clamp(o.OutputLines, MaxOrchestrationOutputLines, def.OutputLines)
	o.MaxAutoTurns = clamp(o.MaxAutoTurns, MaxOrchestrationAutoTurns, def.MaxAutoTurns)
	return o
}
