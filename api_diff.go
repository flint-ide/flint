package main

import "flint/internal/textdiff"

// DiffAPI computes line diffs for the editor gutter, the diff tab and agent edit stats.
type DiffAPI struct{}

// Marks classifies lines of current against base for the git gutter.
func (DiffAPI) Marks(base, current string) []textdiff.LineMark {
	return textdiff.Marks(base, current)
}

// Hunks diffs two texts line by line (agent edit previews and +/− stats).
func (DiffAPI) Hunks(a, b string) []textdiff.Hunk {
	return nonNil(textdiff.Lines(a, b))
}

// Compare diffs two texts by line and marks changed words (side-by-side view).
func (DiffAPI) Compare(a, b string) []textdiff.Hunk {
	return nonNil(textdiff.Compare(a, b))
}

// nonNil keeps the frontend contract: arrays, never null.
func nonNil(h []textdiff.Hunk) []textdiff.Hunk {
	if h == nil {
		return []textdiff.Hunk{}
	}
	return h
}
