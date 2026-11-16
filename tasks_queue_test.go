package main

import (
	"context"
	"os"
	"strings"
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/config"
)

// queueFixture is a task fixture whose repository is trusted (so queued
// tasks may start by themselves) with a limit of working tasks.
func queueFixture(t *testing.T, limit int) *taskFixture {
	t.Helper()
	f := newTaskFixture(t)
	f.app.settings.AgentTasks.MaxParallel = limit
	if err := f.app.setTrust(f.repo, true); err != nil {
		t.Fatal(err)
	}
	return f
}

// work plays the UI starting a task's chat and sending the description.
func (f *taskFixture) work(v TaskView, session string) {
	f.t.Helper()
	f.app.tasks.Bind(session, v.Worktree)
	f.app.tasks.Prompted(session)
	if err := f.api.StartDone(v.ID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *taskFixture) settle(session string) {
	f.app.tasks.Observe(as.Event{SessionID: session, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
}

// states maps the given tasks to their states in the panel, in order.
func (f *taskFixture) states(tasks ...TaskView) string {
	f.t.Helper()
	list, err := f.api.List(f.repo)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, v := range tasks {
		state := "gone"
		for _, l := range list {
			if l.ID == v.ID {
				state = l.State
			}
		}
		out = append(out, state)
	}
	return strings.Join(out, " ")
}

func TestTaskQueueLimit(t *testing.T) {
	f := queueFixture(t, 2)
	a, b := f.create("first"), f.create("second")
	// Both hold a slot from the moment they are created (their chats are
	// starting), so the third waits although no turn is running yet.
	c := f.create("third")
	if got := f.states(a, b, c); got != "idle idle queued" {
		t.Fatalf("states = %s", got)
	}
	// A queued task has its branch and worktree, from the same commit.
	if gitT(t, c.Worktree, "branch", "--show-current") != c.Branch {
		t.Error("the queued task has no worktree on its branch")
	}
	var saved appState
	if err := config.LoadJSON(f.app.statePath(), &saved); err != nil {
		t.Fatal(err)
	}
	if _, task, _ := saved.AgentTasks.Find(c.ID); !task.Queued {
		t.Error("the queue is not in state.json")
	}
	if _, task, _ := saved.AgentTasks.Find(a.ID); task.Queued {
		t.Error("a started task is saved as queued")
	}
}

func TestTaskQueueNoLimit(t *testing.T) {
	f := queueFixture(t, 0)
	var all []TaskView
	for _, d := range []string{"one", "two", "three", "four", "five"} {
		all = append(all, f.create(d))
	}
	if got := f.states(all...); strings.Contains(got, "queued") {
		t.Errorf("0 means no limit: %s", got)
	}
}

func TestTaskQueueStartsOldestWhenATaskSettles(t *testing.T) {
	f := queueFixture(t, 2)
	a, b := f.create("first"), f.create("second")
	f.work(a, "sa")
	f.work(b, "sb")
	c, d := f.create("third"), f.create("fourth")
	if got := f.states(a, b, c, d); got != "working working queued queued" {
		t.Fatalf("states = %s", got)
	}
	// Waiting for an approval is still a running turn.
	f.app.tasks.Observe(as.Event{SessionID: "sa", Kind: as.KindPermission})
	if got := f.states(c, d); got != "queued queued" {
		t.Fatalf("a waiting task freed its slot: %s", got)
	}
	f.settle("sa")
	if got := f.states(a, b, c, d); got != "finished working idle queued" {
		t.Fatalf("after the first settled: %s", got)
	}
	// The third holds the slot while its chat starts, then by its turn.
	f.settle("sa") // no change of state: nothing more starts
	if got := f.states(d); got != "queued" {
		t.Fatalf("the starting task did not hold its slot: %s", got)
	}
	f.work(c, "sc")
	// A failed turn and a closed chat free a slot like a finished one.
	f.app.tasks.Observe(as.Event{SessionID: "sb", Kind: as.KindTurnEnd, StopReason: as.StopError})
	if got := f.states(a, b, c, d); got != "finished failed working idle" {
		t.Fatalf("after the second failed: %s", got)
	}
}

func TestTaskQueueStartThatFailsFreesTheSlot(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	b, c := f.create("second"), f.create("third")
	if got := f.states(a, b, c); got != "idle queued queued" {
		t.Fatalf("states = %s", got)
	}
	// The UI could not start the first one's chat: it reports back, and the
	// next in line gets the slot.
	if err := f.api.StartDone(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.states(a, b, c); got != "idle idle queued" {
		t.Fatalf("after a failed start: %s", got)
	}
	if err := f.api.StartDone("nope"); err == nil {
		t.Error("invalid id accepted")
	}
}

func TestTaskQueueStartNow(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	f.work(a, "sa")
	b, c := f.create("second"), f.create("third")
	v, err := f.api.StartNow(c.ID) // out of order, and beyond the limit
	if err != nil || v.State != agenttask.Idle || v.ID != c.ID {
		t.Fatalf("StartNow = %+v, %v", v, err)
	}
	if got := f.states(a, b, c); got != "working queued idle" {
		t.Fatalf("states = %s", got)
	}
	if _, err := f.api.StartNow(c.ID); err == nil || !strings.Contains(err.Error(), "not queued") {
		t.Errorf("starting it twice = %v", err)
	}
	if _, err := f.api.StartNow("0123456789abcdef"); err == nil {
		t.Error("unknown task accepted")
	}
	// The override counts: with two working, one settling does not start the second.
	f.work(c, "sc")
	f.settle("sa")
	if got := f.states(b); got != "queued" {
		t.Errorf("the limit was exceeded by the queue itself: %s", got)
	}
	f.settle("sc")
	if got := f.states(b); got != "idle" {
		t.Errorf("after both settled: %s", got)
	}
}

func TestTaskQueueRemoval(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	f.work(a, "sa")
	b, c := f.create("second"), f.create("third")
	// A queued task is removed like any other, and nothing starts for it.
	if _, err := f.api.Remove(b.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.Worktree); !os.IsNotExist(err) {
		t.Error("the queued task's worktree was kept")
	}
	if got := f.states(a, b, c); got != "working gone queued" {
		t.Fatalf("states = %s", got)
	}
	// Removing the working task frees its slot.
	if _, err := f.api.Remove(a.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.states(a, c); got != "gone idle" {
		t.Fatalf("after removing the working task: %s", got)
	}
}

func TestTaskQueueRaisedLimitStartsQueued(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	f.work(a, "sa")
	b, c := f.create("second"), f.create("third")
	f.app.settings.AgentTasks.MaxParallel = 2
	f.api.StartQueued()
	if got := f.states(a, b, c); got != "working idle queued" {
		t.Fatalf("states = %s", got)
	}
}

func TestTaskQueueAfterRestart(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second")

	// A new process: the same state.json, no chats, no folder open yet.
	next := &App{configDir: f.app.configDir, settings: f.app.settings, ctx: context.Background()}
	if err := config.LoadJSON(next.statePath(), &next.state); err != nil {
		t.Fatal(err)
	}
	next.tasks = agenttask.NewTracker(next.taskByFolder, next.taskChanged)
	api := &TaskAPI{app: next, git: &GitAPI{app: next}}
	queued := func() bool {
		_, task, ok := next.taskStore().Find(b.ID)
		return ok && task.Queued
	}
	if !queued() {
		t.Fatal("the queue did not survive the restart")
	}
	api.StartQueued()
	if !queued() {
		t.Fatal("a queued task started without its repository open")
	}
	// Another folder open: still not this repository.
	other := t.TempDir()
	next.openProject(other, nil)
	api.StartQueued()
	if !queued() {
		t.Fatal("a queued task started with another folder open")
	}
	next.openProject(f.repo, nil)
	api.StartQueued()
	if queued() {
		t.Fatal("the queued task did not start once its repository was open and a slot free")
	}
	// It holds the slot again in the new process.
	if next.activeTasks() != 1 {
		t.Errorf("active = %d", next.activeTasks())
	}
}

func TestTaskQueueNeverStartsInAnUntrustedFolder(t *testing.T) {
	f := newTaskFixture(t) // the repository is not trusted
	f.app.settings.AgentTasks.MaxParallel = 1
	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second")
	if f.app.isTrusted(b.Worktree) {
		t.Fatal("the worktree of an untrusted repository is trusted")
	}
	f.settle("sa")
	f.api.StartQueued()
	if got := f.states(a, b); got != "finished queued" {
		t.Fatalf("a queued task started by itself in Restricted Mode: %s", got)
	}
	// The user may start it, as they may start a new task there.
	if _, err := f.api.StartNow(b.ID); err != nil {
		t.Fatal(err)
	}
	// Trusting the worktree lets the queue start it.
	c := f.create("third")
	if got := f.states(c); got != "queued" {
		t.Fatalf("states = %s", got)
	}
	if err := f.app.setTrust(c.Worktree, true); err != nil {
		t.Fatal(err)
	}
	if err := f.api.StartDone(b.ID); err != nil { // its chat did not start
		t.Fatal(err)
	}
	if got := f.states(c); got != "idle" {
		t.Fatalf("after trusting its folder: %s", got)
	}
}

func TestTaskQueueRespectsBudgetPause(t *testing.T) {
	f := queueFixture(t, 1)
	f.app.settings.Usage.DailyBudgetTokens = 100
	f.app.settings.Usage.PauseOverBudget = true
	f.app.openUsage()
	t.Cleanup(f.app.closeUsage)
	usageAPI := &UsageAPI{app: f.app}

	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second")
	// The first task's turn spends today's budget.
	f.app.usageChats.bind("sa", usageChat{chat: "sa", agent: "fake", cwd: a.Worktree})
	f.app.recordUsage("sa", mustChat(t, f.app, "sa"), &as.Usage{InputTokens: 90, OutputTokens: 20})
	f.settle("sa")
	if got := f.states(a, b); got != "finished queued" {
		t.Fatalf("a queued task started over budget: %s", got)
	}
	if _, err := f.api.StartNow(b.ID); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("Start now bypassed the budget pause: %v", err)
	}
	if got := f.states(b); got != "queued" {
		t.Fatalf("a refused start took the task off the queue: %s", got)
	}
	// What the queue reports is what the panel shows for the task.
	list, _ := f.api.List(f.repo)
	if list[0].Tokens != 110 {
		t.Errorf("tokens of the first task = %d", list[0].Tokens)
	}
	// "Continue anyway" lifts the pause, and the queue moves.
	if err := usageAPI.Override("sa"); err != nil {
		t.Fatal(err)
	}
	if got := f.states(b); got != "idle" {
		t.Fatalf("after Continue anyway: %s", got)
	}

	// Without the pause setting a spent budget only warns.
	f.work(b, "sb")
	f.app.settings.Usage.PauseOverBudget = false
	c := f.create("third")
	f.app.recordUsage("sa", mustChat(t, f.app, "sa"), &as.Usage{InputTokens: 500})
	f.settle("sb")
	if got := f.states(c); got != "idle" {
		t.Fatalf("without pauseOverBudget: %s", got)
	}
}

func TestTaskTurnTakesItOffTheQueue(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second")
	// The user opened the queued task's folder and sent a message there.
	f.app.tasks.Bind("sb", b.Worktree)
	f.app.tasks.Prompted("sb")
	if got := f.states(b); got != "working" {
		t.Fatalf("state = %s", got)
	}
	if _, task, _ := f.app.taskStore().Find(b.ID); task.Queued {
		t.Error("a working task is still queued")
	}
}
