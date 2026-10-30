package search

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const rgSample = `{"type":"begin","data":{"path":{"text":"./a.go"}}}
{"type":"match","data":{"path":{"text":"./a.go"},"lines":{"text":"func zażółć() {}\r\n"},"line_number":3,"absolute_offset":10,"submatches":[{"match":{"text":"zażółć"},"start":5,"end":15}]}}
{"type":"match","data":{"path":{"text":"./a.go"},"lines":{"text":"x foo foo\n"},"line_number":9,"absolute_offset":40,"submatches":[{"match":{"text":"foo"},"start":2,"end":5},{"match":{"text":"foo"},"start":6,"end":9}]}}
{"type":"end","data":{"path":{"text":"./a.go"},"binary_offset":null,"stats":{"matches":3}}}
this is not json at all
{"type":"begin","data":{"path":{"bytes":"/w=="}}}
{"type":"match","data":{"path":{"bytes":"/w=="},"lines":{"text":"skipped\n"},"line_number":1,"submatches":[{"start":0,"end":7}]}}
{"type":"begin","data":{"path":{"text":"./sub/b.sql"}}}
{"type":"match","data":{"path":{"text":"./sub/b.sql"},"lines":{"text":"SELECT 1\n"},"line_number":1,"submatches":[{"match":{"text":"SELECT"},"start":0,"end":6}]}}
{"type":"end","data":{"path":{"text":"./sub/b.sql"}}}
{"data":{"elapsed_total":{"secs":0}},"type":"summary"}
`

func TestParseRipgrep(t *testing.T) {
	var got []Match
	sum, err := parseRipgrep(strings.NewReader(rgSample), 100, func(m Match) { got = append(got, m) })
	if err != nil {
		t.Fatalf("parseRipgrep() error = %v", err)
	}
	if sum.Matches != 3 || sum.Files != 3 || sum.Truncated {
		t.Errorf("summary = %+v, want 3 matches, 3 begins, not truncated", sum)
	}
	want := []Match{
		{Path: "a.go", Line: 3, Text: "func zażółć() {}", Ranges: []Range{{5, 11}}},
		{Path: "a.go", Line: 9, Text: "x foo foo", Ranges: []Range{{2, 5}, {6, 9}}},
		{Path: "sub/b.sql", Line: 1, Text: "SELECT 1", Ranges: []Range{{0, 6}}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matches: %+v", len(got), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Path != w.Path || g.Line != w.Line || g.Text != w.Text || !slices.Equal(g.Ranges, w.Ranges) {
			t.Errorf("match[%d] = %+v, want %+v", i, g, w)
		}
	}
}

func TestParseRipgrepTruncates(t *testing.T) {
	var n int
	sum, err := parseRipgrep(strings.NewReader(rgSample), 2, func(Match) { n++ })
	if err != nil {
		t.Fatalf("parseRipgrep() error = %v", err)
	}
	if n != 2 || sum.Matches != 2 || !sum.Truncated {
		t.Errorf("emitted %d, summary %+v; want 2 and truncated", n, sum)
	}
}

func TestParseRipgrepEmpty(t *testing.T) {
	sum, err := parseRipgrep(strings.NewReader(""), 10, func(Match) { t.Error("unexpected emit") })
	if err != nil || sum != (Summary{}) {
		t.Errorf("got %+v, %v", sum, err)
	}
}

func TestSplitGlobs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{" , ,", nil},
		{"*.go", []string{"*.go"}},
		{" *.go , *.sql,,docs/** ", []string{"*.go", "*.sql", "docs/**"}},
	}
	for _, tt := range tests {
		if got := splitGlobs(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("splitGlobs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRipgrepArgs(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		has     []string
		hasNot  []string
		globSeq []string
	}{
		{
			name:   "literal, case-insensitive",
			opts:   Options{Query: "-v"},
			has:    []string{"--json", "--fixed-strings", "--ignore-case"},
			hasNot: []string{"--case-sensitive", "--word-regexp"},
		},
		{
			name:   "regex, case-sensitive, whole word",
			opts:   Options{Query: "a.b", Regex: true, CaseSensitive: true, WholeWord: true},
			has:    []string{"--case-sensitive", "--word-regexp"},
			hasNot: []string{"--fixed-strings", "--ignore-case"},
		},
		{
			name:    "globs",
			opts:    Options{Query: "x", Include: "*.go, *.sql", Exclude: "vendor/**"},
			globSeq: []string{"--glob", "*.go", "--glob", "*.sql", "--glob", "!vendor/**"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := ripgrepArgs(tt.opts)
			for _, a := range tt.has {
				if !slices.Contains(args, a) {
					t.Errorf("args %q missing %q", args, a)
				}
			}
			for _, a := range tt.hasNot {
				if slices.Contains(args, a) {
					t.Errorf("args %q unexpectedly contain %q", args, a)
				}
			}
			if tt.globSeq != nil && !strings.Contains(strings.Join(args, "\x00"), strings.Join(tt.globSeq, "\x00")) {
				t.Errorf("args %q missing glob sequence %q", args, tt.globSeq)
			}
			// The query must follow "--" so leading dashes aren't parsed as flags.
			n := len(args)
			if args[n-3] != "--" || args[n-2] != tt.opts.Query || args[n-1] != "." {
				t.Errorf("args tail = %q", args[n-3:])
			}
		})
	}
}

func exitErr(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+string(rune('0'+code))).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	return err
}

func TestRipgrepExitError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		sum     Summary
		stderr  string
		wantErr string
	}{
		{"success", nil, Summary{}, "", ""},
		{"truncated ignores kill", exitErr(t, 2), Summary{Truncated: true}, "", ""},
		{"no matches", exitErr(t, 1), Summary{}, "", ""},
		{"partial error with matches", exitErr(t, 2), Summary{Matches: 3}, "perm denied", ""},
		{"error uses stderr", exitErr(t, 2), Summary{}, "  regex parse error\n", "search failed: regex parse error"},
		{"error without stderr", exitErr(t, 2), Summary{}, "", "search failed: exit status 2"},
		{"non-exit error", errors.New("boom"), Summary{}, "", "rg: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ripgrepExitError(tt.err, tt.sum, tt.stderr)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("error = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
