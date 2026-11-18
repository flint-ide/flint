package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/feature"
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
	// held are the tasks of a CreateAgentTasks call still running, which may
	// yet be rolled back (true: the task holds a slot; false: it is queued).
	// startQueuedTasks leaves them alone, and their slots outlive startGrace,
	// until the call succeeded or rolled back. Guarded by startMu.
	held map[string]bool
	// starts counts the chats being started in the background (tests wait on it).
	starts sync.WaitGroup
	// afterRequest runs after each request of CreateAgentTasks was created
	// (tests: to interleave the queue deterministically).
	afterRequest func(done int)
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

// hold keeps tasks just created out of the queue's hands until unhold (see
// taskQueue.held).
func (a *App) hold(tasks []agenttask.Task) {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	if a.queue.held == nil {
		a.queue.held = map[string]bool{}
	}
	for _, t := range tasks {
		a.queue.held[t.ID] = !t.Queued
	}
}

func (a *App) unhold(tasks []agenttask.Task) {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	for _, t := range tasks {
		delete(a.queue.held, t.ID)
	}
}

func (a *App) isHeld(id string) bool {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	_, ok := a.queue.held[id]
	return ok
}

// activeTasks counts the tasks that hold a slot: working or waiting for an
// approval, or starting. A joint group holds one slot however many of its
// tasks work (its chat is one agent).
func (a *App) activeTasks() int {
	store := a.taskStore()
	slot := func(id string) string {
		if _, t, ok := store.Find(id); ok && t.Joint != "" {
			return "joint:" + t.Joint
		}
		return id
	}
	active := map[string]bool{}
	for _, id := range a.tasks.Active() {
		active[slot(id)] = true
	}
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	for id, since := range a.queue.starting {
		if time.Since(since) > startGrace && !a.queue.held[id] {
			delete(a.queue.starting, id)
			continue
		}
		active[slot(id)] = true
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
// Restricted Mode folder on its own), no budget pauses new turns, and they
// are not held by a CreateAgentTasks call that may still roll them back. The
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
		return !a.isHeld(q.Task.ID) && open[q.Repo] && isDir(q.Task.Worktree) && a.isTrusted(q.Task.Worktree) && a.budgetPause(q.Task.ID) == nil
	})
	for _, task := range next {
		// A joint group comes as its lead and leaves the queue whole.
		if err := a.dequeue(task); err != nil {
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
// For a task of a joint group it starts the group's one chat, in the lead's
// worktree, with the description of the whole group (jointPrompt); openChat
// gives it the other worktrees as additional folders. For a task a feature
// made (an Origin; the lead's for a joint group) the first message starts
// with that feature's context for it, composed now (taskContext).
func (a *App) startTaskChat(task agenttask.Task) {
	defer func() {
		a.releaseStart(task.ID)
		a.startQueuedTasks()
	}()
	prompt := task.Prompt
	if task.Joint != "" {
		members := a.taskStore().Joint(task.Joint)
		if len(members) == 0 {
			return // removed meanwhile
		}
		if lead := members[0]; lead.ID != task.ID {
			a.releaseStart(task.ID) // the deferred release is the lead's
			task = lead
		}
		prompt = jointPrompt(members)
	}
	if a.chatIn(task.Worktree) {
		return
	}
	if text := a.taskContext(task); text != "" {
		prompt = text + "\n\n" + prompt
	}
	ctx, cancel := context.WithTimeout(a.ctx, agentStartTimeout)
	defer cancel()
	origin := taskOrigin
	if _, err := a.openChat(ctx, ChatRequest{
		Agent: task.Agent, Cwd: task.Worktree, Prompt: prompt, TaskID: task.ID, Origin: &origin,
	}); err != nil {
		a.logf("start the chat of task %s: %v", task.ID, err)
	}
}

// taskContext asks the features for the context of a task (a joint group's
// lead) whose chat starts now: "" for a task without an origin, which no
// feature is asked about.
func (a *App) taskContext(task agenttask.Task) string {
	if task.Origin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(a.ctx, gitTimeout)
	defer cancel()
	return a.features.TaskContext(ctx, feature.TaskStart{
		Origin: task.Origin, Worktree: task.Worktree, Branch: task.Branch, Base: task.Base,
		Joint: task.Joint != "", MCP: a.allowMCP(task.Worktree),
	})
}

// flintStarts: Flint starts the task's chat itself, also on Start now (a
// joint group's one chat, or a task with an origin, whose first message
// Flint composes); the UI starts none for it (TaskView.FlintStarts).
func flintStarts(task agenttask.Task) bool { return task.Joint != "" || task.Origin != "" }

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
// chat's turn begins (or the UI reports that it did not start). A task of a
// joint group takes the whole group off the queue, in one save, and the slot
// is held for its lead (where the chat starts).
func (a *App) dequeue(task agenttask.Task) error {
	ids := []string{task.ID}
	slot := task.ID
	if task.Joint != "" {
		ids = ids[:0]
		for i, m := range a.taskStore().Joint(task.Joint) {
			if i == 0 {
				slot = m.ID
			}
			ids = append(ids, m.ID)
		}
	}
	err := a.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		for _, id := range ids {
			s = s.Update(id, func(t agenttask.Task) agenttask.Task { t.Queued = false; return t })
		}
		return s, nil
	})
	if err == nil {
		a.reserveStart(slot)
	}
	return err
}

// StartQueued starts queued tasks that have a free slot. The UI calls it
// when the window's folders are known or change: after a restart the queue
// waits for its repository to be opened.
func (t *TaskAPI) StartQueued() { t.app.startQueuedTasks() }

// StartNow starts a queued task although no slot is free: the user's
// override. A budget that pauses new turns still holds it back. The UI then
// starts its chat, as for a new task; except for a task of a joint group:
// the whole group leaves the queue and Go starts its one chat itself (in the
// lead's worktree, with every worktree's folder and the group's
// description), and for a task with an origin, whose first message starts
// with its feature's context: the UI starts no chat when the view it gets
// says FlintStarts.
func (t *TaskAPI) StartNow(id string) (TaskView, error) {
	a := t.app
	a.queue.mu.Lock()
	defer a.queue.mu.Unlock()
	task, err := t.liveTask(id)
	if err != nil {
		return TaskView{}, err
	}
	members := []agenttask.Task{task}
	if task.Joint != "" {
		members = a.taskStore().Joint(task.Joint)
	}
	if !slices.ContainsFunc(members, func(m agenttask.Task) bool { return m.Queued }) {
		return TaskView{}, errors.New("the task is not queued (it already started)")
	}
	for _, m := range members {
		switch {
		case !isDir(m.Worktree):
			return TaskView{}, fmt.Errorf("the worktree of %q is gone (%s): remove that task first", m.Title, m.Worktree)
		case task.Joint != "" && !a.isTrusted(m.Worktree):
			// The group's chat would not open (jointFolders): it stays queued.
			return TaskView{}, fmt.Errorf("the worktree %s is not trusted (%w)", m.Worktree, errJointUntrusted)
		}
	}
	if err := a.budgetPause(members[0].ID); err != nil {
		return TaskView{}, err
	}
	if err := a.dequeue(task); err != nil {
		return TaskView{}, err
	}
	task.Queued = false
	a.tasksChanged()
	if flintStarts(task) {
		lead := members[0]
		a.queue.starts.Add(1)
		go func() {
			defer a.queue.starts.Done()
			a.startTaskChat(lead)
		}()
	}
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
