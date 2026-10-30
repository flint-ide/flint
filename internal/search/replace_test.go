package search

import (
	"os"
	"path/filepath"
	"testing"
)

func mustReplacer(t *testing.T, opts Options, repl string) *Replacer {
	t.Helper()
	r, err := NewReplacer(opts, repl)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReplaceLine(t *testing.T) {
	cases := []struct {
		name      string
		opts      Options
		repl, in  string
		want      string
		wantCount int
	}{
		{"literal keeps $ as text", Options{Query: "a.b"}, "$1", "a.b axb", "$1 axb", 1},
		{"case insensitive", Options{Query: "foo"}, "bar", "Foo foo FOO", "bar bar bar", 3},
		{"case sensitive", Options{Query: "foo", CaseSensitive: true}, "bar", "Foo foo", "Foo bar", 1},
		{"regex groups", Options{Query: `(\w+)@(\w+)`, Regex: true}, "$2 at ${1}", "ala@kot", "kot at ala", 1},
		{"named group", Options{Query: `(?P<n>\d+)`, Regex: true}, "<${n}>", "a1 b22", "a<1> b<22>", 2},
		{"whole word, unicode", Options{Query: "żółw", WholeWord: true}, "X", "żółw żółwik", "X żółwik", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := mustReplacer(t, c.opts, c.repl).Line(c.in)
			if got != c.want || n != c.wantCount {
				t.Fatalf("Line(%q) = %q, %d; want %q, %d", c.in, got, n, c.want, c.wantCount)
			}
		})
	}
}

func TestAnnotateGivesRangesAndReplacements(t *testing.T) {
	m := Match{Text: "zażółć x zażółć"}
	mustReplacer(t, Options{Query: "x"}, "yy").annotate(&m)
	if len(m.Ranges) != 1 || m.Ranges[0] != (Range{Start: 7, End: 8}) || m.Replacements[0] != "yy" {
		t.Fatalf("ranges %v replacements %v", m.Ranges, m.Replacements)
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyOnlySelectedLinesKeepingEndingsAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	writeFile(t, path, "old one\r\nold two\r\nold three", 0o640)
	r := mustReplacer(t, Options{Query: "old"}, "new")

	res, undo := r.Apply([]ReplaceTarget{{Root: root, Path: "a.txt", Lines: []int{1, 3}}})
	if res.Files != 1 || res.Replacements != 2 || len(res.Skipped) != 0 || !res.Undoable {
		t.Fatalf("result %+v", res)
	}
	if got := readFile(t, path); got != "new one\r\nold two\r\nnew three" {
		t.Fatalf("content %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if n, skipped := undo.Restore(); n != 1 || len(skipped) != 0 {
		t.Fatalf("restore %d %v", n, skipped)
	}
	if got := readFile(t, path); got != "old one\r\nold two\r\nold three" {
		t.Fatalf("after undo %q", got)
	}
}

func TestApplySkipsChangedBinaryAndOutsideFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "changed.txt"), "nothing here\n", 0o644)
	writeFile(t, filepath.Join(root, "bin.dat"), "old\x00old", 0o644)
	writeFile(t, filepath.Join(root, "short.txt"), "old\n", 0o644)
	r := mustReplacer(t, Options{Query: "old"}, "new")

	res, undo := r.Apply([]ReplaceTarget{
		{Root: root, Path: "changed.txt", Lines: []int{1}},
		{Root: root, Path: "bin.dat", Lines: []int{1}},
		{Root: root, Path: "short.txt", Lines: []int{1, 5}},
		{Root: root, Path: "../escape.txt", Lines: []int{1}},
	})
	if res.Files != 0 || len(res.Skipped) != 4 || undo != nil {
		t.Fatalf("result %+v undo %v", res, undo)
	}
	if got := readFile(t, filepath.Join(root, "short.txt")); got != "old\n" {
		t.Fatalf("partially applied: %q", got)
	}
}

func TestUndoSkipsFilesEditedAfterReplace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	writeFile(t, path, "old\n", 0o644)
	_, undo := mustReplacer(t, Options{Query: "old"}, "new").Apply([]ReplaceTarget{{Root: root, Path: "a.txt", Lines: []int{1}}})
	writeFile(t, path, "new and edited\n", 0o644)
	if n, skipped := undo.Restore(); n != 0 || len(skipped) != 1 {
		t.Fatalf("restore %d %v", n, skipped)
	}
	if got := readFile(t, path); got != "new and edited\n" {
		t.Fatalf("edit lost: %q", got)
	}
}

func TestRunAnnotatesMatchesWhenReplacing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "func oldName() {}\n", 0o644)
	var got []Match
	_, err := Run(t.Context(), Options{Root: root, Query: `old(\w+)`, Regex: true, ReplaceOn: true, Replace: "new$1"}, func(ms []Match) {
		got = append(got, ms...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Replacements) != 1 || got[0].Replacements[0] != "newName" {
		t.Fatalf("matches %+v", got)
	}
}
