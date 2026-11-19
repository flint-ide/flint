package main

import (
	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/config"
)

// Agent tasks live in state.json (appState.AgentTasks), per repository. Their
// live state comes from the agent chats running in their worktrees: the
// tracker (App.tasks) is bound when a chat starts or resumes there and fed
// every agent event (newAgentManager).

// eventTasksChanged fires when a task is added, removed or changes state.
const eventTasksChanged = "tasks:changed"

// tasksChanged tells the UI (eventTasksChanged) and the feature modules
// (Instance.TasksChanged) that tasks were added, removed or changed.
func (a *App) tasksChanged() {
	a.emit(eventTasksChanged)
	a.features.TasksChanged()
}

// taskStore returns the remembered tasks (never modified in place).
func (a *App) taskStore() agenttask.Store {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.state.AgentTasks
}

// saveTasks replaces the task store with fn's result and saves state.json; on
// any error the store stays as it was.
func (a *App) saveTasks(fn func(agenttask.Store) (agenttask.Store, error)) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	store, err := fn(a.state.AgentTasks)
	if err != nil {
		return err
	}
	next := a.state
	next.AgentTasks = store
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	return nil
}

// taskByFolder is the id of the task whose worktree is cwd ("" for none),
// for binding chat sessions.
func (a *App) taskByFolder(cwd string) string {
	if cwd == "" {
		return ""
	}
	return a.taskStore().ByWorktree(cleanFolder(cwd))
}

// tasksByFolder lists the tasks a chat in cwd works for, for the tracker:
// the task whose worktree cwd is, or every task of a joint group when cwd is
// its lead's worktree (agenttask.Store.ChatTasks).
func (a *App) tasksByFolder(cwd string) []string {
	if cwd == "" {
		return nil
	}
	return a.taskStore().ChatTasks(cleanFolder(cwd))
}

// bindTaskChat follows a chat that started or resumed in cwd (the tracker)
// and, for the task's own conversation (own: not a chat its agent started),
// remembers the session on every task it works for, so Open can resume it
// after a restart even when its tab is gone.
func (a *App) bindTaskChat(info as.SessionInfo, cwd string, own bool) {
	a.tasks.Bind(info.ID, cwd)
	if !own || info.ID == "" {
		return
	}
	ids := a.tasksByFolder(cwd)
	if len(ids) == 0 {
		return
	}
	stale := false
	store := a.taskStore()
	for _, id := range ids {
		if _, t, ok := store.Find(id); ok && (t.Session != info.ID || t.Engine != info.Engine) {
			stale = true
		}
	}
	if !stale {
		return
	}
	err := a.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		for _, id := range ids {
			s = s.Update(id, func(t agenttask.Task) agenttask.Task {
				t.Session, t.Engine = info.ID, info.Engine
				return t
			})
		}
		return s, nil
	})
	if err != nil {
		a.logf("save task session: %v", err)
	}
}

// rememberedState is what a task should remember once its combined live
// state is live, given what it remembers (prev) and whether a chat of it is
// still open; keep: leave prev alone. An idle task with no chat left had its
// chat closed (a turn it cut off stays interrupted); an idle task with an
// open chat had its turn cancelled, which is not an interruption, while a
// finished or failed one stays as it was.
func rememberedState(prev, live string, chatOpen bool) (state string, keep bool) {
	if live != agenttask.Idle {
		return agenttask.Settled(live), false
	}
	if chatOpen && prev == agenttask.Interrupted {
		return "", false
	}
	return prev, true
}

// taskChanged remembers the state a restart shows (finished, failed, or
// interrupted from the moment a turn starts, since a crash leaves no chance
// to save it later) and tells the UI. A chat closing (idle, no chat left)
// leaves the remembered state alone: a turn it cut off stays interrupted. A
// cancelled turn (idle, its chat still open) is not interrupted. A task that
// settles frees its slot for the oldest queued task (tasks_queue.go).
func (a *App) taskChanged(id string, l agenttask.Live) {
	_, task, ok := a.taskStore().Find(id)
	settled, keep := rememberedState(task.State, l.State, len(a.tasks.Sessions(id)) > 0)
	errText := ""
	if settled == agenttask.Failed {
		errText = l.Error
	}
	busy := agenttask.Busy(l.State)
	// A turn in a queued task's folder (a chat the user started there by
	// hand) means it is not waiting any more.
	unqueue := busy && task.Queued
	if ok && !keep && (task.State != settled || task.Error != errText || unqueue) {
		err := a.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
			return s.Update(id, func(t agenttask.Task) agenttask.Task {
				t.State, t.Error = settled, errText
				if unqueue {
					t.Queued = false
				}
				return t
			}), nil
		})
		if err != nil {
			a.logf("save task state: %v", err)
		}
	}
	a.tasksChanged()
	if busy {
		a.releaseStart(id) // the running turn holds the slot from here on
	} else {
		a.startQueuedTasks() // settled (finished, failed, cancelled or closed): a slot is free
	}
}
