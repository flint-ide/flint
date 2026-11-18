package backlog

import (
	"fmt"
	"slices"
	"strings"
)

// Deps is where an item stands with the items it waits for (its after key).
type Deps struct {
	// WaitingFor are the items of After that are not done, in After's order.
	WaitingFor []string
	// Problem says why the item can never be ready as it is written: an id
	// that names no item, the item itself, or a cycle ("" when none).
	Problem string
}

// Ready: every item of After is done, and nothing is wrong with After.
func (d Deps) Ready() bool { return d.Problem == "" && len(d.WaitingFor) == 0 }

// Dependencies works out, for every item of a backlog, which of the items it
// waits for are not done and what is wrong with its after key. An id that
// names no item (or an item whose file is a problem), an item naming itself
// and an item on a cycle are problems of that item; an item that waits for
// an item on a cycle is not, but it is not ready while that one is not done.
func Dependencies(items []Item) map[string]Deps {
	byID := make(map[string]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := make(map[string]Deps, len(items))
	for _, it := range items {
		var d Deps
		var unknown []string
		for _, dep := range it.After {
			other, ok := byID[dep]
			switch {
			case dep == it.ID:
				d.Problem = "after: the item names itself"
			case !ok:
				unknown = append(unknown, dep)
			case other.Status != StatusDone:
				d.WaitingFor = append(d.WaitingFor, dep)
			}
		}
		if d.Problem == "" && len(unknown) > 0 {
			d.Problem = fmt.Sprintf("after: no item %s in this backlog (or its file has a problem)", strings.Join(unknown, ", "))
		}
		if d.Problem == "" {
			if cycle := cycleFrom(it.ID, byID); cycle != nil {
				d.Problem = "after: a cycle: " + strings.Join(cycle, " → ")
			}
		}
		out[it.ID] = d
	}
	return out
}

// cycleFrom returns a cycle through start (start, …, start) along the after
// keys of items, or nil when start is on none. Items that are not known are
// not followed.
func cycleFrom(start string, byID map[string]Item) []string {
	seen := map[string]bool{}
	var path []string
	var walk func(id string) bool
	walk = func(id string) bool {
		path = append(path, id)
		for _, dep := range byID[id].After {
			if dep == start {
				path = append(path, start)
				return true
			}
			if _, ok := byID[dep]; !ok || seen[dep] || dep == id {
				continue
			}
			seen[dep] = true
			if walk(dep) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if walk(start) {
		return slices.Clip(path)
	}
	return nil
}
