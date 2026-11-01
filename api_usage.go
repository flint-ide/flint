package main

import (
	"time"

	"flint/internal/config"
	"flint/internal/usage"
)

// UsageAPI answers the Usage panel, the budget warning of a chat and the CSV
// export. Everything comes from usage.json; nothing leaves the machine.
type UsageAPI struct {
	app *App
}

// UsageView is the panel's data plus the task titles it shows.
type UsageView struct {
	Summary usage.Summary `json:"summary"`
	// TaskTitles names the tasks in the summary (id → title) that still exist.
	TaskTitles map[string]string `json:"taskTitles"`
}

// Summary returns today / 7 / 30-day totals, the daily chart, breakdowns and
// top chats for q.
func (u *UsageAPI) Summary(q usage.Query) UsageView {
	s := u.app.currentSettings().Usage
	sum := u.app.usage.Summary(q, s.Prices)
	titles := map[string]string{}
	for _, g := range sum.Tasks {
		if t := u.app.taskTitle(g.Key, usage.ScopeTask); t != "" {
			titles[g.Key] = t
		}
	}
	for _, c := range sum.Chats {
		if c.Task != "" && titles[c.Task] == "" {
			if t := u.app.taskTitle(c.Task, usage.ScopeTask); t != "" {
				titles[c.Task] = t
			}
		}
	}
	return UsageView{Summary: sum, TaskTitles: titles}
}

// BudgetStatus is what a chat's header warns about.
type BudgetStatus struct {
	// Alerts are the spent budgets that concern the chat: today's, and its
	// task's when it runs in one.
	Alerts []usage.Alert `json:"alerts"`
	// Pause: new turns are refused while a not-overridden alert is listed.
	Pause bool `json:"pause"`
}

// Budget reports the spent budgets for the chat session sessionID.
func (u *UsageAPI) Budget(sessionID string) BudgetStatus {
	s := u.app.currentSettings().Usage
	task := ""
	if c, ok := u.app.usageChats.get(sessionID); ok {
		task = u.app.taskByFolder(c.cwd)
	}
	alerts := u.app.usage.Over(s.Limits(), task, s.Prices)
	if alerts == nil {
		alerts = []usage.Alert{}
	}
	return BudgetStatus{Alerts: alerts, Pause: s.PauseOverBudget}
}

// Override lets sessionID's chat (and every chat today) start turns although
// a budget is spent: "Continue anyway" in the warning.
func (u *UsageAPI) Override(sessionID string) error {
	task := ""
	if c, ok := u.app.usageChats.get(sessionID); ok {
		task = u.app.taskByFolder(c.cwd)
	}
	if err := u.app.usage.Override(task); err != nil {
		return err
	}
	u.app.emit(eventUsageChanged)
	return nil
}

// CSV returns the daily aggregates matching q as CSV.
func (u *UsageAPI) CSV(q usage.Query) (string, error) {
	data, err := u.app.usage.CSV(q, u.app.currentSettings().Usage.Prices)
	return string(data), err
}

// ExportCSV asks where to save the daily aggregates matching q as CSV and
// writes them; it returns the path, "" when cancelled.
func (u *UsageAPI) ExportCSV(q usage.Query) (string, error) {
	data, err := u.app.usage.CSV(q, u.app.currentSettings().Usage.Prices)
	if err != nil {
		return "", err
	}
	path, err := u.app.pickSaveFile("Export usage as CSV", "flint-usage-"+time.Now().Format("2006-01-02")+".csv")
	if err != nil || path == "" {
		return "", err
	}
	return path, config.WriteFileAtomic(path, data, 0o600)
}
