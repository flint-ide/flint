package lsp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func r(l1, c1, l2, c2 int) Range {
	return Range{Start: Position{Line: l1, Character: c1}, End: Position{Line: l2, Character: c2}}
}

func TestApplyTextEdits(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		edits []TextEdit
		want  string
		err   string
	}{
		{"none", "a\nb\n", nil, "a\nb\n", ""},
		{"replace", "foo bar\nbaz\n", []TextEdit{{r(0, 4, 0, 7), "qux"}}, "foo qux\nbaz\n", ""},
		{"unordered", "abc\ndef\n", []TextEdit{{r(1, 0, 1, 1), "D"}, {r(0, 0, 0, 1), "A"}}, "Abc\nDef\n", ""},
		{"inserts at one position keep order", "x", []TextEdit{{r(0, 0, 0, 0), "1"}, {r(0, 0, 0, 0), "2"}}, "12x", ""},
		{"multi-line delete", "a\nb\nc\n", []TextEdit{{r(0, 1, 2, 0), ""}}, "ac\n", ""},
		{"utf-16 columns", "a😀b\n", []TextEdit{{r(0, 3, 0, 4), "B"}}, "a😀B\n", ""},
		{"two-byte runes", "żółw x\n", []TextEdit{{r(0, 5, 0, 6), "y"}}, "żółw y\n", ""},
		{"past the end of a line stops before the break", "ab\r\ncd", []TextEdit{{r(0, 99, 0, 99), "!"}}, "ab!\r\ncd", ""},
		{"past the last line appends", "ab", []TextEdit{{r(9, 0, 9, 0), "\n"}}, "ab\n", ""},
		{"whole document", "old\ntext\n", []TextEdit{{r(0, 0, 2, 0), "new\n"}}, "new\n", ""},
		{"overlap", "abcdef", []TextEdit{{r(0, 0, 0, 3), "x"}, {r(0, 2, 0, 4), "y"}}, "", "overlaps"},
		{"reversed", "abcdef", []TextEdit{{r(0, 3, 0, 1), "x"}}, "", "ends before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyTextEdits(tt.text, tt.edits)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestDecodeWorkspaceEdit(t *testing.T) {
	versions := map[string]int{"/w/open.go": 7}
	version := func(p string) (int, bool) { v, ok := versions[p]; return v, ok }

	t.Run("changes map, sorted", func(t *testing.T) {
		raw := `{"changes":{"file:///w/b.go":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":1}},"newText":"x"}],"file:///w/a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"y"}]}}`
		we, err := decodeWorkspaceEdit(json.RawMessage(raw), version)
		if err != nil || len(we.Files) != 2 || we.Files[0].Path != "/w/a.go" || we.Files[1].Path != "/w/b.go" || we.Files[0].Open {
			t.Fatalf("got %+v, %v", we, err)
		}
	})
	t.Run("documentChanges with matching version", func(t *testing.T) {
		raw := `{"documentChanges":[{"textDocument":{"uri":"file:///w/open.go","version":7},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"y"}]}]}`
		we, err := decodeWorkspaceEdit(json.RawMessage(raw), version)
		if err != nil || len(we.Files) != 1 || !we.Files[0].Open {
			t.Fatalf("got %+v, %v", we, err)
		}
	})
	t.Run("stale version refused", func(t *testing.T) {
		raw := `{"documentChanges":[{"textDocument":{"uri":"file:///w/open.go","version":6},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"y"}]}]}`
		if _, err := decodeWorkspaceEdit(json.RawMessage(raw), version); !errors.Is(err, ErrStaleEdit) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("null version on a closed file is fine", func(t *testing.T) {
		raw := `{"documentChanges":[{"textDocument":{"uri":"file:///w/closed.go","version":null},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"y"}]}]}`
		if we, err := decodeWorkspaceEdit(json.RawMessage(raw), version); err != nil || len(we.Files) != 1 {
			t.Fatalf("got %+v, %v", we, err)
		}
	})
	t.Run("resource operations refused", func(t *testing.T) {
		raw := `{"documentChanges":[{"kind":"rename","oldUri":"file:///w/a.go","newUri":"file:///w/b.go"}]}`
		if _, err := decodeWorkspaceEdit(json.RawMessage(raw), version); err == nil || !strings.Contains(err.Error(), "rename") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("non-file uri refused", func(t *testing.T) {
		raw := `{"changes":{"untitled:x":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"y"}]}}`
		if _, err := decodeWorkspaceEdit(json.RawMessage(raw), version); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("null and empty", func(t *testing.T) {
		for _, raw := range []string{"", "null", "{}"} {
			if we, err := decodeWorkspaceEdit(json.RawMessage(raw), version); err != nil || we.Files == nil || len(we.Files) != 0 {
				t.Fatalf("%q: %+v, %v", raw, we, err)
			}
		}
	})
}
