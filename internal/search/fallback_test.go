package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"flint/internal/ripgrep"
)

func TestCompilePattern(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		input   string
		want    []string
		wantErr bool
	}{
		{"literal escapes metachars", Options{Query: "a.b"}, "a.b axb", []string{"a.b"}, false},
		{"case-insensitive default", Options{Query: "Foo"}, "foo FOO fOo", []string{"foo", "FOO", "fOo"}, false},
		{"case-sensitive", Options{Query: "Foo", CaseSensitive: true}, "foo Foo", []string{"Foo"}, false},
		{"regex", Options{Query: `\d+`, Regex: true}, "a1 b22", []string{"1", "22"}, false},
		{"whole word", Options{Query: "cat", WholeWord: true}, "cat concat cat_ cat.", []string{"cat", "cat"}, false},
		{"whole word alternation grouped", Options{Query: "a|b", Regex: true, WholeWord: true}, "a ab b", []string{"a", "b"}, false},
		{"literal with regex syntax", Options{Query: "(unclosed"}, "x(unclosed", []string{"(unclosed"}, false},
		{"invalid regex", Options{Query: "(unclosed", Regex: true}, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := compilePattern(tt.opts)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
					t.Errorf("error = %v, want invalid pattern", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("compilePattern() error = %v", err)
			}
			if got := matchedText(m, tt.input); !slices.Equal(got, tt.want) {
				t.Errorf("matches = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompilePatternWholeWordUnicode(t *testing.T) {
	tests := []struct {
		query, line string
		want        bool
	}{
		{"żółw", "mały żółw idzie", true},
		{"kot", "kotś", false},
	}
	for _, tt := range tests {
		m, err := compilePattern(Options{Query: tt.query, WholeWord: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(m.findAll(tt.line)) > 0; got != tt.want {
			t.Errorf("whole word %q in %q = %v, want %v", tt.query, tt.line, got, tt.want)
		}
	}
}

func TestGlobsAllow(t *testing.T) {
	tests := []struct {
		name             string
		rel              string
		include, exclude string
		want             bool
	}{
		{"no globs", "a/b.go", "", "", true},
		{"include basename", "a/b.go", "*.go", "", true},
		{"include miss", "a/b.txt", "*.go,*.sql", "", false},
		{"include second glob", "q.sql", "*.go,*.sql", "", true},
		{"include full path", "cmd/main.go", "cmd/*.go", "", true},
		{"exclude basename", "a/b_test.go", "*.go", "*_test.go", false},
		{"exclude full path", "vendor/x.go", "", "vendor/*", false},
		{"exclude wins over include", "x.go", "*.go", "x.go", false},
		{"exclude miss", "a/b.go", "", "*.md", true},
		{"anchored exclude", "x.go", "", "/x.go", false},
		{"anchored exclude only at root", "a/x.go", "", "/x.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := globsAllow(tt.rel, splitGlobs(tt.include), splitGlobs(tt.exclude)); got != tt.want {
				t.Errorf("globsAllow(%q, %q, %q) = %v, want %v", tt.rel, tt.include, tt.exclude, got, tt.want)
			}
		})
	}
}

// newSearchTree builds a fixture shared by the fallback and ripgrep end-to-end tests.
func newSearchTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":                   "package main\n// TODO: zażółć\nfunc main() {}\n",
		"pkg/util.go":               "package pkg\n\nvar x = 1 // todo later\n",
		"docs/notes.md":             "nothing here\nTODO docs\n",
		"node_modules/dep/index.js": "// TODO in deps\n",
		"bin.dat":                   "TODO\x00binary",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func collect(t *testing.T) (*batcher, *[]Match) {
	t.Helper()
	var all []Match
	return newBatcher(func(ms []Match) { all = append(all, ms...) }), &all
}

func matchKeys(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Path + ":" + string(rune('0'+m.Line))
	}
	slices.Sort(out)
	return out
}

func TestRunFallback(t *testing.T) {
	root := newSearchTree(t)
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"case-insensitive skips node_modules and binaries", Options{Query: "todo"},
			[]string{"docs/notes.md:2", "main.go:2", "pkg/util.go:3"}},
		{"case-sensitive", Options{Query: "TODO", CaseSensitive: true},
			[]string{"docs/notes.md:2", "main.go:2"}},
		{"include", Options{Query: "todo", Include: "*.go"},
			[]string{"main.go:2", "pkg/util.go:3"}},
		{"exclude", Options{Query: "todo", Exclude: "*.md, pkg/*"},
			[]string{"main.go:2"}},
		{"folder excludes as rg reads them", Options{Query: "todo", Exclude: "docs/,/pkg"},
			[]string{"main.go:2"}},
		{"anchored folder exclude stays at the root", Options{Query: "todo", Exclude: "/dep/"},
			[]string{"docs/notes.md:2", "main.go:2", "pkg/util.go:3"}},
		{"no matches", Options{Query: "absent"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Root, tt.opts.MaxResults = root, 100
			b, all := collect(t)
			sum, err := runFallback(context.Background(), tt.opts, b)
			b.flush()
			if err != nil {
				t.Fatalf("runFallback() error = %v", err)
			}
			if got := matchKeys(*all); !slices.Equal(got, tt.want) {
				t.Errorf("matches = %v, want %v", got, tt.want)
			}
			if sum.Matches != len(tt.want) || sum.Files != len(tt.want) || sum.Truncated {
				t.Errorf("summary = %+v", sum)
			}
		})
	}
}

func TestRunFallbackTruncates(t *testing.T) {
	root := newSearchTree(t)
	b, all := collect(t)
	sum, err := runFallback(context.Background(), Options{Root: root, Query: "todo", MaxResults: 2}, b)
	b.flush()
	if err != nil {
		t.Fatalf("runFallback() error = %v", err)
	}
	if sum.Matches != 2 || !sum.Truncated || len(*all) != 2 {
		t.Errorf("summary = %+v, emitted %d; want 2 truncated", sum, len(*all))
	}
}

func TestRunFallbackRangesAreUTF16(t *testing.T) {
	root := newSearchTree(t)
	b, all := collect(t)
	if _, err := runFallback(context.Background(), Options{Root: root, Query: "ółć", MaxResults: 10}, b); err != nil {
		t.Fatal(err)
	}
	b.flush()
	if len(*all) != 1 {
		t.Fatalf("got %d matches", len(*all))
	}
	m := (*all)[0]
	if got := rangeText(m.Text, m.Ranges[0]); got != "ółć" {
		t.Errorf("range selects %q in %q", got, m.Text)
	}
}

func TestRunFallbackErrors(t *testing.T) {
	b, _ := collect(t)
	if _, err := runFallback(context.Background(), Options{Root: t.TempDir(), Query: "(", Regex: true, MaxResults: 1}, b); err == nil {
		t.Error("invalid regex: error = nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runFallback(ctx, Options{Root: newSearchTree(t), Query: "x", MaxResults: 1}, b); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: error = %v, want context.Canceled", err)
	}
}

func requireRipgrep(t *testing.T) {
	t.Helper()
	if ripgrep.Path() == "" {
		t.Skip("rg not installed")
	}
}

func TestRunEmptyQuery(t *testing.T) {
	_, err := Run(context.Background(), Options{Root: t.TempDir()}, func([]Match) {})
	if !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("error = %v, want ErrEmptyQuery", err)
	}
}

func TestRunWithRipgrep(t *testing.T) {
	requireRipgrep(t)
	root := newSearchTree(t)
	// rg doesn't skip node_modules by itself (no .gitignore here) but does skip binaries.
	var all []Match
	sum, err := Run(context.Background(), Options{Root: root, Query: "todo", Exclude: "node_modules/**"},
		func(ms []Match) { all = append(all, ms...) })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"docs/notes.md:2", "main.go:2", "pkg/util.go:3"}
	if got := matchKeys(all); !slices.Equal(got, want) {
		t.Errorf("matches = %v, want %v", got, want)
	}
	if sum.Engine != "ripgrep" || sum.Matches != 3 || sum.Files != 3 || sum.Truncated {
		t.Errorf("summary = %+v", sum)
	}
	for _, m := range all {
		if m.Path == "main.go" {
			if got := rangeText(m.Text, m.Ranges[0]); got != "TODO" {
				t.Errorf("range selects %q in %q", got, m.Text)
			}
		}
	}
}

func TestRunWithRipgrepTruncates(t *testing.T) {
	requireRipgrep(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "many.txt"), []byte(strings.Repeat("hit\n", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	var n int
	sum, err := Run(context.Background(), Options{Root: root, Query: "hit", MaxResults: 10}, func(ms []Match) { n += len(ms) })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !sum.Truncated || sum.Matches != 10 || n != 10 {
		t.Errorf("summary = %+v, emitted %d", sum, n)
	}
}

func TestRunWithRipgrepErrors(t *testing.T) {
	requireRipgrep(t)
	root := newSearchTree(t)

	_, err := Run(context.Background(), Options{Root: root, Query: "(unclosed", Regex: true}, func([]Match) {})
	if err == nil || !strings.Contains(err.Error(), "search failed") {
		t.Errorf("invalid regex: error = %v, want search failed", err)
	}

	sum, err := Run(context.Background(), Options{Root: root, Query: "(unclosed"}, func([]Match) {})
	if err != nil || sum.Matches != 0 {
		t.Errorf("literal with no matches: %+v, %v", sum, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Options{Root: root, Query: "todo"}, func([]Match) {}); err == nil {
		t.Error("cancelled context: error = nil")
	}
}

func TestRunWithRipgrepLongLine(t *testing.T) {
	requireRipgrep(t)
	root := t.TempDir()
	line := strings.Repeat("ą", 5000) + "NEEDLE" + strings.Repeat("b", 100)
	if err := os.WriteFile(filepath.Join(root, "long.txt"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var all []Match
	if _, err := Run(context.Background(), Options{Root: root, Query: "needle"}, func(ms []Match) { all = append(all, ms...) }); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(all) != 1 || len(all[0].Ranges) != 1 {
		t.Fatalf("matches = %+v", all)
	}
	if got := rangeText(all[0].Text, all[0].Ranges[0]); got != "NEEDLE" {
		t.Errorf("range selects %q", got)
	}
}

func matchedText(m *matcher, input string) []string {
	var out []string
	for _, sp := range m.findAll(input) {
		out = append(out, input[sp[0]:sp[1]])
	}
	return out
}

func TestRunRoots(t *testing.T) {
	a, b := newSearchTree(t), newSearchTree(t)
	var all []Match
	sum, err := RunRoots(context.Background(), []string{a, b}, Options{Query: "todo", Exclude: "node_modules/**"},
		func(ms []Match) { all = append(all, ms...) })
	if err != nil {
		t.Fatal(err)
	}
	perRoot := map[string]int{}
	for _, m := range all {
		perRoot[m.Root]++
	}
	if perRoot[a] == 0 || perRoot[a] != perRoot[b] || sum.Matches != len(all) {
		t.Fatalf("matches per root %v, summary %+v", perRoot, sum)
	}

	// The limit is shared: the second root gets what the first one left.
	all = nil
	sum, err = RunRoots(context.Background(), []string{a, b}, Options{Query: "todo", Exclude: "node_modules/**", MaxResults: perRoot[a] + 1},
		func(ms []Match) { all = append(all, ms...) })
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != perRoot[a]+1 || !sum.Truncated {
		t.Fatalf("limited search returned %d matches, summary %+v", len(all), sum)
	}

	if _, err := RunRoots(context.Background(), []string{a}, Options{}, func([]Match) {}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("empty query: %v", err)
	}
}
