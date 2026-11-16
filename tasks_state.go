package main

import (
	"flint/internal/agenttask"
	"flint/internal/config"
)

// Agent tasks live in state.json (appState.AgentTasks), per repository. Their
// live state comes from the agent chats running in their worktrees: the
// tracker (App.tasks) is bound when a chat starts or resumes there and fed
// every agent event (newAgentManager).

// eventTasksChanged fires when a task is added, removed or changes state.
const eventTasksChanged = "tasks:changed"

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

// taskChanged remembers a task's settled state (so a restart still shows
// finished or failed), forgets it when a new turn starts, and tells the UI.
// A chat closing (idle) leaves the remembered state alone. A task that
// settles frees its slot for the oldest queued task (tasks_queue.go).
func (a *App) taskChanged(id string, l agenttask.Live) {
	settled := agenttask.Settled(l.State)
	errText := ""
	if settled == agenttask.Failed {
		errText = l.Error
	}
	_, task, ok := a.taskStore().Find(id)
	busy := agenttask.Busy(l.State)
	// A turn in a queued task's folder (a chat the user started there by
	// hand) means it is not waiting any more.
	unqueue := busy && task.Queued
	if ok && l.State != agenttask.Idle && (task.State != settled || task.Error != errText || unqueue) {
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
	a.emit(eventTasksChanged)
	if busy {
		a.releaseStart(id) // the running turn holds the slot from here on
	} else {
		a.startQueuedTasks() // settled (finished, failed, cancelled or closed): a slot is free
	}
}
