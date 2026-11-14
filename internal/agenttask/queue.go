package agenttask

import (
	"cmp"
	"maps"
	"math"
	"slices"
)

// The queue: at most a set number of tasks work at the same time
// (agentTasks.maxParallel). A task that would exceed it is created with its
// branch and worktree but Queued: its chat starts, and its description is
// sent, when a working task settles and it is the oldest one waiting. The
// worktree is made up front so every task of a group starts from the same
// base commit and a queued task can be inspected and removed like any other.

// Slots is how many more tasks may start with limit (0: no limit) while
// active ones are working or starting.
func Slots(limit, active int) int {
	if limit <= 0 {
		return math.MaxInt
	}
	return max(0, limit-active)
}

// QueuedTask is a queued task and the repository (common git dir) it is
// kept under.
type QueuedTask struct {
	Repo string
	Task Task
}

// QueuedTasks lists the queued tasks of every repository, oldest first
// (tasks created in the same second keep the order they were added in).
func (s Store) QueuedTasks() []QueuedTask {
	var out []QueuedTask
	for _, repo := range slices.Sorted(maps.Keys(s)) {
		for _, t := range s[repo] {
			if t.Queued {
				out = append(out, QueuedTask{Repo: repo, Task: t})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b QueuedTask) int { return cmp.Compare(a.Task.CreatedAt, b.Task.CreatedAt) })
	return out
}

// NextQueued picks the tasks to start now: the oldest queued ones that may start
// (eligible: their repository is open, their folder trusted, no budget in
// the way), at most free of them. A task that may not start is skipped, not
// waited for, so a repository that is closed never blocks another.
func NextQueued(s Store, free int, eligible func(QueuedTask) bool) []Task {
	var out []Task
	for _, q := range s.QueuedTasks() {
		if len(out) >= free {
			break
		}
		if eligible(q) {
			out = append(out, q.Task)
		}
	}
	return out
}
