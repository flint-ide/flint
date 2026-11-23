package projects

import (
	"slices"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

// Link is one agent task of an item, kept in state.json (the link is local:
// tasks exist on one machine). It remembers what the item needs to know
// after the task is removed: whether it was merged or got a pull request.
type Link struct {
	Task string `json:"task"`
	// Repo is the repository's name in flint-project.json ("" for the
	// window's active folder).
	Repo string `json:"repo,omitempty"`
	// State, PullRequest and Merged are as last seen.
	State       string `json:"state,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
	Merged      bool   `json:"merged,omitempty"`
	// Gone: the task was removed.
	Gone bool `json:"gone,omitempty"`
	// Workflow, RunID and Run are the workflow run that takes the task, as
	// last seen: its workflow, id and state ("" when no run takes it). A
	// run's state, not the task's turns, then says how the task is going: its
	// task finishes a turn after every step.
	Workflow string `json:"workflow,omitempty"`
	RunID    string `json:"runId,omitempty"`
	Run      string `json:"run,omitempty"`
	// RunLast: the run's current step is its last (feature.TaskRun.AtLastStep).
	RunLast bool `json:"runLast,omitempty"`
}

// The run states a link follows (workflows' Run states).
const (
	runRunning = "running"
	runWaiting = "waiting"
	runFailed  = "failed"
	runDone    = "done"
	runStopped = "stopped"
)

// runFailed reports a run that failed or was stopped: the item stays in
// doing, marked.
func (l Link) runFailed() bool { return l.Run == runFailed || l.Run == runStopped }

// abandoned: removed without being merged or getting a pull request (a
// failed attempt the user threw away); it no longer counts for the item.
func (l Link) abandoned() bool { return l.Gone && l.PullRequest == "" && !l.Merged }

// delivered: merged, or removed after its pull request was created.
func (l Link) delivered() bool { return l.Merged || l.Gone && l.PullRequest != "" }

// finished: the agent is done with it; for a task a workflow run takes,
// the run is done, or waits for the user at an approval step that is its last
// step (a run that waits at an earlier approval has steps to take after it; a
// running or paused run is still working, a failed or stopped one failed).
func (l Link) finished() bool {
	if l.delivered() {
		return true
	}
	if l.Workflow != "" {
		return !l.Gone && (l.Run == runDone || l.Run == runWaiting && l.RunLast)
	}
	return !l.Gone && l.State == agenttask.Finished
}

// Refresh updates links from the tasks that still exist (by id); a task
// that is not there any more is marked gone and keeps what was last seen.
func Refresh(links []Link, tasks map[string]feature.AgentTask) (out []Link, changed bool) {
	out = slices.Clone(links)
	for i, l := range out {
		if l.Gone {
			continue
		}
		t, ok := tasks[l.Task]
		if !ok {
			l.Gone = true
		} else {
			l.State, l.Merged = t.State, t.Merged
			if t.PullRequest != "" {
				l.PullRequest = t.PullRequest
			}
			// A run that was removed keeps its last state.
			if t.Run.Workflow != "" {
				l.Workflow, l.RunID, l.Run, l.RunLast = t.Run.Workflow, t.Run.ID, t.Run.State, t.Run.AtLastStep
			}
		}
		if l != out[i] {
			out[i], changed = l, true
		}
	}
	return out, changed
}

// NextStatus is the status an item in status moves to by itself, given its
// agent tasks: doing → review when every task finished (a failed one keeps
// it in doing; a task a workflow run takes has finished when its run is done
// or waits at an approval step that is its last), review → done when every task was merged or removed after
// its pull request was created. Tasks removed without either are left out;
// with none left, nothing moves. Other statuses never move by themselves.
func NextStatus(status string, links []Link) string {
	counted := slices.DeleteFunc(slices.Clone(links), Link.abandoned)
	if len(counted) == 0 {
		return status
	}
	all := func(ok func(Link) bool) bool {
		for _, l := range counted {
			if !ok(l) {
				return false
			}
		}
		return true
	}
	if status == StatusDoing && all(Link.finished) {
		status = StatusReview
	}
	if status == StatusReview && all(Link.delivered) {
		status = StatusDone
	}
	return status
}

// NextStatusAfter is NextStatus for an item whose tasks just changed from prev
// to links: besides what NextStatus does, an item in review goes back to doing
// when a workflow run is running again (the user sent it back, or an approval
// let later steps run) and the move to review was automatic, that is, prev
// alone would have put the item in review. An item the user moved to review by
// hand, while prev did not say so, stays there.
func NextStatusAfter(status string, prev, links []Link) string {
	next := NextStatus(status, links)
	if status != StatusReview || next != StatusReview {
		return next
	}
	working := slices.ContainsFunc(links, func(l Link) bool {
		return l.Workflow != "" && !l.Gone && !l.delivered() && l.Run == runRunning
	})
	if working && NextStatus(StatusDoing, prev) == StatusReview && NextStatus(StatusDoing, links) == StatusDoing {
		return StatusDoing
	}
	return next
}

// Failed reports an item with a task whose last turn failed, or whose
// workflow run failed or was stopped.
func Failed(links []Link) bool {
	return slices.ContainsFunc(links, func(l Link) bool {
		if l.Gone || l.delivered() {
			return false
		}
		if l.Workflow != "" {
			return l.runFailed()
		}
		return l.State == agenttask.Failed
	})
}
