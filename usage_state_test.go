package main

import (
	"strings"
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/settings"
	"flint/internal/usage"
)

func usageApp(t *testing.T, change func(*settings.Usage)) *App {
	t.Helper()
	s := settings.Default()
	change(&s.Usage)
	a := &App{configDir: t.TempDir(), settings: s, roots: []string{"/work/repo"}}
	a.openUsage()
	return a
}

func TestRecordUsageCountsChatsPerProjectAndTask(t *testing.T) {
	a := usageApp(t, func(*settings.Usage) {})
	a.state.AgentTasks = agenttask.Store{"repo.git": {{ID: "0123456789abcdef", Title: "Rate limits", Worktree: "/wt/rate", Checkout: "/work/repo"}}}
	a.usageChats.bind("s1", usageChat{chat: "s1", agent: "claude", cwd: "/work/repo/sub", model: "opus"})
	a.usageChats.prompted("s1", "Explain the parser")
	a.usageChats.prompted("s1", "and then this")
	a.usageChats.bind("s2", usageChat{chat: "s2", agent: "codex", cwd: "/wt/rate", model: "gpt-5"})

	a.recordUsage("s1", mustChat(t, a, "s1"), &as.Usage{InputTokens: 100, OutputTokens: 10, CostUSD: 0.2, Model: "opus-4"})
	a.recordUsage("s2", mustChat(t, a, "s2"), &as.Usage{InputTokens: 50, OutputTokens: 5})

	view := (&UsageAPI{app: a}).Summary(usage.Query{})
	sum := view.Summary
	if len(sum.Projects) != 1 || sum.Projects[0].Key != "/work/repo" || sum.Projects[0].Totals.Turns != 2 {
		t.Errorf("both chats count for the repository: %+v", sum.Projects)
	}
	if len(sum.Tasks) != 1 || view.TaskTitles["0123456789abcdef"] != "Rate limits" {
		t.Errorf("tasks %+v titles %+v", sum.Tasks, view.TaskTitles)
	}
	models := []string{}
	for _, m := range sum.Models {
		models = append(models, m.Key)
	}
	if strings.Join(models, ",") != "opus-4,gpt-5" {
		t.Errorf("the turn's model wins over the session's: %v", models)
	}
	if sum.Chats[0].Label != "Explain the parser" {
		t.Errorf("chats are named after their first message: %+v", sum.Chats[0])
	}
}

func TestBudgetAlertAndPause(t *testing.T) {
	a := usageApp(t, func(u *settings.Usage) { u.DailyBudgetTokens = 100; u.PauseOverBudget = true })
	api := &UsageAPI{app: a}
	a.usageChats.bind("s1", usageChat{chat: "s1", agent: "claude", cwd: "/work/repo"})
	if err := a.usageGate("s1"); err != nil {
		t.Fatalf("under budget: %v", err)
	}
	a.recordUsage("s1", mustChat(t, a, "s1"), &as.Usage{InputTokens: 90, OutputTokens: 20})
	err := a.usageGate("s1")
	if err == nil || !strings.Contains(err.Error(), "today's budget of 100 tokens") {
		t.Fatalf("over budget should pause: %v", err)
	}
	st := api.Budget("s1")
	if len(st.Alerts) != 1 || !st.Pause || st.Alerts[0].Overridden {
		t.Errorf("budget status = %+v", st)
	}
	if err := api.Override("s1"); err != nil {
		t.Fatal(err)
	}
	if err := a.usageGate("s1"); err != nil {
		t.Errorf("continue anyway must unpause: %v", err)
	}
}

func TestUsageCSV(t *testing.T) {
	a := usageApp(t, func(u *settings.Usage) { u.Prices = map[string]usage.Price{"gpt-5": {In: 1, Out: 1}} })
	a.usageChats.bind("s", usageChat{chat: "s", agent: "codex", cwd: "/work/repo", model: "gpt-5"})
	a.recordUsage("s", mustChat(t, a, "s"), &as.Usage{InputTokens: 1_000_000})
	out, err := (&UsageAPI{app: a}).CSV(usage.Query{})
	if err != nil || !strings.Contains(out, "codex,gpt-5,,1,1000000,0,0,0,0.000000,1.000000") {
		t.Errorf("csv = %q %v", out, err)
	}
}

func mustChat(t *testing.T, a *App, id string) usageChat {
	t.Helper()
	c, ok := a.usageChats.get(id)
	if !ok {
		t.Fatalf("chat %s not bound", id)
	}
	return c
}
