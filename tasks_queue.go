package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/git"
)

// The queue of agent tasks. At most agentTasks.maxParallel tasks work at the
// same time (0: no limit), counted over this window: tasks with a turn
// running (the tracker) plus tasks whose chat the UI is starting. A task
// created beyond that is remembered as queued (state.json), with its branch
// and worktree but no chat. Nothing polls: the queue moves when a task
// settles (taskChanged), a task is removed, the limit or a budget changes,
// or the workspace folders change (StartQueued). Go starts each one itself:
// openChat opens the chat in the task's worktree and sends the description,
// and the UI adopts the chat from "chat:opened" (store/agentTasks.ts). The
// UI still starts the chats of tasks the user creates or starts by hand.

const (
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
	// starts counts the chats being started in the background (tests wait on it).
	starts sync.WaitGroup
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
		a.queue.starts.Add(1)
		go func() {
			defer a.queue.starts.Done()
			a.startTaskChat(task)
		}()
	}
	if len(next) > 0 {
		a.tasksChanged()
	}
}

// taskOrigin labels the first message of a task's chat: nobody typed it in
// that chat, so the transcript shows where it came from.
var taskOrigin = as.Origin{Agent: "Flint", Role: "task"}

// startTaskChat opens the chat of a task that left the queue in its
// worktree and sends the description, then lets go of the slot held for the
// start (a running turn holds it from there on) and lets the queue move on. It
// runs in its own goroutine: starting an agent can take a while and the
// caller is the event pipeline. A chat that already works in the folder (the
// user opened one by hand) is left alone: a task's chat is started once.
func (a *App) startTaskChat(task agenttask.Task) {
	defer func() {
		a.releaseStart(task.ID)
		a.startQueuedTasks()
	}()
	if a.chatIn(task.Worktree) {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, agentStartTimeout)
	defer cancel()
	origin := taskOrigin
	if _, err := a.openChat(ctx, ChatRequest{
		Agent: task.Agent, Cwd: task.Worktree, Prompt: task.Prompt, TaskID: task.ID, Origin: &origin,
	}); err != nil {
		a.logf("start the chat of task %s: %v", task.ID, err)
	}
}

// chatIn reports whether a chat is open in folder.
func (a *App) chatIn(folder string) bool {
	for _, n := range a.orch.All() {
		if cleanFolder(n.Cwd) == cleanFolder(folder) {
			return true
		}
	}
	return false
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
	a.tasksChanged()
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
