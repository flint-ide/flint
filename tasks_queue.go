package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"flint/internal/agenttask"
	"flint/internal/git"
)

// The queue of agent tasks. At most agentTasks.maxParallel tasks work at the
// same time (0: no limit), counted over this window: tasks with a turn
// running (the tracker) plus tasks whose chat the UI is starting. A task
// created beyond that is remembered as queued (state.json), with its branch
// and worktree but no chat. Nothing polls: the queue moves when a task
// settles (taskChanged), a task is removed, the limit or a budget changes,
// or the workspace folders change (StartQueued). Go decides and announces
// each start with "tasks:start"; the UI opens the folder, starts the chat
// and sends the description, like for a new task.

const (
	// eventTaskStart tells the UI to start the chat of a task (its id) that
	// left the queue.
	eventTaskStart = "tasks:start"
	// startGrace is how long a task counts as starting without a turn: the
	// agent's own start timeout and some room. A UI that never reports back
	// (a reload) frees the slot after it.
	startGrace = 2 * agentStartTimeout
)

// taskQueue is the queue's state that is not worth remembering.
type taskQueue struct {
	// mu makes "is a slot free? then take it" one step.
	mu sync.Mutex
	// startMu guards starting: tasks given a slot whose turn has not begun.
	startMu  sync.Mutex
	starting map[string]time.Time
}

func (a *App) reserveStart(id string) {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	if a.queue.starting == nil {
		a.queue.starting = map[string]time.Time{}
	}
	a.queue.starting[id] = time.Now()
}

func (a *App) releaseStart(id string) {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	delete(a.queue.starting, id)
}

// activeTasks counts the tasks that hold a slot: working or waiting for an
// approval, or starting.
func (a *App) activeTasks() int {
	active := map[string]bool{}
	for _, id := range a.tasks.Active() {
		active[id] = true
	}
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	for id, since := range a.queue.starting {
		if time.Since(since) > startGrace {
			delete(a.queue.starting, id)
			continue
		}
		active[id] = true
	}
	return len(active)
}

// budgetPause is why a task may not start a turn now: a budget is spent and
// usage.pauseOverBudget is on (the rule AgentChatAPI.Prompt applies to a
// chat, here for a task whose chat does not exist yet).
func (a *App) budgetPause(taskID string) error {
	s := a.currentSettings().Usage
	if !s.PauseOverBudget || a.usage == nil {
		return nil
	}
	blocked := a.usage.Blocked(s.Limits(), taskID, s.Prices)
	if len(blocked) == 0 {
		return nil
	}
	return fmt.Errorf("paused: %s. Raise the budget in Settings ▸ Usage, or choose Continue anyway in a chat's budget warning", describeAlert(blocked[0]))
}

// openRepos is the set of repositories (common git dirs, the task store's
// keys) that have a folder open in this window.
func (a *App) openRepos(ctx context.Context) map[string]bool {
	open := map[string]bool{}
	for _, root := range a.projectRoots() {
		if r, err := git.Open(ctx, root); err == nil {
			open[cleanFolder(r.CommonDir)] = true
		}
	}
	return open
}

// startQueuedTasks gives free slots to the oldest queued tasks that may
// start by themselves: their repository is open in this window, their
// worktree exists and is trusted (Flint never starts an agent in a
// Restricted Mode folder on its own), and no budget pauses new turns. The
// others stay queued until that changes or the user starts them.
func (a *App) startQueuedTasks() {
	a.queue.mu.Lock()
	defer a.queue.mu.Unlock()
	store := a.taskStore()
	if len(store.QueuedTasks()) == 0 {
		return
	}
	free := agenttask.Slots(a.currentSettings().AgentTasks.MaxParallel, a.activeTasks())
	if free == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, gitTimeout)
	defer cancel()
	open := a.openRepos(ctx)
	next := agenttask.NextQueued(store, free, func(q agenttask.QueuedTask) bool {
		return open[q.Repo] && isDir(q.Task.Worktree) && a.isTrusted(q.Task.Worktree) && a.budgetPause(q.Task.ID) == nil
	})
	for _, task := range next {
		if err := a.dequeue(task.ID); err != nil {
			a.logf("start queued task: %v", err)
			return
		}
		a.emit(eventTaskStart, task.ID)
	}
	if len(next) > 0 {
		a.emit(eventTasksChanged)
	}
}

// dequeue takes a task off the queue and holds a slot for it until its
// chat's turn begins (or the UI reports that it did not start).
func (a *App) dequeue(id string) error {
	err := a.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		return s.Update(id, func(t agenttask.Task) agenttask.Task { t.Queued = false; return t }), nil
	})
	if err == nil {
		a.reserveStart(id)
	}
	return err
}

// StartQueued starts queued tasks that have a free slot. The UI calls it
// when the window's folders are known or change: after a restart the queue
// waits for its repository to be opened.
func (t *TaskAPI) StartQueued() { t.app.startQueuedTasks() }

// StartNow starts a queued task although no slot is free: the user's
// override. A budget that pauses new turns still holds it back. The UI then
// starts its chat, as for a new task.
func (t *TaskAPI) StartNow(id string) (TaskView, error) {
	a := t.app
	a.queue.mu.Lock()
	defer a.queue.mu.Unlock()
	task, err := t.liveTask(id)
	if err != nil {
		return TaskView{}, err
	}
	if !task.Queued {
		return TaskView{}, errors.New("the task is not queued (it already started)")
	}
	if err := a.budgetPause(id); err != nil {
		return TaskView{}, err
	}
	if err := a.dequeue(id); err != nil {
		return TaskView{}, err
	}
	task.Queued = false
	a.emit(eventTasksChanged)
	ctx, cancel := t.ctx(gitTimeout)
	defer cancel()
	r, err := t.git.repo(ctx, task.Worktree)
	if err != nil {
		return TaskView{}, err
	}
	return t.view(ctx, r, task), nil
}

// StartDone is the UI's report that it finished starting a task's chat,
// whether the turn began or not: the slot held for the start is released (a
// running turn holds it from here on), and the queue moves on.
func (t *TaskAPI) StartDone(id string) error {
	if !agenttask.ValidID(id) {
		return errors.New("invalid task id")
	}
	t.app.releaseStart(id)
	t.app.startQueuedTasks()
	return nil
}
