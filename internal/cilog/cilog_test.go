package cilog

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "log_failed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParse(t *testing.T) {
	steps := Parse(fixture(t))
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	s := steps[0]
	if s.Job != "Build and test (macOS)" || s.Step != "go test" {
		t.Errorf("step = %q / %q", s.Job, s.Step)
	}
	if s.Lines[0] != "Run go test -race ./..." {
		t.Errorf("first line = %q (BOM, timestamp and ##[group] should be gone)", s.Lines[0])
	}
	if s.Lines[1] != "go test -race ./..." {
		t.Errorf("ANSI colours kept: %q", s.Lines[1])
	}
	if slices.Contains(s.Lines, "##[endgroup]") || slices.Contains(s.Lines, "") {
		t.Errorf("markers kept: %q", s.Lines)
	}
	if last := s.Lines[len(s.Lines)-1]; last != "Error: Process completed with exit code 1." {
		t.Errorf("last line = %q", last)
	}
	if !slices.Contains(s.Lines, "ok  \tflint\t12.471s") {
		t.Error("tabs inside the message were lost")
	}
}

func TestParseSeveralSteps(t *testing.T) {
	raw := "build\tlint\t2026-01-01T00:00:00.0000000Z a\n" +
		"build\tlint\t2026-01-01T00:00:00.0000000Z b\n" +
		"test (1.22)\tgo test\t2026-01-01T00:00:00.0000000Z c\n" +
		"no tabs here\n"
	steps := Parse(raw)
	if len(steps) != 2 || steps[0].Step != "lint" || len(steps[0].Lines) != 2 || steps[1].Job != "test (1.22)" {
		t.Fatalf("steps = %+v", steps)
	}
	if got := steps[1].Lines; !slices.Equal(got, []string{"c", "no tabs here"}) {
		t.Errorf("a line without columns belongs to the step before: %q", got)
	}
	if got := Find(steps, "test (1.22)", "go test"); got == nil || got.Lines[0] != "c" {
		t.Errorf("Find = %+v", got)
	}
	if got := Find(steps, "test (1.22)", "other"); got == nil || got.Step != "go test" {
		t.Errorf("Find falls back to the job's last step: %+v", got)
	}
	if Find(steps, "nope", "") != nil {
		t.Error("Find of an unknown job")
	}
}

func TestExcerpt(t *testing.T) {
	lines := []string{}
	for i := range 100 {
		lines = append(lines, "noise "+strings.Repeat("x", i%3))
	}
	lines[10] = "internal/ratelimit/bucket.go:42:9: undefined: refillRate"
	lines[20] = "--- FAIL: TestBucket (0.01s)"
	lines[21] = "    bucket_test.go:17: got 3, want 4"
	lines[30] = "panic: runtime error: index out of range"
	ex := Excerpt(lines, 5, 3)
	want := []string{
		"…",
		lines[10],
		"…",
		lines[20],
		lines[21],
		"…",
		lines[95], lines[96], lines[97], lines[98], lines[99],
	}
	if !slices.Equal(ex.Lines, want) {
		t.Errorf("excerpt =\n%s\nwant\n%s", strings.Join(ex.Lines, "\n"), strings.Join(want, "\n"))
	}
	if ex.Omitted != 100-8 {
		t.Errorf("omitted = %d", ex.Omitted)
	}
	// No error lines: just the tail. Everything fits: everything.
	if ex := Excerpt(lines[:4], 10, 0); !slices.Equal(ex.Lines, lines[:4]) || ex.Omitted != 0 {
		t.Errorf("short = %+v", ex)
	}
	long := strings.Repeat("é", maxLineRunes+50)
	if ex := Excerpt([]string{long}, 5, 5); len([]rune(ex.Lines[0])) != maxLineRunes+1 {
		t.Errorf("long line kept %d runes", len([]rune(ex.Lines[0])))
	}
}

func TestErrorLines(t *testing.T) {
	yes := []string{
		"--- FAIL: TestX (0.00s)",
		"FAIL\tflint/internal/x\t0.1s",
		"panic: boom",
		"main.go:12:3: expected ';'",
		"src/app.ts(12,5): error TS2322: Type 'string' is not assignable",
		"Error: Process completed with exit code 1.",
		"npm ERR! code ELIFECYCLE",
		"  ✖ adds numbers",
		"not ok 3 - parses dates",
		"AssertionError [ERR_ASSERTION]: 1 == 2",
		"E   assert 1 == 2",
		"FAILED tests/test_x.py::test_y - assert 1 == 2",
		"Traceback (most recent call last):",
		"error[E0308]: mismatched types",
	}
	for _, l := range yes {
		if !isErrorLine(l) {
			t.Errorf("not an error line: %q", l)
		}
	}
	for _, l := range []string{"ok  \tflint\t1.0s", "Run go test ./...", "ld: warning: ignoring duplicate libraries", "shell: /bin/bash -e {0}"} {
		if isErrorLine(l) {
			t.Errorf("error line: %q", l)
		}
	}
}

func TestRefs(t *testing.T) {
	lines := []string{
		"internal/ratelimit/bucket.go:42:9: undefined: refillRate",
		"    review_test.go:316: Stop took 4.3s",
		"src/app.ts(12,5): error TS2322",
		"/home/runner/work/flint/flint/cmd/x/main.go:7 +0x1d",
		"see https://example.com/a/b.go:12 for details",
		"version 1.2.3:4",
		"ld: warning: object file (/private/var/folders/go.o) was built",
	}
	got := Refs(lines, 10)
	want := []Ref{
		{Text: "internal/ratelimit/bucket.go:42:9", File: "internal/ratelimit/bucket.go", Line: 42, Column: 9},
		{Text: "review_test.go:316", File: "review_test.go", Line: 316},
		{Text: "src/app.ts(12,5)", File: "src/app.ts", Line: 12, Column: 5},
		{Text: "/home/runner/work/flint/flint/cmd/x/main.go:7", File: "/home/runner/work/flint/flint/cmd/x/main.go", Line: 7},
	}
	if len(Refs(lines, 1)) != 1 || Refs(lines, 0) != nil {
		t.Error("limit ignored")
	}
	if !slices.Equal(got, want) {
		t.Errorf("Refs =\n%+v\nwant\n%+v", got, want)
	}
}

func repo(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolver(t *testing.T) {
	root := repo(t,
		"go.mod",
		"internal/ratelimit/bucket.go",
		"internal/remote/review_test.go",
		"internal/other/review_test.go",
		"cmd/x/main.go",
		"src/app.ts",
	)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("// x\nmodule flint\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := []string{"--- FAIL: TestStop (4.62s)", "    review_test.go:316: x", "FAIL\tflint/internal/remote\t5.546s", "FAIL\tflint/internal/ratelimit [build failed]"}
	r := NewResolver(context.Background(), root, log)
	defer r.Close()
	tests := map[string]string{
		"internal/ratelimit/bucket.go":                "internal/ratelimit/bucket.go",
		"./internal/ratelimit/bucket.go":              "internal/ratelimit/bucket.go",
		"review_test.go":                              "internal/remote/review_test.go", // the failing package wins
		"/home/runner/work/flint/flint/cmd/x/main.go": "cmd/x/main.go",
		`D:\a\flint\flint\src\app.ts`:                 "src/app.ts",
		"flint/src/app.ts":                            "src/app.ts",
		"bucket.go":                                   "internal/ratelimit/bucket.go",
		"../outside.go":                               "",
		"/etc/passwd":                                 "",
		"missing.go":                                  "",
		"internal/ratelimit":                          "", // a folder is not a file
		"go.mod\x00x":                                 "",
	}
	for in, want := range tests {
		got, _ := r.Resolve(in)
		if got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolverWithoutGoModule(t *testing.T) {
	root := repo(t, "lib/a.py")
	r := NewResolver(context.Background(), root, []string{"FAIL\tsomething/else"})
	defer r.Close()
	if got, ok := r.Resolve("lib/a.py"); !ok || got != "lib/a.py" {
		t.Errorf("Resolve = %q, %v", got, ok)
	}
	if got, ok := r.Resolve("a.py"); ok {
		t.Errorf("a bare name outside known packages resolved to %q", got)
	}
}
