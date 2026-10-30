// Package search runs project-wide text search, streaming matches in batches.
package search

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"flint/internal/ripgrep"
)

const (
	defaultMaxResults = 5000
	maxPreviewRunes   = 400
	batchSize         = 200
	batchInterval     = 50 * time.Millisecond
)

// Options describe a single search request.
type Options struct {
	Root          string `json:"root"`
	Query         string `json:"query"`
	Regex         bool   `json:"regex"`
	CaseSensitive bool   `json:"caseSensitive"`
	WholeWord     bool   `json:"wholeWord"`
	Include       string `json:"include"` // comma-separated globs, e.g. "*.go,*.sql"
	Exclude       string `json:"exclude"`
	MaxResults    int    `json:"maxResults"`
}

// Range is a match span in UTF-16 code units, matching JS string / CodeMirror offsets.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Match is one matching line.
type Match struct {
	Root   string  `json:"root"` // the searched folder (Options.Root)
	Path   string  `json:"path"` // relative to Root, slash-separated
	Line   int     `json:"line"` // 1-based
	Text   string  `json:"text"`
	Ranges []Range `json:"ranges"`
}

// Summary is reported once the search finishes.
type Summary struct {
	Matches   int    `json:"matches"`
	Files     int    `json:"files"`
	Truncated bool   `json:"truncated"`
	Engine    string `json:"engine"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// ErrEmptyQuery is returned when there is nothing to search for.
var ErrEmptyQuery = errors.New("empty search query")

// Run executes the search and calls onBatch with groups of matches as they arrive.
func Run(ctx context.Context, opts Options, onBatch func([]Match)) (Summary, error) {
	if opts.Query == "" {
		return Summary{}, ErrEmptyQuery
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = defaultMaxResults
	}
	start := time.Now()
	b := newBatcher(func(matches []Match) {
		for i := range matches {
			matches[i].Root = opts.Root
		}
		onBatch(matches)
	})

	var (
		sum Summary
		err error
	)
	if rg := ripgrep.Path(); rg != "" {
		sum, err = runRipgrep(ctx, rg, opts, b)
		sum.Engine = "ripgrep"
	} else {
		sum, err = runFallback(ctx, opts, b)
		sum.Engine = "builtin"
	}
	b.flush()
	sum.ElapsedMs = time.Since(start).Milliseconds()
	return sum, err
}

// RunRoots searches several project folders one after another, sharing the
// result limit. Every match carries the root it was found in.
func RunRoots(ctx context.Context, roots []string, opts Options, onBatch func([]Match)) (Summary, error) {
	if opts.Query == "" {
		return Summary{}, ErrEmptyQuery
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = defaultMaxResults
	}
	start := time.Now()
	var total Summary
	for _, root := range roots {
		if ctx.Err() != nil || total.Matches >= opts.MaxResults {
			break
		}
		o := opts
		o.Root = root
		o.MaxResults = opts.MaxResults - total.Matches
		sum, err := Run(ctx, o, onBatch)
		total.Matches += sum.Matches
		total.Files += sum.Files
		total.Truncated = total.Truncated || sum.Truncated
		total.Engine = sum.Engine
		if err != nil {
			total.ElapsedMs = time.Since(start).Milliseconds()
			return total, fmt.Errorf("%s: %w", filepath.Base(root), err)
		}
	}
	total.ElapsedMs = time.Since(start).Milliseconds()
	return total, nil
}

// batcher groups matches so the UI receives a few events instead of thousands.
type batcher struct {
	emit      func([]Match)
	pending   []Match
	lastFlush time.Time
}

func newBatcher(emit func([]Match)) *batcher {
	return &batcher{emit: emit, lastFlush: time.Now()}
}

func (b *batcher) add(m Match) {
	b.pending = append(b.pending, m)
	if len(b.pending) >= batchSize || time.Since(b.lastFlush) >= batchInterval {
		b.flush()
	}
}

func (b *batcher) flush() {
	b.lastFlush = time.Now()
	if len(b.pending) == 0 {
		return
	}
	b.emit(b.pending)
	b.pending = nil
}

// newMatch builds a Match from a line and byte-offset spans, trimming very long lines
// to a window around the first match and converting offsets to UTF-16 units.
func newMatch(path string, line int, text string, byteSpans [][2]int) Match {
	offset := 0
	if utf8.RuneCountInString(text) > maxPreviewRunes && len(byteSpans) > 0 {
		offset = previewStart(text, byteSpans[0][0])
		text = truncateRunes(text[offset:], maxPreviewRunes)
	}
	ranges := make([]Range, 0, len(byteSpans))
	for _, sp := range byteSpans {
		s, e := sp[0]-offset, sp[1]-offset
		if s < 0 || e > len(text) || s > e {
			continue
		}
		ranges = append(ranges, Range{Start: utf16Len(text[:s]), End: utf16Len(text[:e])})
	}
	return Match{Path: path, Line: line, Text: text, Ranges: ranges}
}

// previewStart returns a rune-aligned byte offset ~40 runes before the match.
func previewStart(text string, matchByte int) int {
	pos := matchByte
	for i := 0; i < 40 && pos > 0; i++ {
		_, size := utf8.DecodeLastRuneInString(text[:pos])
		pos -= size
	}
	return pos
}

func truncateRunes(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
