package workflows

import (
	"regexp"
	"slices"
	"strings"
)

// The kinds of limit that hold a run (LimitKind, Hold.Kind).
const (
	// LimitBudget: Flint's own usage budget (usage.pauseOverBudget) is spent.
	LimitBudget = "budget"
	// LimitAgent: the agent's provider refused for its quota or rate limit
	// (a subscription's usage limit, an API's rate limit, no credits left).
	LimitAgent = "limit"
)

// Bounds of the automatic handling of limits.
const (
	// MaxAutoResumes bounds the automatic resumes of one step
	// (workflows.resumeAfterMinutes); after that the run waits for the user.
	MaxAutoResumes = 12
	// limitLines is how many of the last non-empty lines of a turn's
	// output are searched for a limit, so a limit mentioned earlier in the
	// work (a task about rate limiting) does not count.
	limitLines = 5
)

var (
	// budgetText is the wording of Flint's refusal of a turn over budget
	// (usageGate and describeAlert in package main, usage_state.go):
	// "paused: today's budget of 100 tokens is spent (120). Choose …" or
	// "paused: this task's budget of $5.00 is spent ($5.10). …". A test in
	// package main (TestUsageGateIsAWorkflowHold) fails when it changes.
	budgetText = regexp.MustCompile(`(?i)\bpaused: (today's|this task's) budget\b[^\n]*?\bis spent\b`)

	// limitText are the providers' words for a quota or rate limit. Every
	// pattern is a whole phrase between word boundaries, so "limited", "the
	// line limit of the file" or "quotation" do not match; a bare number is
	// a limit only next to "status", "http", "error" or "code" (or "Too Many
	// Requests"), and "overloaded" only as the API's error or about a
	// server, so a method that is overloaded does not count.
	limitText = regexp.MustCompile(`(?i)` + strings.Join([]string{
		`\busage limits?\b`,
		`\brate[ _-]?limit(s|ed|_error|_exceeded)?\b`,
		`\blimit (reached|exceeded)\b`,
		`\b(hit|reached) (your|its|the) (usage |daily |weekly |monthly )?limit\b`,
		`\bquotas?\b( exceeded)?`,
		`\binsufficient_quota\b`,
		`\btoo many requests\b`,
		`\b(status|http|error|code)[ :=]*(429|529)\b`,
		`\b429 too many\b`,
		`\boverloaded_error\b`,
		`\b(api|server|servers|service|model|capacity) (is |are )?(currently |temporarily )?overloaded\b`,
		`\bcredit balance\b`,
		`\binsufficient (credits?|funds|balance)\b`,
		`\bout of credits\b`,
	}, "|"))
)

// outputLimit are the providers' own announcements of a usage limit, for the
// ordinary output of a turn that ended normally (LimitInOutput). Each is
// matched at the start of a line (past markdown marks and a label such as
// "API Error:"), so an agent that merely writes about limits ("Added the rate
// limit middleware", "handled the 429 Too Many Requests response") does not
// match. The wordings are the agents' real ones: Claude Code ("Claude AI
// usage limit reached|<time>", "You've hit your limit · resets 3pm",
// "5-hour limit reached ∙ resets 3pm", "Your credit balance is too low …"),
// Codex ("You've hit your usage limit …") and the APIs' ("429 Too Many
// Requests", "insufficient_quota").
var outputLimit = regexp.MustCompile(`(?im)^[\s>*_` + "`" + `\-•]*` +
	`(?:(?:claude(?: ai)?|codex|api error|stream error|error)[: ]+(?:\d{3}[: ]+)?)?` +
	`(?:` + strings.Join([]string{
	`usage limit (?:reached|exceeded)\b`,
	`you(?:'|\x{2019})?(?:ve| have) (?:hit|reached) your (?:usage |daily |weekly |monthly |5-hour )?limit\b`,
	`(?:5-hour|daily|weekly|monthly) limit (?:reached|exceeded)\b`,
	`insufficient_quota\b`,
	`429 too many requests\b`,
	`(?:your )?credit balance is too low\b`,
}, "|") + `)`)

// LimitInOutput says whether the ordinary output of a turn that ended
// normally (the last lines of what an agent said, with no result line) is the
// provider announcing the agent's usage limit: LimitAgent, or "". It is much
// stricter than LimitKind, which reads error texts: an agent working on a
// rate limiter ends with words like "rate limit" and "quota" that are its
// work, not a limit it hit.
func LimitInOutput(text string) string {
	if outputLimit.MatchString(text) {
		return LimitAgent
	}
	return ""
}

// LimitKind says whether a step's failure text is a limit: LimitBudget for
// Flint's own pause over budget, LimitAgent for the provider's quota or rate
// limit, "" for any other failure. It is for error texts (a failed turn, a
// refused prompt); the output of a turn that ended normally is LimitInOutput's.
func LimitKind(text string) string {
	switch {
	case budgetText.MatchString(text):
		return LimitBudget
	case limitText.MatchString(text):
		return LimitAgent
	}
	return ""
}

// lastLines is the last n non-empty lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return strings.Join(out, "\n")
}

// policy is what the engine's settings and agents say about a step's end.
type policy struct {
	maxAttempts int
	// fallback is workflows.fallbackAgent; available the agents that can
	// start now.
	fallback  string
	available []string
	// resumeAfter is workflows.resumeAfterMinutes, in seconds (0: never).
	resumeAfter int64
}

// limited is the transition for a step a limit stopped: with the agent's
// own limit and a fallback agent that is available and is not the one
// that hit it, the step is taken again at once by the fallback; else the
// run is held, to resume by itself after resumeAfter while the step has
// had fewer than MaxAutoResumes holds.
func limited(r Run, kind, reason string, p policy, now int64) Run {
	if !r.valid() {
		return r
	}
	if kind == LimitAgent && p.fallback != "" && p.fallback != r.StepAgent() && slices.Contains(p.available, p.fallback) {
		return r.Fallback(p.fallback, now)
	}
	var until int64
	if p.resumeAfter > 0 && r.Records[r.Current].Holds < MaxAutoResumes {
		until = now + p.resumeAfter
	}
	return r.Held(kind, reason, until, now)
}
