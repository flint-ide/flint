package agenthost

import "sort"

// item is one numbered entry of a ring. Data is never modified after adding,
// so replies may share it.
type item struct {
	seq  uint64
	data []byte
}

// ring keeps the newest items under two bounds, bytes and count (0: no count
// bound), so a long-running terminal or chat costs a fixed amount of memory.
// The newest item is always kept, even when it alone exceeds the byte bound.
// Callers lock around it.
type ring struct {
	items    []item
	bytes    int
	maxBytes int
	maxItems int
}

func newRing(maxBytes, maxItems int) *ring { return &ring{maxBytes: maxBytes, maxItems: maxItems} }

// last is the newest seq, 0 for an empty ring.
func (r *ring) last() uint64 {
	if len(r.items) == 0 {
		return 0
	}
	return r.items[len(r.items)-1].seq
}

// add appends an item and evicts the oldest ones over the bounds. Seqs must
// grow; an item that does not is ignored (it would break lookups).
func (r *ring) add(seq uint64, data []byte) {
	if seq <= r.last() {
		return
	}
	r.items = append(r.items, item{seq: seq, data: data})
	r.bytes += len(data)
	drop := 0
	for len(r.items)-drop > 1 && (r.bytes > r.maxBytes || (r.maxItems > 0 && len(r.items)-drop > r.maxItems)) {
		r.bytes -= len(r.items[drop].data)
		r.items[drop] = item{} // let the data go
		drop++
	}
	if drop > 0 {
		// Copy down instead of reslicing: a resliced head would keep the
		// evicted prefix of the backing array alive for good.
		n := copy(r.items, r.items[drop:])
		clear(r.items[n:])
		r.items = r.items[:n]
	}
}

// after returns the items newer than afterSeq, at most budget bytes of them
// (but at least one). reset is true when items between afterSeq and the oldest
// one were evicted, or afterSeq is ahead of the ring (a host that restarted
// or an id that was reused): the items are then the whole ring. more is true
// when the budget cut the list; ask again from the last item's seq.
func (r *ring) after(afterSeq uint64, budget int) (items []item, reset, more bool) {
	if len(r.items) == 0 {
		return nil, false, false
	}
	start := 0
	switch {
	case afterSeq > r.last() || afterSeq+1 < r.items[0].seq:
		reset = true
	default:
		start = sort.Search(len(r.items), func(i int) bool { return r.items[i].seq > afterSeq })
	}
	size := 0
	end := start
	for end < len(r.items) {
		n := len(r.items[end].data)
		if end > start && size+n > budget {
			more = true
			break
		}
		size += n
		end++
	}
	return append([]item(nil), r.items[start:end]...), reset, more
}
