package mcptools

import (
	"fmt"
	"strings"
	"testing"

	"flint/internal/lsp"
	"flint/internal/testrun"
)

func TestPlainText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain\nlines\n", "plain\nlines"},
		{"\x1b[31mred\x1b[0m text", "red text"},
		{"\x1b]0;title\x07after", "after"},
		{"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"progress 10%\rprogress 100%\n", "progress 100%"},
		{"crlf\r\nnext", "crlf\nnext"},
		{"abc\b\bX", "aX"},
		{"tab\there\x00\x07", "tab\there"},
		{"trailing   \n", "trailing"},
		{"\x1b(Bcharset", "charset"},
		{"zażółć \x1b[1;32mgęślą\x1b[m", "zażółć gęślą"},
	} {
		if got := PlainText([]byte(tc.in)); got != tc.want {
			t.Errorf("PlainText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Fatalf("got %q", got)
	}
	if got := lastLines("a\nb", 5); got != "a\nb" {
		t.Fatalf("got %q", got)
	}
}

func TestTerminalsKeepABoundedTailPerTerminal(t *testing.T) {
	ts := NewTerminals()
	ts.Open("t1", KindTerminal, "", "/p")
	ts.Open("t2", KindTerminal, "", "/elsewhere")
	ts.Write("t1", []byte("hello\n"))
	ts.Write("unknown", []byte("ignored"))
	ts.SetInfo("t1", "npm run dev", "/p/web")
	for i := range 20000 {
		ts.Write("t1", fmt.Appendf(nil, "line %d\n", i))
	}
	list := ts.List("/p")
	if len(list) != 1 || list[0].ID != "t1" || list[0].Title != "npm run dev" || list[0].Cwd != "/p/web" {
		t.Fatalf("list %+v", list)
	}
	text, ok := ts.Tail("t1", 3)
	if !ok || text != "line 19997\nline 19998\nline 19999" {
		t.Fatalf("tail %q", text)
	}
	ts.mu.Lock()
	size := len(ts.byID["t1"].buf)
	ts.mu.Unlock()
	if size > 2*maxTailBytes {
		t.Fatalf("buffer grew to %d", size)
	}
	full, _ := ts.Tail("t1", 100000)
	if strings.HasPrefix(full, "hello") || !strings.HasPrefix(full, "line ") {
		t.Fatalf("a cut tail must start at a whole line: %q", full[:20])
	}
	ts.Exit("t1", 3)
	if l := ts.List("/p"); !l[0].Exited || l[0].Code != 3 {
		t.Fatalf("exit not recorded: %+v", l)
	}
	ts.Close("t1")
	if _, ok := ts.Tail("t1", 1); ok {
		t.Fatal("closed terminal still listed")
	}
}

func TestTerminalsEvictOldest(t *testing.T) {
	ts := NewTerminals()
	for i := range maxTerminals + 5 {
		ts.Open(fmt.Sprint(i), KindRun, "run", "/p")
	}
	if n := len(ts.List("/p")); n != maxTerminals {
		t.Fatalf("%d terminals kept", n)
	}
	if _, ok := ts.Tail("0", 1); ok {
		t.Fatal("oldest not evicted")
	}
}

func TestDiagnosticsStore(t *testing.T) {
	d := NewDiagnostics()
	d.Set("/p/a.go", []lsp.Diagnostic{{Severity: 1, Message: "boom"}})
	d.Set("/p/b.go", []lsp.Diagnostic{{Severity: 2, Message: "meh"}})
	d.Set("/other/c.go", []lsp.Diagnostic{{Severity: 1, Message: "x"}})
	d.Set("relative.go", []lsp.Diagnostic{{Severity: 1}})
	got := d.In("/p")
	if len(got) != 2 || got["/p/a.go"][0].Message != "boom" {
		t.Fatalf("in /p: %+v", got)
	}
	d.Set("/p/a.go", nil)
	if got := d.In("/p"); len(got) != 1 {
		t.Fatalf("cleared file kept: %+v", got)
	}
	many := make([]lsp.Diagnostic, maxDiagsPerFile+10)
	d.Set("/p/many.go", many)
	if n := len(d.In("/p")["/p/many.go"]); n != maxDiagsPerFile {
		t.Fatalf("kept %d", n)
	}
}

func TestEditorSnapshotValidation(t *testing.T) {
	e := NewEditor()
	ok := EditorSnapshot{
		Files:     []OpenFile{{Path: "/p/a.go", Dirty: true}, {Path: "/q/b.go"}},
		Active:    "/p/a.go",
		Selection: &Selection{StartLine: 3, StartColumn: 1, EndLine: 4, EndColumn: 2, Text: "x := 1"},
	}
	if err := e.Set(ok); err != nil {
		t.Fatal(err)
	}
	in := e.In("/p")
	if len(in.Files) != 1 || in.Active != "/p/a.go" || in.Selection == nil || in.Selection.Text != "x := 1" {
		t.Fatalf("in /p: %+v", in)
	}
	if out := e.In("/q"); out.Active != "" || out.Selection != nil || len(out.Files) != 1 {
		t.Fatalf("in /q: %+v", out)
	}
	bad := []EditorSnapshot{
		{Files: []OpenFile{{Path: "relative.go"}}},
		{Files: []OpenFile{{Path: "/p/../etc/passwd"}}},
		{Active: "/p/a.go\n"},
		{Files: make([]OpenFile, maxOpenFiles+1)},
		{Active: "/p/a.go", Selection: &Selection{StartLine: 0, EndLine: 1}},
		{Active: "/p/a.go", Selection: &Selection{StartLine: 5, EndLine: 4}},
		{Selection: &Selection{StartLine: 1, EndLine: 1}},
	}
	for i, s := range bad {
		if err := e.Set(s); err == nil {
			t.Errorf("bad snapshot %d accepted", i)
		}
	}
	if e.In("/p").Active != "/p/a.go" {
		t.Fatal("a rejected snapshot replaced the last good one")
	}
	long := ok
	long.Selection = &Selection{StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 1, Text: strings.Repeat("ł", maxSelection)}
	if err := e.Set(long); err != nil {
		t.Fatal(err)
	}
	if n := len(e.In("/p").Selection.Text); n > maxSelection {
		t.Fatalf("selection of %d bytes kept", n)
	}
}

func TestTestsStoreKeepsLastRunPerProject(t *testing.T) {
	ts := NewTests()
	ts.Begin("r1", "go test ./...", "/p")
	ts.Update("r1", []testrun.Node{
		{ID: "pkg", Name: "flint/a", Kind: testrun.KindPackage, Status: testrun.Running},
		{ID: "t1", Parent: "pkg", Name: "TestA", Test: "TestA", Kind: testrun.KindTest, Status: testrun.Running},
	})
	ts.Update("r1", []testrun.Node{{ID: "t1", Parent: "pkg", Name: "TestA", Test: "TestA", Kind: testrun.KindTest, Status: testrun.Failed, Output: "boom"}})
	ts.Begin("r2", "other", "/q")
	run, ok := ts.Last("/p")
	if !ok || run.Title != "go test ./..." || run.Done || len(run.Nodes) != 2 || run.Nodes[1].Status != testrun.Failed {
		t.Fatalf("running: %+v", run)
	}
	ts.Finish("r1", 1, false, 1500, "")
	run, _ = ts.Last("/p")
	if !run.Done || run.ExitCode != 1 || run.ElapsedMs != 1500 {
		t.Fatalf("finished: %+v", run)
	}
	if _, ok := ts.Last("/nothing"); ok {
		t.Fatal("run found for another project")
	}
	for i := range maxRuns + 3 {
		ts.Begin(fmt.Sprint("x", i), "t", "/z")
	}
	ts.mu.Lock()
	n := len(ts.runs)
	ts.mu.Unlock()
	if n != maxRuns {
		t.Fatalf("%d runs kept", n)
	}
}
