package agenthost

import (
	"bytes"
	"testing"
)

func seqs(items []item) []uint64 {
	var out []uint64
	for _, it := range items {
		out = append(out, it.seq)
	}
	return out
}

func equalSeqs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRingEvictsOldestByBytes(t *testing.T) {
	r := newRing(10, 0)
	for i := uint64(1); i <= 5; i++ {
		r.add(i, bytes.Repeat([]byte{'x'}, 4))
	}
	if r.bytes > 10 {
		t.Errorf("bytes = %d, want at most 10", r.bytes)
	}
	if got := seqs(r.items); !equalSeqs(got, []uint64{4, 5}) {
		t.Errorf("kept %v, want [4 5]", got)
	}
}

func TestRingEvictsOldestByCount(t *testing.T) {
	r := newRing(1<<20, 3)
	for i := uint64(1); i <= 10; i++ {
		r.add(i, []byte("e"))
	}
	if got := seqs(r.items); !equalSeqs(got, []uint64{8, 9, 10}) {
		t.Errorf("kept %v, want [8 9 10]", got)
	}
}

func TestRingKeepsNewestEvenWhenItAloneIsTooBig(t *testing.T) {
	r := newRing(4, 0)
	r.add(1, []byte("ab"))
	r.add(2, []byte("0123456789"))
	if got := seqs(r.items); !equalSeqs(got, []uint64{2}) {
		t.Errorf("kept %v, want [2]", got)
	}
}

func TestRingIgnoresSeqThatDoesNotGrow(t *testing.T) {
	r := newRing(100, 0)
	r.add(5, []byte("a"))
	r.add(5, []byte("b"))
	r.add(3, []byte("c"))
	if got := seqs(r.items); !equalSeqs(got, []uint64{5}) {
		t.Errorf("kept %v, want [5]", got)
	}
}

func TestRingAfter(t *testing.T) {
	r := newRing(1<<20, 5)
	for i := uint64(1); i <= 8; i++ { // keeps 4..8
		r.add(i, []byte("0123456789"))
	}
	tests := []struct {
		name      string
		after     uint64
		budget    int
		want      []uint64
		wantReset bool
		wantMore  bool
	}{
		{"caught up", 8, 1 << 20, nil, false, false},
		{"just behind", 7, 1 << 20, []uint64{8}, false, false},
		{"oldest still buffered", 3, 1 << 20, []uint64{4, 5, 6, 7, 8}, false, false},
		{"evicted", 2, 1 << 20, []uint64{4, 5, 6, 7, 8}, true, false},
		{"never seen anything", 0, 1 << 20, []uint64{4, 5, 6, 7, 8}, true, false},
		{"ahead of the host", 99, 1 << 20, []uint64{4, 5, 6, 7, 8}, true, false},
		{"budget cuts", 4, 25, []uint64{5, 6}, false, true},
		{"budget keeps at least one", 4, 1, []uint64{5}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reset, more := r.after(tt.after, tt.budget)
			if !equalSeqs(seqs(got), tt.want) || reset != tt.wantReset || more != tt.wantMore {
				t.Errorf("after(%d) = %v reset=%v more=%v, want %v reset=%v more=%v",
					tt.after, seqs(got), reset, more, tt.want, tt.wantReset, tt.wantMore)
			}
		})
	}
}

func TestRingAfterOnEmptyRing(t *testing.T) {
	items, reset, more := newRing(10, 0).after(0, 10)
	if len(items) != 0 || reset || more {
		t.Errorf("empty ring gave %v %v %v", items, reset, more)
	}
}
