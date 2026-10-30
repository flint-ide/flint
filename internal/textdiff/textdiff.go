// Package textdiff computes line diffs for the editor: git gutter markers, the
// side-by-side diff view and the +/− stats of agent edits.
package textdiff

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

const (
	lineBudget = time.Second
	// Inline (word) diffs only run on hunks small enough to stay readable.
	inlineBudget   = 50 * time.Millisecond
	maxInlineBytes = 20_000
)

// Hunk is one changed block as 0-based, half-open line ranges: lines
// [FromA, ToA) of the old text became lines [FromB, ToB) of the new one.
type Hunk struct {
	FromA int `json:"fromA"`
	ToA   int `json:"toA"`
	FromB int `json:"fromB"`
	ToB   int `json:"toB"`
	// Inline marks changed words inside a modified hunk (Compare only).
	Inline []Range `json:"inline,omitempty"`
}

// Range is a changed stretch inside a hunk, in UTF-16 code units (JavaScript
// string offsets) from the hunk's first character, its lines joined with "\n".
type Range struct {
	FromA int `json:"fromA"`
	ToA   int `json:"toA"`
	FromB int `json:"fromB"`
	ToB   int `json:"toB"`
}

// LineKind classifies a line of the new text against the old one.
type LineKind string

const (
	Added    LineKind = "added"
	Modified LineKind = "modified"
	Deleted  LineKind = "deleted" // lines were removed just before this one
)

// LineMark is a gutter marker on a 1-based line of the new text.
type LineMark struct {
	Line int      `json:"line"`
	Kind LineKind `json:"kind"`
}

// Stat counts added and deleted lines.
type Stat struct {
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

// SplitLines splits like CodeMirror: on "\n", dropping a "\r" before it, and an
// empty text is one empty line.
func SplitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// Lines diffs two texts line by line.
func Lines(a, b string) []Hunk {
	return lineHunks(SplitLines(a), SplitLines(b))
}

func lineHunks(la, lb []string) []Hunk {
	ids := make(map[string]int, len(la))
	ta, tb := tokenize(la, ids), tokenize(lb, ids)
	spans := diffTokens(ta, tb, time.Now().Add(lineBudget))
	hunks := make([]Hunk, len(spans))
	for i, s := range spans {
		hunks[i] = Hunk{FromA: s.A0, ToA: s.A1, FromB: s.B0, ToB: s.B1}
	}
	return hunks
}

// Compare diffs two texts by line and marks changed words in modified hunks.
func Compare(a, b string) []Hunk {
	la, lb := SplitLines(a), SplitLines(b)
	hunks := lineHunks(la, lb)
	for i, h := range hunks {
		if h.FromA < h.ToA && h.FromB < h.ToB {
			hunks[i].Inline = inline(strings.Join(la[h.FromA:h.ToA], "\n"), strings.Join(lb[h.FromB:h.ToB], "\n"))
		}
	}
	return hunks
}

// Count sums the added and deleted lines of a diff.
func Count(hunks []Hunk) Stat {
	var s Stat
	for _, h := range hunks {
		s.Added += h.ToB - h.FromB
		s.Deleted += h.ToA - h.FromA
	}
	return s
}

// Marks classifies lines of current against base for the git gutter. A deletion
// is marked on the line after the removed block (the last line at the end).
func Marks(base, current string) []LineMark {
	lb := SplitLines(current)
	kinds := make(map[int]LineKind)
	for _, h := range lineHunks(SplitLines(base), lb) {
		if h.FromB == h.ToB {
			line := min(h.FromB, len(lb)-1) + 1
			if _, ok := kinds[line]; !ok {
				kinds[line] = Deleted
			}
			continue
		}
		kind := Modified
		if h.FromA == h.ToA {
			kind = Added
		}
		for l := h.FromB; l < h.ToB; l++ {
			kinds[l+1] = kind
		}
	}
	marks := make([]LineMark, 0, len(kinds))
	for line, kind := range kinds {
		marks = append(marks, LineMark{Line: line, Kind: kind})
	}
	slices.SortFunc(marks, func(x, y LineMark) int { return x.Line - y.Line })
	return marks
}

func tokenize(items []string, ids map[string]int) []int {
	out := make([]int, len(items))
	for i, s := range items {
		id, ok := ids[s]
		if !ok {
			id = len(ids)
			ids[s] = id
		}
		out[i] = id
	}
	return out
}

// inline diffs two hunk texts word by word and returns the changed ranges.
func inline(a, b string) []Range {
	if len(a)+len(b) > maxInlineBytes {
		return nil
	}
	wa, wb := words(a), words(b)
	ids := make(map[string]int)
	ta, tb := tokenize(texts(wa), ids), tokenize(texts(wb), ids)
	spans := diffTokens(ta, tb, time.Now().Add(inlineBudget))
	// A diff where everything changed says nothing the line colours don't.
	if len(spans) == 1 && spans[0] == (span{0, len(ta), 0, len(tb)}) {
		return nil
	}
	out := make([]Range, len(spans))
	for i, s := range spans {
		out[i] = Range{
			FromA: wordOffset(wa, s.A0), ToA: wordOffset(wa, s.A1),
			FromB: wordOffset(wb, s.B0), ToB: wordOffset(wb, s.B1),
		}
	}
	return out
}

type word struct {
	text  string
	start int // UTF-16 offset
}

// words splits text into identifier-like runs and single other characters.
func words(s string) []word {
	var out []word
	pos := 0
	runStart, runPos := -1, 0
	flush := func(end int) {
		if runStart >= 0 {
			out = append(out, word{s[runStart:end], runPos})
			runStart = -1
		}
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			if runStart < 0 {
				runStart, runPos = i, pos
			}
		} else {
			flush(i)
			out = append(out, word{string(r), pos})
		}
		pos += utf16.RuneLen(r)
	}
	flush(len(s))
	return append(out, word{"", pos}) // sentinel: end offset
}

func texts(ws []word) []string {
	out := make([]string, len(ws)-1)
	for i := range out {
		out[i] = ws[i].text
	}
	return out
}

func wordOffset(ws []word, i int) int { return ws[i].start }
