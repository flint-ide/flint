package backlog

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
	// Repo is the repository's name in flint-backlog.json ("" for the
	// window's active project).
	Repo string `json:"repo,omitempty"`
	// State, PullRequest and Merged are as last seen.
	State       string `json:"state,omitempty"`
	PullRequest string `json:"pullRequest,omitempty"`
	Merged      bool   `json:"merged,omitempty"`
	// Gone: the task was removed.
	Gone bool `json:"gone,omitempty"`
}

// abandoned: removed without being merged or getting a pull request (a
// failed attempt the user threw away); it no longer counts for the item.
func (l Link) abandoned() bool { return l.Gone && l.PullRequest == "" && !l.Merged }

// delivered: merged, or removed after its pull request was created.
func (l Link) delivered() bool { return l.Merged || l.Gone && l.PullRequest != "" }

// finished: the agent is done with it.
func (l Link) finished() bool {
	return l.delivered() || !l.Gone && l.State == agenttask.Finished
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
		}
		if l != out[i] {
			out[i], changed = l, true
		}
	}
	return out, changed
}

// NextStatus is the status an item in status moves to by itself, given its
// agent tasks: doing → review when every task finished (a failed one keeps
// it in doing), review → done when every task was merged or removed after
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

// Failed reports an item with a task whose last turn failed.
func Failed(links []Link) bool {
	return slices.ContainsFunc(links, func(l Link) bool {
		return !l.Gone && !l.delivered() && l.State == agenttask.Failed
	})
}
