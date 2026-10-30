package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// Position is zero-based; Character counts UTF-16 code units (LSP's default
// encoding, which also matches JS strings and CodeMirror offsets).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range in a file, with the first line's text for result lists.
type Location struct {
	Path    string `json:"path"`
	Range   Range  `json:"range"`
	Preview string `json:"preview"`
}

// CompletionItem is the subset of LSP completion data the editor uses.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail"`
	Documentation string `json:"documentation"`
	InsertText    string `json:"insertText"`
	FilterText    string `json:"filterText"`
	SortText      string `json:"sortText"`
	// Edit, when set, replaces Edit.Range instead of the word before the cursor.
	Edit *TextEdit `json:"edit,omitempty"`
}

// TextEdit replaces Range with NewText.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// CompletionList is the completion result.
type CompletionList struct {
	Incomplete bool             `json:"incomplete"`
	Items      []CompletionItem `json:"items"`
}

// Diagnostic is a problem reported by the server.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1 error, 2 warning, 3 info, 4 hint
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// PathToURI converts an absolute path to a file:// URI.
func PathToURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

// URIToPath converts a file:// URI back to an absolute path ("" if not a file URI).
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return filepath.FromSlash(u.Path)
}

// wire types for decoding server responses -------------------------------------------

type wireCompletionItem struct {
	Label         string          `json:"label"`
	Kind          int             `json:"kind"`
	Detail        string          `json:"detail"`
	Documentation json.RawMessage `json:"documentation"`
	InsertText    string          `json:"insertText"`
	FilterText    string          `json:"filterText"`
	SortText      string          `json:"sortText"`
	TextEdit      json.RawMessage `json:"textEdit"`
}

func (w wireCompletionItem) toItem() CompletionItem {
	item := CompletionItem{
		Label:         w.Label,
		Kind:          w.Kind,
		Detail:        w.Detail,
		Documentation: markupText(w.Documentation),
		InsertText:    w.InsertText,
		FilterText:    w.FilterText,
		SortText:      w.SortText,
	}
	if len(w.TextEdit) > 0 && string(w.TextEdit) != "null" {
		// TextEdit {range,newText} or InsertReplaceEdit {insert,replace,newText}.
		var e struct {
			Range   *Range `json:"range"`
			Replace *Range `json:"replace"`
			NewText string `json:"newText"`
		}
		if json.Unmarshal(w.TextEdit, &e) == nil {
			r := e.Range
			if r == nil {
				r = e.Replace
			}
			if r != nil {
				item.Edit = &TextEdit{Range: *r, NewText: e.NewText}
			}
		}
	}
	if item.InsertText == "" {
		item.InsertText = item.Label
	}
	return item
}

// markupText flattens string | MarkupContent | MarkedString | MarkedString[].
func markupText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var mc struct {
		Kind     string `json:"kind"`
		Language string `json:"language"`
		Value    string `json:"value"`
	}
	if json.Unmarshal(raw, &mc) == nil && mc.Value != "" {
		if mc.Language != "" {
			return "```" + mc.Language + "\n" + mc.Value + "\n```"
		}
		return mc.Value
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			if t := markupText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// decodeLocations accepts Location | Location[] | LocationLink[].
func decodeLocations(raw json.RawMessage) []Location {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	type wire struct {
		URI                  string `json:"uri"`
		Range                *Range `json:"range"`
		TargetURI            string `json:"targetUri"`
		TargetSelectionRange *Range `json:"targetSelectionRange"`
	}
	var list []wire
	if raw[0] == '{' {
		var one wire
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		list = []wire{one}
	} else if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	out := make([]Location, 0, len(list))
	for _, w := range list {
		uri, r := w.URI, w.Range
		if w.TargetURI != "" {
			uri, r = w.TargetURI, w.TargetSelectionRange
		}
		if path := URIToPath(uri); path != "" && r != nil {
			out = append(out, Location{Path: path, Range: *r})
		}
	}
	return out
}
