package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Bounds on what one answer may change, so a broken server can't make Flint
// rewrite a whole tree or hold gigabytes of edits.
const (
	maxEditFiles = 2000
	maxEdits     = 200000
)

// FileEdit is the edits of one file; the positions of all its Edits refer to
// the file before any of them is applied.
type FileEdit struct {
	Path  string     `json:"path"`
	Edits []TextEdit `json:"edits"`
	// Open says the file is open in an editor, so the edits belong in its
	// buffer (undoable) instead of on disk.
	Open bool `json:"open"`
}

// WorkspaceEdit is a change across files (rename, a code action). Files are
// applied in order; a path may appear more than once when a server sent
// several edits one after another.
type WorkspaceEdit struct {
	Files []FileEdit `json:"files"`
}

// ErrStaleEdit is returned when a server's edit was computed for an older
// version of an open file.
var ErrStaleEdit = errors.New("the file changed while the language server was working, so its edit no longer fits. Try again")

type wireTextDocumentEdit struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version *int   `json:"version"`
	} `json:"textDocument"`
	Edits []TextEdit `json:"edits"`
	// Kind is set on resource operations (create, rename, delete), which
	// Flint doesn't announce support for and refuses.
	Kind string `json:"kind"`
}

type wireWorkspaceEdit struct {
	Changes         map[string][]TextEdit  `json:"changes"`
	DocumentChanges []wireTextDocumentEdit `json:"documentChanges"`
}

// decodeWorkspaceEdit turns a server's WorkspaceEdit into file edits.
// version reports the current version of an open document (ok false when the
// file isn't open); an edit made for another version is refused.
func decodeWorkspaceEdit(raw json.RawMessage, version func(path string) (int, bool)) (WorkspaceEdit, error) {
	out := WorkspaceEdit{Files: []FileEdit{}}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var w wireWorkspaceEdit
	if err := json.Unmarshal(raw, &w); err != nil {
		return out, fmt.Errorf("the language server sent an edit Flint can't read (%w)", err)
	}
	total := 0
	add := func(uri string, edits []TextEdit, ver *int) error {
		path := URIToPath(uri)
		if path == "" {
			return fmt.Errorf("the language server wants to change %q, which is not a file on disk", uri)
		}
		if len(edits) == 0 {
			return nil
		}
		cur, open := version(path)
		if open && ver != nil && *ver != cur {
			return ErrStaleEdit
		}
		total += len(edits)
		if len(out.Files) >= maxEditFiles || total > maxEdits {
			return fmt.Errorf("the language server's change is too large to apply safely (more than %d files or %d edits)", maxEditFiles, maxEdits)
		}
		out.Files = append(out.Files, FileEdit{Path: path, Edits: edits, Open: open})
		return nil
	}
	if len(w.DocumentChanges) > 0 {
		for _, dc := range w.DocumentChanges {
			if dc.Kind != "" {
				return out, fmt.Errorf("the language server's change creates, renames or deletes files (%s), which Flint doesn't apply. Do that step by hand, then try again", dc.Kind)
			}
			if err := add(dc.TextDocument.URI, dc.Edits, dc.TextDocument.Version); err != nil {
				return out, err
			}
		}
		return out, nil
	}
	uris := make([]string, 0, len(w.Changes))
	for uri := range w.Changes {
		uris = append(uris, uri)
	}
	sort.Strings(uris) // a stable order for the UI and tests
	for _, uri := range uris {
		if err := add(uri, w.Changes[uri], nil); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ApplyTextEdits applies edits to text. Positions are LSP positions (UTF-16
// columns) in the original text; edits must not overlap. Inserts at the same
// position keep their order, as the protocol requires.
func ApplyTextEdits(text string, edits []TextEdit) (string, error) {
	if len(edits) == 0 {
		return text, nil
	}
	starts := lineStarts(text)
	type span struct {
		from, to int
		text     string
	}
	spans := make([]span, len(edits))
	for i, e := range edits {
		from, to := offsetAt(text, starts, e.Range.Start), offsetAt(text, starts, e.Range.End)
		if to < from {
			return "", fmt.Errorf("edit %d ends before it starts", i)
		}
		spans[i] = span{from, to, e.NewText}
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	var b strings.Builder
	pos := 0
	for i, s := range spans {
		if s.from < pos {
			return "", fmt.Errorf("edit %d overlaps the one before it", i)
		}
		b.WriteString(text[pos:s.from])
		b.WriteString(s.text)
		pos = s.to
	}
	b.WriteString(text[pos:])
	return b.String(), nil
}

// lineStarts returns the byte offset of every line of text.
func lineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// offsetAt converts an LSP position to a byte offset, clamped to the text
// and to the end of its line (before the line break).
func offsetAt(text string, starts []int, p Position) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(starts) {
		return len(text)
	}
	i := starts[p.Line]
	end := len(text)
	if p.Line+1 < len(starts) {
		end = starts[p.Line+1] - 1
		if end > i && text[end-1] == '\r' {
			end--
		}
	}
	units := 0
	for i < end && units < p.Character {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
		i += size
	}
	return i
}
