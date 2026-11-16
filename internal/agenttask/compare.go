package agenttask

import (
	"fmt"
	"slices"
	"strings"

	"flint/internal/textdiff"
)

// Comparing the tasks of a group: which files any of them changed, with each
// task's line counts, and one file's changes as unified diff lines.

// FileChange is what one task did to one file since the merge base.
type FileChange struct {
	// OrigPath is the path the file was renamed from ("" when it was not).
	OrigPath  string `json:"origPath"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	Binary    bool   `json:"binary"`
	Untracked bool   `json:"untracked"`
}

// FileRow is one file of a comparison. Changes has one entry per compared
// task, in their order: nil for a task that left the file alone.
type FileRow struct {
	Path    string        `json:"path"`
	Changes []*FileChange `json:"changes"`
}

// Union merges the changed files of several tasks (path → change, one map
// per task) into rows sorted by path, at most limit of them; truncated tells
// that more files were changed.
func Union(perTask []map[string]FileChange, limit int) (rows []FileRow, truncated bool) {
	seen := map[string]bool{}
	var paths []string
	for _, files := range perTask {
		for p := range files {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	slices.Sort(paths)
	if len(paths) > limit {
		paths, truncated = paths[:limit], true
	}
	rows = make([]FileRow, 0, len(paths))
	for _, p := range paths {
		row := FileRow{Path: p, Changes: make([]*FileChange, len(perTask))}
		for i, files := range perTask {
			if c, ok := files[p]; ok {
				row.Changes[i] = &c
			}
		}
		rows = append(rows, row)
	}
	return rows, truncated
}

// Kinds of a DiffLine.
const (
	LineContext = "ctx"
	LineAdded   = "add"
	LineDeleted = "del"
	LineHunk    = "hunk" // a "@@ -a,n +b,m @@" header
)

// maxLineRunes bounds one line of a unified diff (minified files).
const maxLineRunes = 1000

// DiffLine is one line of a unified diff. A and B are the 1-based line
// numbers in the old and the new text (0 where the line is not in it).
type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	A    int    `json:"a"`
	B    int    `json:"b"`
}

// Unified renders the line diff of a → b like "git diff": hunks with context
// lines of unchanged text around them, at most limit lines (truncated tells
// that the diff is longer). Equal texts give no lines. Like git, an empty
// text has no lines and a final line break ends the last line (texts that
// differ only in it are equal here).
func Unified(a, b string, context, limit int) (lines []DiffLine, stat textdiff.Stat, truncated bool) {
	a, b = strings.TrimSuffix(a, "\n"), strings.TrimSuffix(b, "\n")
	hunks := textdiff.Lines(a, b)
	la, lb := textdiff.SplitLines(a), textdiff.SplitLines(b)
	// textdiff counts like the editor, where an empty text is one empty line.
	na, nb := len(la), len(lb)
	if a == "" {
		na = 0
	}
	if b == "" {
		nb = 0
	}
	lines = []DiffLine{}
	switch {
	case na == 0 || nb == 0:
		stat = textdiff.Stat{Added: nb, Deleted: na}
	default:
		stat = textdiff.Count(hunks)
	}
	if len(hunks) == 0 || na+nb == 0 {
		return lines, stat, false
	}
	add := func(l DiffLine) bool {
		if len(lines) >= limit {
			return false
		}
		l.Text = cutLine(l.Text)
		lines = append(lines, l)
		return true
	}
	// old, added and same emit one line each; the editor's phantom line of
	// an empty text is left out.
	old := func(ai int) bool {
		return ai >= na || add(DiffLine{Kind: LineDeleted, Text: la[ai], A: ai + 1})
	}
	added := func(bi int) bool {
		return bi >= nb || add(DiffLine{Kind: LineAdded, Text: lb[bi], B: bi + 1})
	}
	same := func(ai, bi int) bool {
		switch {
		case ai >= na:
			return added(bi)
		case bi >= nb:
			return old(ai)
		}
		return add(DiffLine{Kind: LineContext, Text: la[ai], A: ai + 1, B: bi + 1})
	}
	for i := 0; i < len(hunks); {
		// Hunks whose context touches or overlaps print as one block.
		j := i
		for j+1 < len(hunks) && hunks[j+1].FromA-hunks[j].ToA <= 2*context {
			j++
		}
		fromA, toA := max(0, hunks[i].FromA-context), min(na, hunks[j].ToA+context)
		fromB, toB := max(0, hunks[i].FromB-context), min(nb, hunks[j].ToB+context)
		if !add(DiffLine{Kind: LineHunk, Text: hunkHeader(fromA, toA, fromB, toB)}) {
			return lines, stat, true
		}
		ai, bi := fromA, fromB
		for _, h := range hunks[i : j+1] {
			for ; ai < h.FromA; ai, bi = ai+1, bi+1 {
				if !same(ai, bi) {
					return lines, stat, true
				}
			}
			for ; ai < h.ToA; ai++ {
				if !old(ai) {
					return lines, stat, true
				}
			}
			for ; bi < h.ToB; bi++ {
				if !added(bi) {
					return lines, stat, true
				}
			}
		}
		for ; ai < toA && bi < toB; ai, bi = ai+1, bi+1 {
			if !same(ai, bi) {
				return lines, stat, true
			}
		}
		i = j + 1
	}
	return lines, stat, false
}

// hunkHeader is git's "@@ -start,count +start,count @@" (an empty range
// names the line before it).
func hunkHeader(fromA, toA, fromB, toB int) string {
	start := func(from, to int) int {
		if to == from {
			return from
		}
		return from + 1
	}
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", start(fromA, toA), toA-fromA, start(fromB, toB), toB-fromB)
}

func cutLine(s string) string {
	if len(s) <= maxLineRunes {
		return s
	}
	if r := []rune(s); len(r) > maxLineRunes {
		return string(r[:maxLineRunes]) + "…"
	}
	return s
}
