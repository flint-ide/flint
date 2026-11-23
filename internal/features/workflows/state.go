package workflows

import "slices"

// stateRuns is the state.json key of the runs, newest last.
const stateRuns = "workflowRuns"

// MaxRuns bounds the runs state.json keeps.
const MaxRuns = 100

// prune keeps at most MaxRuns runs, in their order. The oldest (by Updated)
// runs that are over go first; a run that is not over goes only when nothing
// else is left to drop. runs is not changed.
func prune(runs []Run) []Run {
	excess := len(runs) - MaxRuns
	if excess <= 0 {
		return runs
	}
	order := make([]int, len(runs))
	for i := range order {
		order[i] = i
	}
	// Over runs first, then the oldest, then the earliest in the list.
	slices.SortStableFunc(order, func(a, b int) int {
		if oa, ob := runs[a].Over(), runs[b].Over(); oa != ob {
			if oa {
				return -1
			}
			return 1
		}
		switch {
		case runs[a].Updated < runs[b].Updated:
			return -1
		case runs[a].Updated > runs[b].Updated:
			return 1
		}
		return 0
	})
	drop := make(map[int]bool, excess)
	for _, i := range order[:excess] {
		drop[i] = true
	}
	out := make([]Run, 0, MaxRuns)
	for i, r := range runs {
		if !drop[i] {
			out = append(out, r)
		}
	}
	return out
}
