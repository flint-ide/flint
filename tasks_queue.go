package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/git"
	"flint/internal/redact"
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
	// down are the agents that could not start for a task the queue started
	// (agent name → why): their other queued tasks wait until the user
	// chooses Start now, the agents' settings change, or a chat of that
	// agent works (agentWorks). first are the tasks whose chat the queue
	// started and whose agent has done nothing yet (watchFirstTurn). Both
	// guarded by startMu.
	down  map[string]string
	first map[string]*firstTurn
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
		return !a.isHeld(q.Task.ID) && open[q.Repo] && isDir(q.Task.Worktree) && a.isTrusted(q.Task.Worktree) &&
			a.agentDown(q.Task.Agent) == "" && a.budgetPause(q.Task.ID) == nil
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
	start := a.taskStart(task)
	if err := a.features.TaskStartCheck(start); err != nil {
		// The task stays off the queue, without a chat: a stopped workflow
		// run never starts an agent later.
		a.logf("start the chat of task %s: not started: %v", task.ID, err)
		a.tasksChanged()
		return
	}
	if text := a.taskContext(task); text != "" {
		prompt = text + "\n\n" + prompt
	}
	ctx, cancel := context.WithTimeout(a.ctx, agentStartTimeout)
	defer cancel()
	origin := taskOrigin
	a.watchFirstTurn(task)
	info, err := a.openChat(ctx, ChatRequest{
		Agent: task.Agent, Cwd: task.Worktree, Prompt: prompt, TaskID: task.ID, Origin: &origin,
		opened: func(id string) { a.features.TaskChatStarted(start, id) },
	})
	if err != nil {
		a.unwatchFirstTurn(task.ID)
		var down *agentStartError
		a.taskStartFailed(task, info.ID, err, errors.As(err, &down))
	}
}

// taskStartFailed marks a task (every task of its joint group) failed when
// the chat Go started for it could not start or take its first message,
// with the reason, so the Agent Tasks panel and the Projects item show it;
// and tells the user (an alert: a toast in front, a notification behind).
// chat is the chat that did open ("" when none did). agentDown: the agent
// itself did not start, not something of this task; then its other queued
// tasks do not start by themselves (agentFailed), and the user is told once
// however many tasks were starting.
func (a *App) taskStartFailed(task agenttask.Task, chat string, err error, agentDown bool) {
	a.logf("start the chat of task %s: %v", task.ID, err)
	if errors.Is(err, as.ErrClosed) || a.ctx.Err() != nil {
		return // Flint is quitting: the task did not fail
	}
	reason := startFailure(err)
	ids := []string{task.ID}
	if task.Joint != "" {
		ids = ids[:0]
		for _, m := range a.taskStore().Joint(task.Joint) {
			ids = append(ids, m.ID)
		}
	}
	if err := a.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		for _, id := range ids {
			s = s.Update(id, func(t agenttask.Task) agenttask.Task {
				t.State, t.Error = agenttask.Failed, reason
				return t
			})
		}
		return s, nil
	}); err != nil {
		a.logf("save task state: %v", err)
	}
	a.tasksChanged()
	if agentDown {
		a.agentFailed(task, chat, reason)
		return
	}
	a.alert("An agent task did not start", task.Title+": "+reason, chatTab(chat))
}

// agentLabel names an agent in a sentence.
func agentLabel(agent string) string { return cmp.Or(agent, "the default agent") }

// chatTab is the editor tab of a chat, "" for none.
func chatTab(chat string) string {
	if chat == "" {
		return ""
	}
	return "chat:" + chat
}

// agentFailed records that task's agent could not start (reason), so the
// queue starts no other task of it by itself, and tells the user, once:
// a second failure while it is recorded (tasks that were starting at the
// same time) says nothing more.
func (a *App) agentFailed(task agenttask.Task, chat, reason string) {
	a.queue.startMu.Lock()
	_, known := a.queue.down[task.Agent]
	if a.queue.down == nil {
		a.queue.down = map[string]string{}
	}
	a.queue.down[task.Agent] = reason
	a.queue.startMu.Unlock()
	a.tasksChanged() // the queued tasks say why they wait
	if known {
		return
	}
	body := task.Title + ": " + reason
	if slices.ContainsFunc(a.taskStore().QueuedTasks(), func(q agenttask.QueuedTask) bool { return q.Task.Agent == task.Agent }) {
		body += ". The other queued tasks of " + agentLabel(task.Agent) + " wait: Start now on one tries again"
	}
	a.alert("An agent task did not start", body, chatTab(chat))
}

// agentDown is why the queue starts no task of agent by itself ("" when it
// may).
func (a *App) agentDown(agent string) string {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	return a.queue.down[agent]
}

// agentUp forgets that agent could not start (every agent for ""), and lets
// the queue move.
func (a *App) agentUp(agent string) {
	a.queue.startMu.Lock()
	n := len(a.queue.down)
	if agent == "" {
		a.queue.down = nil
	} else {
		delete(a.queue.down, agent)
	}
	changed := len(a.queue.down) != n
	a.queue.startMu.Unlock()
	if changed {
		a.tasksChanged()
		go a.startQueuedTasks() // not on the caller's goroutine: it may be the event pipeline
	}
}

// firstTurn is a queue-started task chat whose agent has done nothing yet.
type firstTurn struct{ task agenttask.Task }

// watchFirstTurn follows the first turn of the chat startTaskChat opens for
// task (observeTaskStart), before it opens: its first events may come
// before openChat returns.
func (a *App) watchFirstTurn(task agenttask.Task) {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	if a.queue.first == nil {
		a.queue.first = map[string]*firstTurn{}
	}
	a.queue.first[task.ID] = &firstTurn{task: task}
}

func (a *App) unwatchFirstTurn(id string) {
	a.queue.startMu.Lock()
	defer a.queue.startMu.Unlock()
	delete(a.queue.first, id)
}

// observeTaskStart follows the chats the queue started until their agent
// does something. An error before that (the agent's own, or the Manager's
// for a turn the driver could not begin) is the agent failing to start (a
// process that died at once, a login that expired), not the task:
// taskStartFailed's agentDown case. The tracker marks the task failed with
// the error. Output from a
// chat of an agent recorded as down clears that. It runs for every event
// before the tracker sees it (so the queue moves on with the agent already
// down), and costs a lock and two map lookups when nothing is watched.
func (a *App) observeTaskStart(e as.Event) {
	worked := false
	switch e.Kind {
	case as.KindMessageChunk, as.KindThoughtChunk, as.KindToolCall, as.KindToolCallUpdate, as.KindPlan, as.KindPermission:
		worked = true
	case as.KindError, as.KindTurnEnd:
	default:
		return
	}
	a.queue.startMu.Lock()
	if len(a.queue.first) == 0 && len(a.queue.down) == 0 {
		a.queue.startMu.Unlock()
		return
	}
	var w *firstTurn
	for _, id := range a.tasks.Tasks(e.SessionID) {
		if w = a.queue.first[id]; w != nil {
			break
		}
	}
	failed := ""
	if w != nil {
		// The tracker counts an error as the task failing (and the queue
		// moves on), so the first one decides here too.
		delete(a.queue.first, w.task.ID)
		switch {
		case e.Kind == as.KindError:
			failed = cmp.Or(e.Text, "the agent reported an error")
		case e.Kind == as.KindTurnEnd && e.StopReason == as.StopError:
			failed = "the agent stopped with an error before it began"
		}
	}
	down := ""
	if worked && len(a.queue.down) > 0 && a.orch != nil {
		if node, ok := a.orch.Get(e.SessionID); ok && a.queue.down[node.Agent] != "" {
			down = node.Agent
		}
	}
	a.queue.startMu.Unlock()
	if failed != "" {
		a.agentFailed(w.task, e.SessionID, startFailure(errors.New(failed)))
	}
	if down != "" {
		a.agentUp(down)
	}
}

// maxStartFailure bounds the reason kept for a task whose chat did not start.
const maxStartFailure = 300

// startFailure is the reason a task shows when its chat did not start.
func startFailure(err error) string {
	s := "the agent did not start: " + redact.Secrets(err.Error())
	if r := []rune(s); len(r) > maxStartFailure {
		s = string(r[:maxStartFailure]) + "…"
	}
	return s
}

// queueBlocked says why a queued task does not start by itself although
// startQueuedTasks looks at it (the rules there), "" when it only waits for
// a free slot. open is openRepos.
func (a *App) queueBlocked(repo string, task agenttask.Task, open map[string]bool) string {
	switch {
	case a.isHeld(task.ID):
		return ""
	case !open[repo]:
		return "Its repository is not open in this window: open it, and the task starts when a slot is free"
	case !isDir(task.Worktree):
		return ""
	case !a.isTrusted(task.Worktree):
		return "Its worktree is in Restricted Mode, and Flint never starts an agent there by itself: trust the folder, or choose Start now"
	}
	if why := a.agentDown(task.Agent); why != "" {
		return "Not started by itself: when Flint last started a task of " + agentLabel(task.Agent) + ", " + strings.TrimPrefix(why, "the agent did not start: ") + ". Start now tries again"
	}
	if err := a.budgetPause(task.ID); err != nil {
		return "Paused by the budget: " + strings.TrimPrefix(err.Error(), "paused: ")
	}
	return ""
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
	return a.features.TaskContext(ctx, a.taskStart(task))
}

// taskStart describes the start of task's chat (a joint group's lead) to
// the features.
func (a *App) taskStart(task agenttask.Task) feature.TaskStart {
	return feature.TaskStart{
		Origin: task.Origin, TaskID: task.ID, Worktree: task.Worktree, Branch: task.Branch, Base: task.Base,
		Joint: task.Joint != "", MCP: a.allowMCP(task.Worktree),
	}
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
	// The user tries the agent again: if it starts, the queue moves on;
	// if not, its tasks wait again, and the user is told again.
	a.queue.startMu.Lock()
	delete(a.queue.down, task.Agent)
	a.queue.startMu.Unlock()
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
