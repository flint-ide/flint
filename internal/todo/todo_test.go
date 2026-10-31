package todo

import (
	"os"
	"path/filepath"
	"testing"

	"flint/internal/search"
)

func TestFromMatchParsesTagOwnerAndNote(t *testing.T) {
	cases := []struct {
		text       string
		start, end int
		want       Item
	}{
		{"// TODO: wire the cache", 3, 7, Item{Tag: "TODO", Text: "wire the cache", Column: 4}},
		{"/* FIXME(ala) - leaks on close */", 3, 8, Item{Tag: "FIXME", Owner: "ala", Text: "leaks on close", Column: 4}},
		{"# HACK", 2, 6, Item{Tag: "HACK", Column: 3}},
		{"<!-- XXX: zażółć -->", 5, 8, Item{Tag: "XXX", Text: "zażółć", Column: 6}},
	}
	for _, c := range cases {
		got := fromMatch(search.Match{Root: "/p", Path: "a.go", Line: 3, Text: c.text, Ranges: []search.Range{{Start: c.start, End: c.end}}})
		c.want.Root, c.want.Path, c.want.Line = "/p", "a.go", 3
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%q: got %+v, want %+v", c.text, got, c.want)
		}
	}
}

func TestScanFindsMarkersAsWholeUpperCaseWords(t *testing.T) {
	root := t.TempDir()
	src := "package a\n\n// TODO: first\nvar todo = 1 // not a marker: lower case\n// TODOS are plural, not a tag\nfunc f() {} // BUG(bob): off by one\n"
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Scan(t.Context(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items %+v", res.Items)
	}
	if res.Items[0].Tag != "TODO" || res.Items[0].Line != 3 || res.Items[1].Tag != "BUG" || res.Items[1].Owner != "bob" {
		t.Fatalf("items %+v", res.Items)
	}
}
