package search

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// rangeText slices s by a UTF-16 Range, the way the JS frontend would.
func rangeText(s string, r Range) string {
	u := utf16.Encode([]rune(s))
	if r.Start < 0 || r.End > len(u) || r.Start > r.End {
		return "<out of bounds>"
	}
	return string(utf16.Decode(u[r.Start:r.End]))
}

// span returns the byte span of the first occurrence of sub in s.
func span(t *testing.T, s, sub string) [2]int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("%q not in line", sub)
	}
	return [2]int{i, i + len(sub)}
}

func TestNewMatchUTF16Offsets(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		subs  []string
		wants []Range
	}{
		{"ascii", "hello world", []string{"world"}, []Range{{6, 11}}},
		{"polish diacritics", "zażółć gęślą jaźń", []string{"gęślą"}, []Range{{7, 12}}},
		// ą(2B) ę(2B) 😀(4B, surrogate pair = 2 units) → x at byte 8, UTF-16 unit 4.
		{"emoji surrogate pair", "ąę😀x", []string{"x"}, []Range{{4, 5}}},
		{"match is emoji", "a😀b", []string{"😀"}, []Range{{1, 3}}},
		{"multiple spans", "żółw i żółw 🐢 żółw", []string{"i", "🐢"}, []Range{{5, 6}, {12, 14}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := make([][2]int, len(tt.subs))
			for i, s := range tt.subs {
				spans[i] = span(t, tt.text, s)
			}
			m := newMatch("p.go", 7, tt.text, spans)
			if m.Path != "p.go" || m.Line != 7 || m.Text != tt.text {
				t.Errorf("metadata = %+v", m)
			}
			if len(m.Ranges) != len(tt.wants) {
				t.Fatalf("ranges = %v, want %v", m.Ranges, tt.wants)
			}
			for i, r := range m.Ranges {
				if r != tt.wants[i] {
					t.Errorf("range[%d] = %v, want %v", i, r, tt.wants[i])
				}
				if got := rangeText(m.Text, r); got != tt.subs[i] {
					t.Errorf("range[%d] selects %q, want %q", i, got, tt.subs[i])
				}
			}
		})
	}
}

func TestNewMatchInvalidSpansDropped(t *testing.T) {
	m := newMatch("p", 1, "short", [][2]int{{0, 2}, {3, 99}, {4, 2}, {-1, 1}})
	if len(m.Ranges) != 1 || m.Ranges[0] != (Range{0, 2}) {
		t.Errorf("ranges = %v, want [{0 2}]", m.Ranges)
	}
}

func TestNewMatchLongLineWindow(t *testing.T) {
	prefix := strings.Repeat("ł", 1000) + "😀"
	line := prefix + "NEEDLE" + strings.Repeat("ż", 1000) + "TAIL"
	needle := span(t, line, "NEEDLE")
	tail := span(t, line, "TAIL")

	m := newMatch("p", 1, line, [][2]int{needle, tail})

	if n := len([]rune(m.Text)); n != maxPreviewRunes {
		t.Errorf("preview has %d runes, want %d", n, maxPreviewRunes)
	}
	if !strings.HasPrefix(m.Text, strings.Repeat("ł", 39)+"😀NEEDLE") {
		t.Errorf("preview should start 40 runes before match, got %q...", m.Text[:60])
	}
	// TAIL lies outside the window, so only NEEDLE's range survives.
	if len(m.Ranges) != 1 {
		t.Fatalf("ranges = %v, want 1", m.Ranges)
	}
	// 39 × ł (1 unit) + 😀 (2 units) = 41 units before NEEDLE.
	if want := (Range{41, 47}); m.Ranges[0] != want {
		t.Errorf("range = %v, want %v", m.Ranges[0], want)
	}
	if got := rangeText(m.Text, m.Ranges[0]); got != "NEEDLE" {
		t.Errorf("range selects %q, want NEEDLE", got)
	}
}

func TestNewMatchLongLineMatchNearStart(t *testing.T) {
	line := "ab NEEDLE " + strings.Repeat("ś", 1000)
	m := newMatch("p", 1, line, [][2]int{span(t, line, "NEEDLE")})
	if !strings.HasPrefix(m.Text, "ab NEEDLE") {
		t.Errorf("preview should start at line start, got %q", m.Text[:12])
	}
	if got := rangeText(m.Text, m.Ranges[0]); got != "NEEDLE" {
		t.Errorf("range selects %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"zażółć", 3, "zaż"},
		{"abc", 5, "abc"},
		{"😀😀", 1, "😀"},
		{"", 2, ""},
	}
	for _, tt := range tests {
		if got := truncateRunes(tt.in, tt.n); got != tt.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestBatcher(t *testing.T) {
	var batches [][]Match
	b := newBatcher(func(ms []Match) { batches = append(batches, ms) })
	b.lastFlush = time.Now().Add(time.Hour) // keep the interval trigger out of the way

	for i := 0; i < batchSize-1; i++ {
		b.add(Match{Line: i})
	}
	if len(batches) != 0 {
		t.Fatalf("flushed early: %d batches", len(batches))
	}
	b.add(Match{Line: batchSize - 1})
	if len(batches) != 1 || len(batches[0]) != batchSize {
		t.Fatalf("after size threshold: %d batches", len(batches))
	}

	b.flush() // nothing pending → no empty batch
	if len(batches) != 1 {
		t.Errorf("empty flush emitted a batch")
	}

	b.lastFlush = time.Now().Add(-time.Second) // interval elapsed → flush on next add
	b.add(Match{Line: 1000})
	if len(batches) != 2 || len(batches[1]) != 1 {
		t.Fatalf("interval flush: batches = %d", len(batches))
	}

	b.lastFlush = time.Now().Add(time.Hour)
	b.add(Match{Line: 1})
	b.add(Match{Line: 2})
	b.flush()
	if len(batches) != 3 || len(batches[2]) != 2 {
		t.Errorf("final flush: batches = %d", len(batches))
	}
	if b.pending != nil {
		t.Errorf("pending not reset")
	}
}
