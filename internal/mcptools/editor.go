package mcptools

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// Limits of an editor snapshot sent by the frontend (the WebView is not
// trusted).
const (
	maxOpenFiles = 500
	maxPathLen   = 4096
	maxSelection = 32 << 10
	maxLine      = 10_000_000
)

// EditorSnapshot is what the editor shows: the open files, the active one
// and its selection (or cursor). The frontend sends it when it changes.
type EditorSnapshot struct {
	Files []OpenFile `json:"files"`
	// Active is the file of the focused editor tab ("" when another kind of
	// tab is active).
	Active string `json:"active"`
	// Selection is the active file's main selection; an empty Text is the
	// cursor position.
	Selection *Selection `json:"selection,omitempty"`
}

// OpenFile is one editor tab.
type OpenFile struct {
	Path  string `json:"path"`
	Dirty bool   `json:"dirty"` // unsaved changes
}

// Selection is a range in the active file; lines and columns are 1-based.
type Selection struct {
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
	Text        string `json:"text"`
}

// Editor keeps the latest snapshot. It is safe for concurrent use.
type Editor struct {
	mu   sync.Mutex
	snap EditorSnapshot
}

// NewEditor returns a store with nothing open.
func NewEditor() *Editor { return &Editor{} }

// Set validates and stores a snapshot; an invalid one is refused and the
// previous snapshot stays. A long selection is cut.
func (e *Editor) Set(s EditorSnapshot) error {
	if len(s.Files) > maxOpenFiles {
		return fmt.Errorf("too many open files (max %d)", maxOpenFiles)
	}
	next := EditorSnapshot{Files: make([]OpenFile, 0, len(s.Files)), Active: s.Active}
	for _, f := range s.Files {
		if err := checkPath(f.Path); err != nil {
			return err
		}
		next.Files = append(next.Files, f)
	}
	if s.Active != "" {
		if err := checkPath(s.Active); err != nil {
			return err
		}
	}
	if sel := s.Selection; sel != nil {
		if s.Active == "" {
			return errors.New("selection without an active file")
		}
		if sel.StartLine < 1 || sel.EndLine < sel.StartLine || sel.EndLine > maxLine ||
			sel.StartColumn < 0 || sel.EndColumn < 0 || sel.StartColumn > maxLine || sel.EndColumn > maxLine {
			return fmt.Errorf("invalid selection %d:%d–%d:%d", sel.StartLine, sel.StartColumn, sel.EndLine, sel.EndColumn)
		}
		c := *sel
		c.Text = cut(c.Text, maxSelection)
		next.Selection = &c
	}
	e.mu.Lock()
	e.snap = next
	e.mu.Unlock()
	return nil
}

// checkPath accepts an absolute, clean path without control characters.
func checkPath(p string) error {
	if len(p) > maxPathLen || !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsFunc(p, unicode.IsControl) {
		return fmt.Errorf("invalid path %q", cut(p, 200))
	}
	return nil
}

// In returns the part of the snapshot in root: its open files, and the
// active file and selection when the active file is in root.
func (e *Editor) In(root string) EditorSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := EditorSnapshot{Files: []OpenFile{}}
	for _, f := range e.snap.Files {
		if within(f.Path, root) {
			out.Files = append(out.Files, f)
		}
	}
	if e.snap.Active != "" && within(e.snap.Active, root) {
		out.Active = e.snap.Active
		if e.snap.Selection != nil {
			sel := *e.snap.Selection
			out.Selection = &sel
		}
	}
	out.Files = slices.Clip(out.Files)
	return out
}
