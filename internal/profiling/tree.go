package profiling

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/google/pprof/profile"
)

// RootName names the synthetic frame every stack starts from.
const RootName = "root"

// frameRef identifies a function on a stack. Frames are merged by name: two
// call sites of the same function under the same parent are one frame.
type frameRef struct {
	name string
	file string
	line int
}

type node struct {
	frameRef
	self, total int64
	children    map[string]*node
	order       []*node // insertion order, so equal totals sort deterministically
}

func (n *node) child(f frameRef) *node {
	if c := n.children[f.name]; c != nil {
		return c
	}
	if n.children == nil {
		n.children = map[string]*node{}
	}
	c := &node{frameRef: f}
	n.children[f.name] = c
	n.order = append(n.order, c)
	return c
}

type fnStat struct {
	frameRef
	flat, cum int64
	// seen is the last sample counted into cum (recursion counts once).
	seen int
}

// aggregation is the full (untrimmed) call tree and per-function totals of
// one sample type.
type aggregation struct {
	root *node
	fns  map[string]*fnStat
}

func aggregate(p *profile.Profile, index int) *aggregation {
	a := &aggregation{root: &node{frameRef: frameRef{name: RootName}}, fns: map[string]*fnStat{}}
	var stack []frameRef
	for gen, s := range p.Sample {
		if index >= len(s.Value) {
			continue
		}
		v := s.Value[index]
		if v <= 0 {
			continue // diff profiles can be negative; a flame graph shows only positive weight
		}
		stack = appendStack(stack[:0], s)
		n := a.root
		n.total += v
		for depth, f := range stack {
			if depth >= MaxDepth {
				break // deeper frames fold into the frame at the cap
			}
			n = n.child(f)
			n.total += v
		}
		n.self += v
		for i, f := range stack {
			st := a.fns[f.name]
			if st == nil {
				st = &fnStat{frameRef: f, seen: -1}
				a.fns[f.name] = st
			}
			if st.seen != gen {
				st.seen = gen
				st.cum += v
			}
			if i == len(stack)-1 {
				st.flat += v
			}
		}
	}
	return a
}

// appendStack appends the sample's frames root first. A location holds its
// inlined functions innermost first, and the sample its locations leaf first.
func appendStack(out []frameRef, s *profile.Sample) []frameRef {
	for i := len(s.Location) - 1; i >= 0; i-- {
		loc := s.Location[i]
		if len(loc.Line) == 0 {
			out = append(out, frameRef{name: fmt.Sprintf("0x%x", loc.Address)}) // not symbolized
			continue
		}
		for j := len(loc.Line) - 1; j >= 0; j-- {
			out = append(out, lineFrame(loc.Line[j]))
		}
	}
	return out
}

func lineFrame(l profile.Line) frameRef {
	fn := l.Function
	if fn == nil {
		return frameRef{name: "?", line: int(l.Line)}
	}
	line := int(fn.StartLine)
	if line <= 0 {
		line = int(l.Line)
	}
	name := fn.Name
	if name == "" {
		name = fn.SystemName
	}
	return frameRef{name: name, file: fn.Filename, line: line}
}

// frames converts the tree, dropping frames below frac of the total. It
// returns the tree, the number of frames kept and the number left out.
func (a *aggregation) frames(frac float64) (Frame, int, int) {
	limit := max(int64(math.Ceil(float64(a.root.total)*frac)), 1)
	kept, trimmed := 0, 0
	var convert func(n *node) Frame
	convert = func(n *node) Frame {
		kept++
		f := Frame{Name: n.name, File: n.file, Line: n.line, Self: n.self, Total: n.total}
		kids := make([]*node, 0, len(n.order))
		for _, c := range n.order {
			if c.total >= limit {
				kids = append(kids, c)
			} else {
				trimmed += c.size()
			}
		}
		// Heaviest first (left-heavy), so the hot path reads from the left edge.
		slices.SortStableFunc(kids, func(x, y *node) int { return cmp.Compare(y.total, x.total) })
		if len(kids) > 0 {
			f.Children = make([]Frame, len(kids))
			for i, c := range kids {
				f.Children[i] = convert(c)
			}
		}
		return f
	}
	root := convert(a.root)
	return root, kept, trimmed
}

// size counts n and its descendants.
func (n *node) size() int {
	s := 1
	for _, c := range n.order {
		s += c.size()
	}
	return s
}

// top returns the n heaviest functions by flat value plus the n heaviest by
// cumulative value (the table sorts either way), ordered by flat.
func (a *aggregation) top(n int) []TopEntry {
	all := make([]*fnStat, 0, len(a.fns))
	for _, st := range a.fns {
		all = append(all, st)
	}
	byName := func(x, y *fnStat) int { return cmp.Compare(x.name, y.name) }
	byFlat := func(x, y *fnStat) int {
		return cmp.Or(cmp.Compare(y.flat, x.flat), cmp.Compare(y.cum, x.cum), byName(x, y))
	}
	byCum := func(x, y *fnStat) int {
		return cmp.Or(cmp.Compare(y.cum, x.cum), cmp.Compare(y.flat, x.flat), byName(x, y))
	}

	picked := map[string]bool{}
	var out []*fnStat
	for _, order := range []func(x, y *fnStat) int{byFlat, byCum} {
		slices.SortFunc(all, order)
		for _, st := range all[:min(n, len(all))] {
			if !picked[st.name] {
				picked[st.name] = true
				out = append(out, st)
			}
		}
	}
	slices.SortFunc(out, byFlat)
	entries := make([]TopEntry, len(out))
	for i, st := range out {
		entries[i] = TopEntry{Name: st.name, File: st.file, Line: st.line, Flat: st.flat, Cum: st.cum}
	}
	return entries
}
