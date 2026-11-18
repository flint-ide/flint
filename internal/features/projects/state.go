package projects

import (
	"slices"
	"time"
)

// The feature's state.json keys. The link between items and agent tasks and
// the checkouts chosen with Locate… are local: they never go into a project.
const (
	// stateProjects: the folders opened or created with Open / New Project,
	// most recent first.
	stateProjects = "projects"
	// stateCheckouts: the folder chosen with Locate… per repository (RepoKey).
	stateCheckouts = "projectCheckouts"
	// stateTasks: the agent tasks of each item, per project folder.
	stateTasks = "projectTasks"
	// stateWaiting: the items the user asked to start when they are ready,
	// per project folder.
	stateWaiting = "projectWaiting"
)

// Bounds of what state.json keeps.
const (
	MaxRemembered     = 20
	maxCheckouts      = 200
	maxLinkedProjects = 100
	// MaxTasksPerItem bounds the agent tasks an item remembers: delegating
	// again adds tasks, and removed ones are dropped first.
	MaxTasksPerItem = 100
	// MaxWaiting bounds the waiting starts of one project.
	MaxWaiting = 100
)

// Waiting is an item the user asked to start when it is ready (Delegate on
// an item that was not): Flint delegates it, as a click would, once the
// last item it waits for is done.
type Waiting struct {
	// Root is the window's active folder when the user asked, for an
	// item without repositories (checked again when it starts).
	Root  string    `json:"root,omitempty"`
	Since time.Time `json:"since"`
	// Reason says why the last start failed ("" before any); Tried is what
	// that start saw (fingerprint): it is tried again only once something
	// relevant changed.
	Reason string `json:"reason,omitempty"`
	Tried  string `json:"tried,omitempty"`
}

// waits are the waiting starts of each project (folder → item id).
type waits map[string]map[string]Waiting

// pruneProjects drops the waiting starts of projects that are not known
// (keep) once more than maxLinkedProjects have some.
func (w waits) pruneProjects(keep func(folder string) bool) {
	if len(w) <= maxLinkedProjects {
		return
	}
	for folder := range w {
		if !keep(folder) {
			delete(w, folder)
		}
	}
}

// Checkout is a folder chosen with Locate… for a repository.
type Checkout struct {
	Repo   string `json:"repo"` // RepoKey of the repository's URL
	Folder string `json:"folder"`
}

// links are the agent tasks of each item of each project (folder → id).
type links map[string]map[string][]Link

// remember puts folder first in list, at most MaxRemembered.
func remember(list []string, folder string) []string {
	out := append([]string{folder}, slices.DeleteFunc(slices.Clone(list), func(f string) bool { return f == folder })...)
	if len(out) > MaxRemembered {
		out = out[:MaxRemembered]
	}
	return out
}

// forget drops folder from list.
func forget(list []string, folder string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(f string) bool { return f == folder })
}

// setCheckout makes folder repo's checkout, most recent first, bounded.
func setCheckout(list []Checkout, repo, folder string) []Checkout {
	out := append([]Checkout{{Repo: repo, Folder: folder}}, slices.DeleteFunc(slices.Clone(list), func(c Checkout) bool { return c.Repo == repo })...)
	if len(out) > maxCheckouts {
		out = out[:maxCheckouts]
	}
	return out
}

// checkoutOf is the folder chosen for repo ("" when none).
func checkoutOf(list []Checkout, repo string) string {
	for _, c := range list {
		if c.Repo == repo {
			return c.Folder
		}
	}
	return ""
}

// addLinks appends new tasks to an item's links. Over MaxTasksPerItem, the
// oldest removed tasks go first; ok is false when that is not enough.
func addLinks(list, added []Link) (out []Link, ok bool) {
	out = append(slices.Clone(list), added...)
	for len(out) > MaxTasksPerItem {
		i := slices.IndexFunc(out, func(l Link) bool { return l.Gone })
		if i < 0 {
			return list, false
		}
		out = slices.Delete(out, i, i+1)
	}
	return out, true
}

// pruneProjects drops the links of projects that are not known (keep) once
// more than maxLinkedProjects have links.
func (l links) pruneProjects(keep func(folder string) bool) {
	if len(l) <= maxLinkedProjects {
		return
	}
	for folder := range l {
		if !keep(folder) {
			delete(l, folder)
		}
	}
}

// taskIDs are the ids of the links that are not gone.
func taskIDs(items map[string][]Link) []string {
	var ids []string
	for _, list := range items {
		for _, l := range list {
			if !l.Gone {
				ids = append(ids, l.Task)
			}
		}
	}
	return ids
}
