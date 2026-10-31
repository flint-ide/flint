// Package todo lists TODO / FIXME / HACK / XXX / BUG comments across the
// workspace folders, using the project search (ripgrep, or the Go fallback).
package todo

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf16"

	"flint/internal/search"
)

// MaxItems bounds a scan; huge vendored trees are cut off, not walked forever.
const MaxItems = 5000

// Tags are the markers found, in the order the UI lists them.
var Tags = []string{"TODO", "FIXME", "HACK", "XXX", "BUG"}

// query matches a tag as a whole word in upper case, like "TODO", "TODO:" or "TODO(ad):".
const query = `\b(TODO|FIXME|HACK|XXX|BUG)\b`

// rest takes what follows a tag: an optional "(who)" and ":" or "-", then the note.
var rest = regexp.MustCompile(`^(?:\(([^)]*)\))?\s*[:\-–]?\s*(.*)$`)

// Item is one marker.
type Item struct {
	Root   string `json:"root"`
	Path   string `json:"path"` // relative to Root, slash-separated
	Line   int    `json:"line"` // 1-based
	Column int    `json:"column"`
	Tag    string `json:"tag"`
	Owner  string `json:"owner,omitempty"` // TODO(owner)
	Text   string `json:"text"`
}

// Result is a scan's outcome.
type Result struct {
	Items     []Item `json:"items"`
	Truncated bool   `json:"truncated"`
}

// Scan finds markers in every root.
func Scan(ctx context.Context, roots []string) (Result, error) {
	res := Result{Items: []Item{}}
	sum, err := search.RunRoots(ctx, roots, search.Options{Query: query, Regex: true, CaseSensitive: true, MaxResults: MaxItems}, func(ms []search.Match) {
		for _, m := range ms {
			res.Items = append(res.Items, fromMatch(m)...)
		}
	})
	res.Truncated = sum.Truncated
	return res, err
}

// fromMatch turns a matching line into its markers (usually one).
func fromMatch(m search.Match) []Item {
	units := utf16.Encode([]rune(m.Text))
	var items []Item
	for _, r := range m.Ranges {
		if r.Start < 0 || r.End > len(units) || r.Start >= r.End {
			continue
		}
		tag := string(utf16.Decode(units[r.Start:r.End]))
		after := strings.TrimSpace(string(utf16.Decode(units[r.End:])))
		it := Item{Root: m.Root, Path: m.Path, Line: m.Line, Column: r.Start + 1, Tag: tag}
		if sm := rest.FindStringSubmatch(after); sm != nil {
			it.Owner = strings.TrimSpace(sm[1])
			it.Text = cleanNote(sm[2])
		}
		items = append(items, it)
	}
	return items
}

// cleanNote drops comment closers left on the line ("*/", "-->").
func cleanNote(s string) string {
	s = strings.TrimSpace(s)
	for _, end := range []string{"*/", "-->", "#}", "%>"} {
		s = strings.TrimSpace(strings.TrimSuffix(s, end))
	}
	return s
}
