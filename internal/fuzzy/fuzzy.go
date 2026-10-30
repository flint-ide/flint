// Package fuzzy ranks project paths for quick-open (⌘P). A query matches when its
// characters appear in order; the score rewards matches at word boundaries,
// consecutive runs and matches inside the file name.
package fuzzy

import (
	"container/heap"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

// Match is one ranked path. Indices are the matched characters, counted in
// code points (JavaScript: Array.from(path)), for highlighting.
type Match struct {
	Path    string `json:"path"`
	Indices []int  `json:"indices"`
}

// Target is a path prepared for repeated matching.
type Target struct {
	Path  string
	lower string
	ascii bool
}

// Prepare lowercases a path once so each keystroke only scans.
func Prepare(path string) Target {
	return Target{Path: path, lower: lowerRunes(path), ascii: isASCII(path)}
}

// Search returns the best `limit` matches, highest score first. An empty query
// returns the first `limit` targets in order.
func Search(query string, targets []Target, limit int) []Match {
	if limit <= 0 {
		return []Match{}
	}
	if query == "" {
		out := make([]Match, 0, min(limit, len(targets)))
		for _, t := range targets[:min(limit, len(targets))] {
			out = append(out, Match{Path: t.Path, Indices: []int{}})
		}
		return out
	}
	q := newQuery(query)
	top := &ranking{}
	var buf []int
	for i := range targets {
		s, ok := q.score(&targets[i], &buf)
		if !ok {
			continue
		}
		c := candidate{score: s, length: len(targets[i].Path), index: i}
		if top.Len() < limit {
			heap.Push(top, c)
		} else if c.better((*top)[0]) {
			(*top)[0] = c
			heap.Fix(top, 0)
		}
	}
	out := make([]Match, top.Len())
	for i := len(out) - 1; i >= 0; i-- {
		c := heap.Pop(top).(candidate)
		t := &targets[c.index]
		indices := []int{}
		q.score(t, &indices)
		out[i] = Match{Path: t.Path, Indices: indices}
	}
	return out
}

type query struct {
	ascii       bool
	orig, lower []byte // ASCII form
	rOrig       []rune // general form
	rLower      []rune
}

func newQuery(s string) *query {
	lower := lowerRunes(s)
	return &query{
		ascii: isASCII(s),
		orig:  []byte(s), lower: []byte(lower),
		rOrig: []rune(s), rLower: []rune(lower),
	}
}

// score matches one target; when indices is non-nil it receives the positions.
func (q *query) score(t *Target, indices *[]int) (float64, bool) {
	if q.ascii && t.ascii {
		return bestScore(q.orig, q.lower, bytesOf(t.Path), bytesOf(t.lower), indices)
	}
	// Cheap rejection before converting to runes.
	if !subsequence(q.rLower, t.lower) {
		return 0, false
	}
	return bestScore(q.rOrig, q.rLower, []rune(t.Path), []rune(t.lower), indices)
}

type char interface{ ~byte | ~rune }

// bestScore prefers matching inside the base name: from the name first, then
// from the start of the path.
func bestScore[C char](query, q, target, lower []C, indices *[]int) (float64, bool) {
	nameStart := 0
	for i := len(target) - 1; i >= 0; i-- {
		if target[i] == '/' {
			nameStart = i + 1
			break
		}
	}
	if s, ok := matchFrom(query, q, target, lower, nameStart, nameStart, indices); ok {
		return s, true
	}
	if nameStart == 0 {
		return 0, false
	}
	return matchFrom(query, q, target, lower, nameStart, 0, indices)
}

func matchFrom[C char](query, q, target, lower []C, nameStart, start int, indices *[]int) (float64, bool) {
	if indices != nil {
		*indices = (*indices)[:0]
	}
	total := 0.0
	prev := -2
	first := -1
	ti := start
	for qi, ch := range q {
		// Greedy, but jump to a later word-boundary occurrence when it exists nearby.
		found := indexFrom(lower, ch, ti)
		if found < 0 {
			return 0, false
		}
		// The jump must leave room for the rest of the query, or a real match is lost.
		if b := nextBoundary(target, lower, ch, found); b >= 0 && b != found && b-found < 16 && found != prev+1 &&
			isSubsequence(q[qi+1:], lower[b+1:]) {
			found = b
		}
		s := 1.0
		if found == prev+1 {
			s += 5 // consecutive run
		}
		if isBoundary(target, found) {
			s += 8
		}
		if found >= nameStart {
			s += 3
		}
		if target[found] == query[qi] {
			s++ // exact case
		}
		total += s
		if first < 0 {
			first = found
		}
		if indices != nil {
			*indices = append(*indices, found)
		}
		prev = found
		ti = found + 1
	}
	// Shorter paths and earlier first matches rank higher.
	total -= float64(len(target))*0.05 + float64(first-start)*0.1
	return total, true
}

func isBoundary[C char](target []C, i int) bool {
	if i == 0 {
		return true
	}
	switch before := target[i-1]; before {
	case '/', '\\', '.', '_', '-', ' ':
		return true
	default:
		c := target[i]
		return c >= 'A' && c <= 'Z' && before >= 'a' && before <= 'z' // camelCase hump
	}
}

func nextBoundary[C char](target, lower []C, ch C, from int) int {
	for i := indexFrom(lower, ch, from); i >= 0; i = indexFrom(lower, ch, i+1) {
		if isBoundary(target, i) {
			return i
		}
	}
	return -1
}

func indexFrom[C char](s []C, ch C, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == ch {
			return i
		}
	}
	return -1
}

func isSubsequence[C char](q, s []C) bool {
	i := 0
	for _, c := range s {
		if i < len(q) && c == q[i] {
			i++
		}
	}
	return i == len(q)
}

func subsequence(q []rune, lower string) bool {
	i := 0
	for _, r := range lower {
		if i < len(q) && r == q[i] {
			i++
		}
	}
	return i == len(q)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// candidate orders matches like the old frontend matcher: score, then shorter
// path, then original (listing) order.
type candidate struct {
	score  float64
	length int
	index  int
}

func (c candidate) better(o candidate) bool {
	if c.score != o.score {
		return c.score > o.score
	}
	if c.length != o.length {
		return c.length < o.length
	}
	return c.index < o.index
}

// ranking is a min-heap: the worst kept match sits at the root.
type ranking []candidate

func (r ranking) Len() int           { return len(r) }
func (r ranking) Less(i, j int) bool { return r[j].better(r[i]) }
func (r ranking) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r *ranking) Push(x any)        { *r = append(*r, x.(candidate)) }
func (r *ranking) Pop() any {
	old := *r
	c := old[len(old)-1]
	*r = old[:len(old)-1]
	return c
}

// lowerRunes lowercases rune by rune, so indices line up with the original.
func lowerRunes(s string) string { return strings.Map(unicode.ToLower, s) }

// bytesOf views a string as bytes without copying; the matcher never writes.
func bytesOf(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }
