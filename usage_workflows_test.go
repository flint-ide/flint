package main

import (
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/features/workflows"
	"flint/internal/settings"
	"flint/internal/usage"
)

// A workflow step refused by the usage budget holds its run instead of
// failing it: workflows.LimitKind recognises usageGate's wording. Rewording
// the refusal must update the pattern there (budgetText in limits.go).
func TestUsageGateIsAWorkflowHold(t *testing.T) {
	a := usageApp(t, func(u *settings.Usage) { u.DailyBudgetTokens = 100; u.PauseOverBudget = true })
	a.usageChats.bind("s1", usageChat{chat: "s1", agent: "claude", cwd: "/work/repo"})
	a.recordUsage("s1", mustChat(t, a, "s1"), &as.Usage{InputTokens: 90, OutputTokens: 20})
	err := a.usageGate("s1")
	if err == nil {
		t.Fatal("over budget should pause")
	}
	if got := workflows.LimitKind("start Claude for task 0123: " + err.Error()); got != workflows.LimitBudget {
		t.Errorf("LimitKind(%q) = %q", err.Error(), got)
	}
	// The other wordings of describeAlert, after usageGate's prefix.
	for _, al := range []usage.Alert{
		{Scope: usage.ScopeTask, Kind: usage.KindUSD, Limit: 5, Spent: 5.1, Estimated: true},
		{Scope: usage.ScopeTask, Kind: usage.KindTokens, Limit: 1000, Spent: 1200},
		{Scope: usage.ScopeDay, Kind: usage.KindUSD, Limit: 20, Spent: 21},
	} {
		text := "paused: " + describeAlert(al) + ". Choose Continue anyway"
		if got := workflows.LimitKind(text); got != workflows.LimitBudget {
			t.Errorf("LimitKind(%q) = %q", text, got)
		}
	}
}
