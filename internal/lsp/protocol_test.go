package lsp

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

func TestURIRoundTrip(t *testing.T) {
	for _, p := range []string{"/Users/me/My Project/main.go", "/tmp/zażółć/a#b.go", "/a/b?c.ts"} {
		uri := PathToURI(p)
		if !strings.HasPrefix(uri, "file:///") {
			t.Errorf("PathToURI(%q) = %q", p, uri)
		}
		if got := URIToPath(uri); got != p {
			t.Errorf("round trip %q -> %q -> %q", p, uri, got)
		}
	}
	if URIToPath("untitled:Untitled-1") != "" {
		t.Error("non-file URIs must map to empty path")
	}
}

func TestReadMessage(t *testing.T) {
	in := "Content-Length: 7\r\nContent-Type: application/vscode-jsonrpc\r\n\r\n{\"a\":1}Content-Length: 2\r\n\r\n{}"
	r := bufio.NewReader(strings.NewReader(in))
	for _, want := range []string{`{"a":1}`, `{}`} {
		got, err := readMessage(r)
		if err != nil || string(got) != want {
			t.Fatalf("readMessage = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := readMessage(bufio.NewReader(strings.NewReader("X: 1\r\n\r\n"))); err == nil {
		t.Error("missing Content-Length must fail")
	}
	if _, err := readMessage(bufio.NewReader(strings.NewReader("Content-Length: 99999999999\r\n\r\n"))); err == nil {
		t.Error("oversized Content-Length must fail")
	}
}

func TestMarkupText(t *testing.T) {
	tests := map[string]string{
		`"plain"`:                              "plain",
		`{"kind":"markdown","value":"**b**"}`:  "**b**",
		`{"language":"go","value":"func f()"}`: "```go\nfunc f()\n```",
		`["a",{"language":"go","value":"x"}]`:  "a\n\n```go\nx\n```",
		`null`:                                 "",
	}
	for in, want := range tests {
		if got := markupText(json.RawMessage(in)); got != want {
			t.Errorf("markupText(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeLocations(t *testing.T) {
	r := `{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}`
	single := `{"uri":"file:///a/b.go","range":` + r + `}`
	if got := decodeLocations(json.RawMessage(single)); len(got) != 1 || got[0].Path != "/a/b.go" || got[0].Range.Start.Character != 2 {
		t.Errorf("single = %+v", got)
	}
	links := `[{"targetUri":"file:///c.go","targetRange":` + r + `,"targetSelectionRange":` + r + `}]`
	if got := decodeLocations(json.RawMessage(links)); len(got) != 1 || got[0].Path != "/c.go" {
		t.Errorf("links = %+v", got)
	}
	if got := decodeLocations(json.RawMessage("null")); got != nil {
		t.Errorf("null = %+v", got)
	}
}

func TestDecodeCompletion(t *testing.T) {
	list := decodeCompletion(json.RawMessage(`{"isIncomplete":true,"items":[
		{"label":"Println","kind":3,"detail":"func(a ...any)","documentation":{"kind":"markdown","value":"prints"},
		 "textEdit":{"range":{"start":{"line":0,"character":4},"end":{"line":0,"character":6}},"newText":"Println"}},
		{"label":"Printf","insertText":"Printf","textEdit":{"insert":{"start":{"line":0,"character":4},"end":{"line":0,"character":6}},
		 "replace":{"start":{"line":0,"character":4},"end":{"line":0,"character":8}},"newText":"Printf"}},
		{"label":"x"}]}`))
	if !list.Incomplete || len(list.Items) != 3 {
		t.Fatalf("list = %+v", list)
	}
	a, b, c := list.Items[0], list.Items[1], list.Items[2]
	if a.Documentation != "prints" || a.Edit == nil || a.Edit.Range.End.Character != 6 || a.InsertText != "Println" {
		t.Errorf("item a = %+v", a)
	}
	if b.Edit == nil || b.Edit.Range.End.Character != 8 {
		t.Errorf("insert/replace edit should use the replace range: %+v", b.Edit)
	}
	if c.InsertText != "x" {
		t.Errorf("insertText should default to label: %+v", c)
	}
	if got := decodeCompletion(json.RawMessage(`[{"label":"y"}]`)); len(got.Items) != 1 || got.Incomplete {
		t.Errorf("array form = %+v", got)
	}
}

func TestLanguageFor(t *testing.T) {
	if l, id, ok := languageFor("/x/App.TSX"); !ok || l != "typescript" || id != "typescriptreact" {
		t.Errorf("tsx = %s %s %v", l, id, ok)
	}
	if _, _, ok := languageFor("/x/notes.txt"); ok {
		t.Error("txt must not have a server")
	}
	spec, _ := specFor("python")
	argv, ok := spec.resolveCommand(func(name string) (string, error) {
		if name == "pyright-langserver" {
			return "/bin/pyright-langserver", nil
		}
		return "", errNotFound
	})
	if !ok || argv[0] != "/bin/pyright-langserver" || argv[1] != "--stdio" {
		t.Errorf("resolveCommand = %v %v", argv, ok)
	}
}
