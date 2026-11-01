package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	as "flint/internal/agentsession"
	"flint/internal/redact"
	"flint/internal/usage"
)

// Usage of agent chats is recorded in Go as it arrives (usage.json in the
// config directory, internal/usage): every "usage" event of a bound chat is
// one turn, added to its day × project × agent × model × task row. Then the
// budgets are checked; a newly spent one is announced once ("usage:alert").

const (
	eventUsageChanged = "usage:changed"
	eventUsageAlert   = "usage:alert"
	maxLabelSource    = 2000 // bytes of a first message a chat's name is made from
)

// UsageAlert tells the UI that a budget was spent by a turn of a chat.
type UsageAlert struct {
	SessionID string      `json:"sessionId"`
	Alert     usage.Alert `json:"alert"`
	// TaskTitle names the task of a task budget.
	TaskTitle string `json:"taskTitle,omitempty"`
}

// usageChat is what a chat session's usage is recorded under.
type usageChat struct {
	chat  string // the chat's id: the session it started as (resumes may fork)
	agent string
	cwd   string
	model string
	label string // its first message
}

// usageChats maps live session ids to their chats.
type usageChats struct {
	mu    sync.Mutex
	chats map[string]usageChat
}

func (u *usageChats) bind(sessionID string, c usageChat) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.chats == nil {
		u.chats = map[string]usageChat{}
	}
	u.chats[sessionID] = c
}

func (u *usageChats) get(sessionID string) (usageChat, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.chats[sessionID]
	return c, ok
}

// prompted names a chat after its first message (its start, with
// credentials removed: the name is kept in usage.json).
func (u *usageChats) prompted(sessionID, text string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c, ok := u.chats[sessionID]; ok && c.label == "" {
		if len(text) > maxLabelSource {
			text = text[:maxLabelSource]
		}
		c.label = redact.Secrets(text)
		u.chats[sessionID] = c
	}
}

func (u *usageChats) unbind(sessionID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.chats, sessionID)
}

// openUsage loads usage.json; a broken file only costs its history.
func (a *App) openUsage() {
	store, err := usage.Open(filepath.Join(a.configDir, usage.FileName), a.settings.Usage.RetentionDays)
	if err != nil {
		a.logf("usage: %v", err)
	}
	a.usage = store
}

// observeUsage records a usage event off the agent's goroutine (the file is
// written once per turn, never while events are being stamped).
func (a *App) observeUsage(e as.Event) {
	if e.Kind != as.KindUsage || e.Usage == nil || a.usage == nil {
		return
	}
	c, ok := a.usageChats.get(e.SessionID)
	if !ok {
		return
	}
	go a.recordUsage(e.SessionID, c, e.Usage)
}

func (a *App) recordUsage(sessionID string, c usageChat, u *as.Usage) {
	task := a.taskByFolder(c.cwd)
	model := u.Model
	if model == "" {
		model = c.model
	}
	err := a.usage.Add(usage.Entry{
		Chat: c.chat, Label: c.label, Project: a.usageProject(c.cwd, task),
		Agent: c.agent, Model: model, Task: task, Usage: usage.FromEvent(u),
	})
	if err != nil {
		a.logf("usage: %v", err)
		return
	}
	a.emit(eventUsageChanged)
	s := a.currentSettings().Usage
	alerts, err := a.usage.Fire(s.Limits(), task, s.Prices)
	if err != nil {
		a.logf("usage: %v", err)
	}
	for _, al := range alerts {
		a.emit(eventUsageAlert, UsageAlert{SessionID: sessionID, Alert: al, TaskTitle: a.taskTitle(al.Key, al.Scope)})
	}
}

// usageProject is the project a chat's usage counts for: a task's main
// checkout, else the open workspace folder holding cwd, else cwd.
func (a *App) usageProject(cwd, task string) string {
	if task != "" {
		if _, t, ok := a.taskStore().Find(task); ok && t.Checkout != "" {
			return t.Checkout
		}
	}
	best := ""
	for _, root := range a.projectRoots() {
		if (cwd == root || strings.HasPrefix(cwd, root+"/")) && len(root) > len(best) {
			best = root
		}
	}
	if best != "" {
		return best
	}
	return cwd
}

func (a *App) taskTitle(id, scope string) string {
	if scope != usage.ScopeTask {
		return ""
	}
	if _, t, ok := a.taskStore().Find(id); ok {
		return t.Title
	}
	return ""
}

// usageGate refuses a new turn while a budget is spent, when the user asked
// for that in settings and has not chosen to continue.
func (a *App) usageGate(sessionID string) error {
	s := a.currentSettings().Usage
	if !s.PauseOverBudget || a.usage == nil {
		return nil
	}
	c, ok := a.usageChats.get(sessionID)
	if !ok {
		return nil
	}
	blocked := a.usage.Blocked(s.Limits(), a.taskByFolder(c.cwd), s.Prices)
	if len(blocked) == 0 {
		return nil
	}
	return fmt.Errorf("paused: %s. Choose Continue anyway in the budget warning above the chat, or raise the budget in Settings ▸ Usage", describeAlert(blocked[0]))
}

// describeAlert is one line about a spent budget.
func describeAlert(al usage.Alert) string {
	what := "today's budget"
	if al.Scope == usage.ScopeTask {
		what = "this task's budget"
	}
	if al.Kind == usage.KindTokens {
		return fmt.Sprintf("%s of %.0f tokens is spent (%.0f)", what, al.Limit, al.Spent)
	}
	est := ""
	if al.Estimated {
		est = ", partly estimated"
	}
	return fmt.Sprintf("%s of $%.2f is spent ($%.2f%s)", what, al.Limit, al.Spent, est)
}
