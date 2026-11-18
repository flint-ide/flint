package agenttask

import (
	"cmp"
	"slices"
)

// Joint groups: one agent that works in several repositories. Each repository
// still gets a task of its own (branch, worktree, merge, pull request), and
// the tasks share one chat, which runs in the lead's worktree and is given
// the other worktrees as additional folders. A chat in the lead's worktree
// counts for every task of the group (ChatTasks); the group takes one slot of
// the queue and leaves it as a unit (NextQueued).

// MaxJoint bounds the repositories (tasks) of one joint group.
const MaxJoint = 10

// Joint returns the tasks of joint group id: the lead first, then by
// creation time and worktree (a stable order for the chat's folders).
func (s Store) Joint(id string) []Task {
	if id == "" {
		return nil
	}
	var out []Task
	for _, tasks := range s {
		for _, t := range tasks {
			if t.Joint == id {
				out = append(out, t)
			}
		}
	}
	slices.SortFunc(out, func(a, b Task) int {
		if a.Lead != b.Lead {
			if a.Lead {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.Worktree, b.Worktree))
	})
	return out
}

// ChatTasks lists the tasks a chat whose folder is path works for: every
// task of a joint group (lead first) when path is its lead's worktree, else
// the task whose worktree path is; none when path is no task's.
func (s Store) ChatTasks(path string) []string {
	id := s.ByWorktree(path)
	if id == "" {
		return nil
	}
	_, t, _ := s.Find(id)
	if t.Joint == "" || !t.Lead {
		return []string{id}
	}
	var out []string
	for _, m := range s.Joint(t.Joint) {
		out = append(out, m.ID)
	}
	return out
}

// NormalizeJoint keeps joint group id consistent after a task of it was
// removed: a group of one is dissolved (its task is a task on its own), and
// a group without a lead gets one (the first of Joint's order).
func (s Store) NormalizeJoint(id string) Store {
	members := s.Joint(id)
	switch {
	case len(members) == 1:
		return s.Update(members[0].ID, func(t Task) Task { t.Joint, t.Lead = "", false; return t })
	case len(members) > 1 && !members[0].Lead:
		return s.Update(members[0].ID, func(t Task) Task { t.Lead = true; return t })
	}
	return s
}
