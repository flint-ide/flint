package search

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"flint/internal/config"
	"flint/internal/workspace"
)

// Replacing: the preview and the write both go through Replacer (Go's regexp),
// so what the user ticked is exactly what gets written, whichever engine found
// the matches.

// maxUndoBytes bounds what one replace keeps for undo (old and new contents).
const maxUndoBytes = 64 << 20

// Replacer replaces a query's matches in a line.
type Replacer struct {
	m           *matcher
	replacement string
	literal     bool // plain-text query: "$1" in the replacement is just text
}

// NewReplacer compiles opts the same way the built-in search does.
func NewReplacer(opts Options, replacement string) (*Replacer, error) {
	if opts.Query == "" {
		return nil, ErrEmptyQuery
	}
	m, err := compilePattern(opts)
	if err != nil {
		return nil, err
	}
	return &Replacer{m: m, replacement: replacement, literal: !opts.Regex}, nil
}

// spans returns the byte spans of the matches in text with what each becomes.
func (r *Replacer) spans(text string) ([][2]int, []string) {
	var spans [][2]int
	var with []string
	for _, loc := range r.m.re.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] == loc[1] {
			continue
		}
		if r.m.wholeWord && !(wordBoundaryBefore(text, loc[0]) && wordBoundaryAfter(text, loc[1])) {
			continue
		}
		spans = append(spans, [2]int{loc[0], loc[1]})
		if r.literal {
			with = append(with, r.replacement)
		} else {
			with = append(with, string(r.m.re.ExpandString(nil, r.replacement, text, loc)))
		}
	}
	return spans, with
}

// Line returns text with every match replaced and how many there were.
func (r *Replacer) Line(text string) (string, int) {
	spans, with := r.spans(text)
	if len(spans) == 0 {
		return text, 0
	}
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for i, sp := range spans {
		b.WriteString(text[last:sp[0]])
		b.WriteString(with[i])
		last = sp[1]
	}
	b.WriteString(text[last:])
	return b.String(), len(spans)
}

// annotate sets m's ranges to this replacer's matches in m.Text (the preview
// line) and the text each one becomes.
func (r *Replacer) annotate(m *Match) {
	spans, with := r.spans(m.Text)
	m.Ranges = make([]Range, len(spans))
	for i, sp := range spans {
		m.Ranges[i] = Range{Start: utf16Len(m.Text[:sp[0]]), End: utf16Len(m.Text[:sp[1]])}
	}
	m.Replacements = with
}

// ReplaceTarget selects the lines to change in one file.
type ReplaceTarget struct {
	Root  string `json:"root"`
	Path  string `json:"path"`  // relative to Root, slash-separated (as in Match)
	Lines []int  `json:"lines"` // 1-based
}

// SkippedFile is a target that was left alone, and why.
type SkippedFile struct {
	Root   string `json:"root"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ReplaceResult sums up an Apply.
type ReplaceResult struct {
	Files        int           `json:"files"`
	Replacements int           `json:"replacements"`
	Skipped      []SkippedFile `json:"skipped"`
	// Undoable: the previous contents were kept (they fit in maxUndoBytes).
	Undoable bool `json:"undoable"`
}

// Undo holds the contents an Apply replaced, to put them back.
type Undo struct {
	files []undoFile
}

type undoFile struct {
	path          string
	before, after []byte
}

// errChanged marks a file whose selected lines no longer match.
var errChanged = errors.New("changed since the search")

// Apply replaces the matches on the selected lines of every target. Each file is
// re-read; one whose selected lines no longer all match (edited since the
// preview), binary or too big is skipped. Writes are atomic and keep the mode.
func (r *Replacer) Apply(targets []ReplaceTarget) (ReplaceResult, *Undo) {
	res := ReplaceResult{Skipped: []SkippedFile{}}
	undo := &Undo{}
	kept := 0
	for _, t := range targets {
		path := filepath.Join(t.Root, filepath.FromSlash(t.Path))
		if !filepath.IsAbs(t.Root) || !strings.HasPrefix(path, filepath.Clean(t.Root)+string(filepath.Separator)) {
			res.Skipped = append(res.Skipped, SkippedFile{t.Root, t.Path, "outside the project"})
			continue
		}
		before, after, n, err := r.applyFile(path, t.Lines)
		if err != nil {
			res.Skipped = append(res.Skipped, SkippedFile{t.Root, t.Path, err.Error()})
			continue
		}
		res.Files++
		res.Replacements += n
		if kept += len(before) + len(after); kept <= maxUndoBytes {
			undo.files = append(undo.files, undoFile{path: path, before: before, after: after})
		}
	}
	res.Undoable = kept <= maxUndoBytes && len(undo.files) > 0
	if !res.Undoable {
		undo = nil
	}
	return res, undo
}

func (r *Replacer) applyFile(path string, lines []int) (before, after []byte, n int, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("can't read: %w", err)
	}
	if info.Size() > workspace.MaxFileSize {
		return nil, nil, 0, errors.New("too big")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("can't read: %w", err)
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil, nil, 0, errors.New("binary file")
	}
	want := map[int]bool{}
	for _, l := range lines {
		want[l] = true
	}
	parts := bytes.SplitAfter(data, []byte("\n"))
	var out bytes.Buffer
	out.Grow(len(data))
	for i, part := range parts {
		if !want[i+1] {
			out.Write(part)
			continue
		}
		body, ending := splitEnding(part)
		replaced, count := r.Line(string(body))
		if count == 0 {
			return nil, nil, 0, errChanged
		}
		n += count
		out.WriteString(replaced)
		out.Write(ending)
		delete(want, i+1)
	}
	if len(want) > 0 {
		return nil, nil, 0, errChanged // the file got shorter
	}
	if err := config.WriteFileAtomic(path, out.Bytes(), info.Mode().Perm()); err != nil {
		return nil, nil, 0, err
	}
	return data, out.Bytes(), n, nil
}

// splitEnding separates a line from its "\n" or "\r\n".
func splitEnding(line []byte) (body, ending []byte) {
	switch {
	case bytes.HasSuffix(line, []byte("\r\n")):
		return line[:len(line)-2], line[len(line)-2:]
	case bytes.HasSuffix(line, []byte("\n")):
		return line[:len(line)-1], line[len(line)-1:]
	}
	return line, nil
}

// Restore puts back the contents an Apply replaced, except in files changed
// since (they are listed as skipped).
func (u *Undo) Restore() (restored int, skipped []string) {
	for _, f := range u.files {
		cur, err := os.ReadFile(f.path)
		if err != nil || !bytes.Equal(cur, f.after) {
			skipped = append(skipped, f.path)
			continue
		}
		mode := os.FileMode(0o644)
		if info, err := os.Stat(f.path); err == nil {
			mode = info.Mode().Perm()
		}
		if err := config.WriteFileAtomic(f.path, f.before, mode); err != nil {
			skipped = append(skipped, f.path)
			continue
		}
		restored++
	}
	sort.Strings(skipped)
	return restored, skipped
}

// UndoSlot keeps the last replace's undo, guarded for concurrent API calls.
type UndoSlot struct {
	mu   sync.Mutex
	last *Undo
}

func (s *UndoSlot) Set(u *Undo) {
	s.mu.Lock()
	s.last = u
	s.mu.Unlock()
}

// Take returns the last undo once (nil when there is none).
func (s *UndoSlot) Take() *Undo {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.last
	s.last = nil
	return u
}
