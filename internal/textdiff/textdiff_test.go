package textdiff

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLines(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want []Hunk
	}{
		{"identical", "a\nb\nc", "a\nb\nc", []Hunk{}},
		{"insert", "a\nc", "a\nb1\nb2\nc", []Hunk{{FromA: 1, ToA: 1, FromB: 1, ToB: 3}}},
		{"delete", "a\nb\nc", "a\nc", []Hunk{{FromA: 1, ToA: 2, FromB: 1, ToB: 1}}},
		{"modify", "a\nb\nc", "a\nB\nc", []Hunk{{FromA: 1, ToA: 2, FromB: 1, ToB: 2}}},
		{"two hunks", "a\nb\nc\nd\ne", "A\nb\nc\nd\nE", []Hunk{{0, 1, 0, 1, nil}, {4, 5, 4, 5, nil}}},
		{"crlf ignored", "a\r\nb", "a\nb", []Hunk{}},
		{"from empty", "", "x\ny", []Hunk{{FromA: 0, ToA: 1, FromB: 0, ToB: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Lines(tt.a, tt.b)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Lines(%q, %q) = %+v, want %+v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMarks(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want []LineMark
	}{
		{"identical", "a\nb\nc", "a\nb\nc", []LineMark{}},
		{"added", "a\nc", "a\nb1\nb2\nc", []LineMark{{2, Added}, {3, Added}}},
		{"modified", "a\nb\nc", "a\nB\nc", []LineMark{{2, Modified}}},
		{"deleted marks next line", "a\nb\nc", "a\nc", []LineMark{{2, Deleted}}},
		{"deleted at end marks last line", "a\nb\nc", "a\nb", []LineMark{{2, Deleted}}},
		{"modified wins over deleted", "a\nb\nc\nd", "a\nX\nd", []LineMark{{2, Modified}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Marks(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Marks = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCount(t *testing.T) {
	got := Count(Lines("a\nb\nc\nd", "a\nB\nc\nd\ne\nf"))
	if got != (Stat{Added: 3, Deleted: 1}) {
		t.Fatalf("Count = %+v", got)
	}
}

func TestCompareInline(t *testing.T) {
	hunks := Compare("x := foo(a)\nkeep", "x := bar(a)\nkeep")
	if len(hunks) != 1 {
		t.Fatalf("hunks = %+v", hunks)
	}
	want := []Range{{FromA: 5, ToA: 8, FromB: 5, ToB: 8}}
	if !reflect.DeepEqual(hunks[0].Inline, want) {
		t.Fatalf("inline = %+v, want %+v", hunks[0].Inline, want)
	}
}

func TestCompareInlineUTF16Offsets(t *testing.T) {
	// "😀" is two UTF-16 units: the changed word starts at offset 3, not 5 (bytes).
	hunks := Compare("😀 a", "😀 b")
	want := []Range{{FromA: 3, ToA: 4, FromB: 3, ToB: 4}}
	if len(hunks) != 1 || !reflect.DeepEqual(hunks[0].Inline, want) {
		t.Fatalf("hunks = %+v, want inline %+v", hunks, want)
	}
}

func TestCompareSkipsInlineWhenEverythingChanged(t *testing.T) {
	hunks := Compare("alpha", "omega")
	if len(hunks) != 1 || hunks[0].Inline != nil {
		t.Fatalf("hunks = %+v", hunks)
	}
}

// Applying the hunks to A must reproduce B, and unchanged lines must match.
func TestLinesRoundTripRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	alphabet := []string{"a", "b", "c", "d", "e", "{", "}", ""}
	gen := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = alphabet[r.IntN(len(alphabet))]
		}
		return out
	}
	for range 500 {
		la, lb := gen(r.IntN(40)+1), gen(r.IntN(40)+1)
		hunks := Lines(strings.Join(la, "\n"), strings.Join(lb, "\n"))
		var rebuilt []string
		prevA, prevB := 0, 0
		for _, h := range hunks {
			if h.FromA-prevA != h.FromB-prevB {
				t.Fatalf("unaligned unchanged run before %+v", h)
			}
			for i := prevA; i < h.FromA; i++ {
				if la[i] != lb[prevB+i-prevA] {
					t.Fatalf("line %d reported equal but differs", i)
				}
			}
			rebuilt = append(rebuilt, la[prevA:h.FromA]...)
			rebuilt = append(rebuilt, lb[h.FromB:h.ToB]...)
			prevA, prevB = h.ToA, h.ToB
		}
		rebuilt = append(rebuilt, la[prevA:]...)
		if !reflect.DeepEqual(rebuilt, lb) {
			t.Fatalf("rebuild mismatch\na=%q\nb=%q\nhunks=%+v", la, lb, hunks)
		}
	}
}

// Myers finds a shortest edit script: the classic example needs 5 edits.
func TestLinesMinimal(t *testing.T) {
	a := strings.Join(strings.Split("ABCABBA", ""), "\n")
	b := strings.Join(strings.Split("CBABAC", ""), "\n")
	edits := 0
	for _, h := range Lines(a, b) {
		edits += (h.ToA - h.FromA) + (h.ToB - h.FromB)
	}
	if edits != 5 {
		t.Fatalf("edits = %d, want 5", edits)
	}
}

func TestDeadlineFallsBackToReplace(t *testing.T) {
	a := []int{1, 2, 3, 4, 5}
	b := []int{5, 4, 3, 2, 1}
	spans := diffTokens(a, b, time.Now().Add(-time.Second))
	if len(spans) != 1 || spans[0] != (span{0, 5, 0, 5}) {
		t.Fatalf("spans = %+v", spans)
	}
}

func BenchmarkLinesLargeFile(b *testing.B) {
	var sb strings.Builder
	for i := range 20_000 {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%40))
		sb.WriteByte('\n')
	}
	base := sb.String()
	edited := strings.Replace(base, "line xxxxx\n", "changed\n", 50)
	for b.Loop() {
		Marks(base, edited)
	}
}
