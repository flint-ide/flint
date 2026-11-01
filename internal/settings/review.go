package settings

import "fmt"

// AgentReview configures "Review Changes with Agent" (internal/review).
type AgentReview struct {
	// Scope is what a review covers by default: "selected" (the files ticked
	// in the Commit panel) or "all" (every change).
	Scope string `json:"scope"`
	// MaxDiffKB bounds the diff sent to the agent; larger diffs are
	// shortened per file.
	MaxDiffKB int `json:"maxDiffKB"`
	// Agent reviews ("claude", "codex" or a custom agent's id); empty means
	// the default agent.
	Agent string `json:"agent"`
	// TimeoutSeconds stops a review that takes longer.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// Review scopes.
const (
	ReviewSelected = "selected"
	ReviewAll      = "all"
)

// Bounds of AgentReview. The diff limit stays well below what an argument
// list can carry (Codex gets its input appended to the prompt argument).
const (
	MinReviewDiffKB   = 8
	MaxReviewDiffKB   = 512
	MinReviewTimeout  = 30
	MaxReviewTimeout  = 1800
	defaultReviewKB   = 128
	defaultReviewTime = 300
)

func defaultAgentReview() AgentReview {
	return AgentReview{Scope: ReviewSelected, MaxDiffKB: defaultReviewKB, TimeoutSeconds: defaultReviewTime}
}

// normalizeAgentReview replaces invalid values with defaults and clamps the
// numbers (0 or missing is the default). An agent that is not configured
// falls back to the default agent.
func normalizeAgentReview(r AgentReview, custom []CustomAgent) AgentReview {
	def := defaultAgentReview()
	out := r
	if r.Scope != ReviewSelected && r.Scope != ReviewAll {
		out.Scope = def.Scope
	}
	clamp := func(v, lo, hi, d int) int {
		if v == 0 {
			return d
		}
		return max(lo, min(hi, v))
	}
	out.MaxDiffKB = clamp(r.MaxDiffKB, MinReviewDiffKB, MaxReviewDiffKB, def.MaxDiffKB)
	out.TimeoutSeconds = clamp(r.TimeoutSeconds, MinReviewTimeout, MaxReviewTimeout, def.TimeoutSeconds)
	if !validAgent(r.Agent, custom) {
		out.Agent = ""
	}
	return out
}

func validateAgentReview(r AgentReview, custom []CustomAgent) error {
	switch {
	case r.Scope != ReviewSelected && r.Scope != ReviewAll:
		return fmt.Errorf("agentReview.scope %q is not %q or %q", r.Scope, ReviewSelected, ReviewAll)
	case r.MaxDiffKB < MinReviewDiffKB || r.MaxDiffKB > MaxReviewDiffKB:
		return fmt.Errorf("agentReview.maxDiffKB %d out of range %d–%d", r.MaxDiffKB, MinReviewDiffKB, MaxReviewDiffKB)
	case r.TimeoutSeconds < MinReviewTimeout || r.TimeoutSeconds > MaxReviewTimeout:
		return fmt.Errorf("agentReview.timeoutSeconds %d out of range %d–%d", r.TimeoutSeconds, MinReviewTimeout, MaxReviewTimeout)
	case !validAgent(r.Agent, custom):
		return fmt.Errorf("agentReview.agent: unsupported agent %q", r.Agent)
	}
	return nil
}
