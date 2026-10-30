package textdiff

import "time"

// span is one changed region: a[A0:A1] was replaced by b[B0:B1] (either may be empty).
type span struct{ A0, A1, B0, B1 int }

// diffTokens returns the changed regions between two token sequences, using
// Myers' linear-space algorithm (middle snake bisection). When the deadline
// passes, the remaining region is reported as one replacement: a coarser but
// still correct diff instead of an unbounded wait on huge, unrelated inputs.
func diffTokens(a, b []int, deadline time.Time) []span {
	d := differ{a: a, b: b, deadline: deadline}
	d.compare(0, len(a), 0, len(b))
	return merge(d.out)
}

type differ struct {
	a, b     []int
	deadline time.Time
	out      []span
}

func (d *differ) compare(a0, a1, b0, b1 int) {
	for a0 < a1 && b0 < b1 && d.a[a0] == d.b[b0] {
		a0++
		b0++
	}
	for a1 > a0 && b1 > b0 && d.a[a1-1] == d.b[b1-1] {
		a1--
		b1--
	}
	switch {
	case a0 == a1 && b0 == b1:
		return
	case a0 == a1 || b0 == b1:
		d.out = append(d.out, span{a0, a1, b0, b1})
		return
	}
	x, y, ok := d.bisect(a0, a1, b0, b1)
	if !ok {
		d.out = append(d.out, span{a0, a1, b0, b1})
		return
	}
	d.compare(a0, x, b0, y)
	d.compare(x, a1, y, b1)
}

// bisect finds the middle snake of a[a0:a1] vs b[b0:b1] and returns the point
// splitting both into independent halves; ok is false on timeout or no split.
func (d *differ) bisect(a0, a1, b0, b1 int) (x, y int, ok bool) {
	n, m := a1-a0, b1-b0
	maxD := (n + m + 1) / 2
	offset := maxD
	size := 2*maxD + 2
	vf := make([]int, size)
	vb := make([]int, size)
	for i := range vf {
		vf[i], vb[i] = -1, -1
	}
	vf[offset+1], vb[offset+1] = 0, 0
	delta := n - m
	front := delta%2 != 0 // the forward path detects overlap when delta is odd
	// k-ranges that have run off the grid, so later rounds skip them.
	k1start, k1end, k2start, k2end := 0, 0, 0, 0
	for step := range maxD {
		if step%64 == 0 && !d.deadline.IsZero() && time.Now().After(d.deadline) {
			return 0, 0, false
		}
		for k1 := -step + k1start; k1 <= step-k1end; k1 += 2 {
			i := offset + k1
			var px int
			if k1 == -step || (k1 != step && vf[i-1] < vf[i+1]) {
				px = vf[i+1]
			} else {
				px = vf[i-1] + 1
			}
			py := px - k1
			for px < n && py < m && d.a[a0+px] == d.b[b0+py] {
				px++
				py++
			}
			vf[i] = px
			switch {
			case px > n:
				k1end += 2
			case py > m:
				k1start += 2
			case front:
				j := offset + delta - k1
				if j >= 0 && j < size && vb[j] != -1 && px >= n-vb[j] {
					return a0 + px, b0 + py, true
				}
			}
		}
		for k2 := -step + k2start; k2 <= step-k2end; k2 += 2 {
			i := offset + k2
			var qx int
			if k2 == -step || (k2 != step && vb[i-1] < vb[i+1]) {
				qx = vb[i+1]
			} else {
				qx = vb[i-1] + 1
			}
			qy := qx - k2
			for qx < n && qy < m && d.a[a1-qx-1] == d.b[b1-qy-1] {
				qx++
				qy++
			}
			vb[i] = qx
			switch {
			case qx > n:
				k2end += 2
			case qy > m:
				k2start += 2
			case !front:
				j := offset + delta - k2
				if j >= 0 && j < size && vf[j] != -1 {
					px := vf[j]
					py := px - (delta - k2)
					if px >= n-qx {
						return a0 + px, b0 + py, true
					}
				}
			}
		}
	}
	return 0, 0, false
}

// merge joins adjacent spans (the recursion can split one change in two).
func merge(spans []span) []span {
	out := spans[:0]
	for _, s := range spans {
		if n := len(out); n > 0 && out[n-1].A1 == s.A0 && out[n-1].B1 == s.B0 {
			out[n-1].A1, out[n-1].B1 = s.A1, s.B1
			continue
		}
		out = append(out, s)
	}
	return out
}
