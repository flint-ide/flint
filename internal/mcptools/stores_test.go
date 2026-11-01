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
	text, ok := ts.Tail("t1", "/p", 3)
	if !ok || text != "line 19997\nline 19998\nline 19999" {
		t.Fatalf("tail %q", text)
	}
	ts.mu.Lock()
	size := 0
	for _, s := range ts.byID["t1"].segs {
		size += len(s.data)
	}
	ts.mu.Unlock()
	if size > 2*maxTailBytes {
		t.Fatalf("buffer grew to %d", size)
	}
	full, _ := ts.Tail("t1", "/p", 100000)
	if strings.HasPrefix(full, "hello") || !strings.HasPrefix(full, "line ") {
		t.Fatalf("a cut tail must start at a whole line: %q", full[:20])
	}
	ts.Exit("t1", 3)
	if l := ts.List("/p"); !l[0].Exited || l[0].Code != 3 {
		t.Fatalf("exit not recorded: %+v", l)
	}
	ts.Close("t1")
	if _, ok := ts.Tail("t1", "/p", 1); ok {
		t.Fatal("closed terminal still listed")
	}
}

// Output printed before the shell moved into a project (e.g. a token shown
// in ~) never reaches that project's agent, and vice versa.
func TestTerminalOutputIsScopedToWhereItWasPrinted(t *testing.T) {
	ts := NewTerminals()
	ts.Open("t", KindTerminal, "", "/home")
	ts.Write("t", []byte("export TOKEN=secret-from-home\n"))
	ts.SetInfo("t", "", "/home/proj")
	ts.Write("t", []byte("go test ./...\nFAIL\n"))
	ts.SetInfo("t", "", "/home/other")
	ts.Write("t", []byte("other project output\n"))
	ts.SetInfo("t", "", "/home/proj/sub")
	ts.Write("t", []byte("back in proj\n"))

	text, ok := ts.Tail("t", "/home/proj", 100)
	if !ok || text != "go test ./...\nFAIL\nback in proj" {
		t.Fatalf("proj tail %q", text)
	}
	if l := ts.List("/home/other"); len(l) != 1 {
		t.Fatalf("a console that printed in other is listed there: %+v", l)
	}
	if text, _ := ts.Tail("t", "/home/other", 100); text != "other project output" {
		t.Fatalf("other tail %q", text)
	}
	if l := ts.List("/elsewhere"); len(l) != 0 {
		t.Fatalf("listed for an unrelated root: %+v", l)
	}
	// Trimming drops whole old segments first and keeps the byte bound.
	for range 3000 {
		ts.Write("t", []byte(strings.Repeat("x", 99)+"\n"))
	}
	ts.mu.Lock()
	c := ts.byID["t"]
	size, sum := c.size, 0
	for _, s := range c.segs {
		sum += len(s.data)
	}
	ts.mu.Unlock()
	if size != sum || size > 2*maxTailBytes {
		t.Fatalf("size %d, segments hold %d", size, sum)
	}
	if text, _ := ts.Tail("t", "/home", 100000); strings.Contains(text, "secret-from-home") {
		t.Fatal("old output not dropped")
	}
}

// OpenNew never wipes a running console (a duplicate start fails anyway)
// but starts a finished one over.
func TestOpenNew(t *testing.T) {
	ts := NewTerminals()
	if !ts.OpenNew("r", KindRun, "run", "/p") {
		t.Fatal("new id refused")
	}
	ts.Write("r", []byte("first\n"))
	if ts.OpenNew("r", KindRun, "run", "/p") {
		t.Fatal("running console replaced")
	}
	if text, _ := ts.Tail("r", "/p", 10); text != "first" {
		t.Fatalf("output lost: %q", text)
	}
	ts.Exit("r", 0)
	if !ts.OpenNew("r", KindRun, "run again", "/p") {
		t.Fatal("finished console not started over")
	}
	if text, _ := ts.Tail("r", "/p", 10); text != "" {
		t.Fatalf("old output kept: %q", text)
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
	if _, ok := ts.Tail("0", "/p", 1); ok {
		t.Fatal("oldest not evicted")
	}
	ts.Exit("40", 0)
	ts.Open("new", KindRun, "run", "/p")
	if _, ok := ts.Tail("40", "/p", 1); ok {
		t.Fatal("a finished console must go before older live ones")
	}
	if _, ok := ts.Tail("5", "/p", 1); !ok {
		t.Fatal("a live console was evicted while a finished one was kept")
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

func TestDiagnosticsEvictLeastRecentAndClearPerRoot(t *testing.T) {
	d := NewDiagnostics()
	one := []lsp.Diagnostic{{Severity: 1, Message: "x"}}
	for i := range maxDiagFiles {
		d.Set(fmt.Sprintf("/p/f%d.go", i), one)
	}
	d.Set("/p/f0.go", one) // updated: now the newest
	d.Set("/p/new.go", one)
	got := d.In("/p")
	if len(got) != maxDiagFiles || got["/p/new.go"] == nil || got["/p/f0.go"] == nil || got["/p/f1.go"] != nil {
		t.Fatalf("%d files; new %v f0 %v f1 %v", len(got), got["/p/new.go"] != nil, got["/p/f0.go"] != nil, got["/p/f1.go"] != nil)
	}
	d.Set("/q/a.go", one)
	d.ClearRoot("/p")
	if len(d.In("/p")) != 0 || len(d.In("/q")) != 1 {
		t.Fatal("ClearRoot cleared the wrong files")
	}
}

func TestTestsStoreHasAByteBudget(t *testing.T) {
	ts := NewTests()
	big := strings.Repeat("o", 1<<20)
	for run := range 6 {
		id := fmt.Sprint("r", run)
		ts.Begin(id, "t", "/p")
		for i := range 8 {
			ts.Update(id, []testrun.Node{{ID: fmt.Sprint(i), Kind: testrun.KindTest, Status: testrun.Failed, Output: big}})
		}
		ts.mu.Lock()
		r := ts.runLocked(id)
		if r.bytes > maxRunBytes+(1<<10) {
			t.Fatalf("run %d holds %d bytes", run, r.bytes)
		}
		ts.mu.Unlock()
	}
	ts.mu.Lock()
	total := 0
	for _, r := range ts.runs {
		total += r.bytes
	}
	ts.mu.Unlock()
	if total > maxTotalBytes {
		t.Fatalf("store holds %d bytes", total)
	}
	last, _ := ts.Last("/p")
	if last.Nodes[7].Output != droppedOutput || last.Nodes[0].Output != big {
		t.Fatal("output over the run budget not replaced")
	}
	ts.Forget("r5")
	ts.mu.Lock()
	gone := ts.runLocked("r5") == nil
	ts.mu.Unlock()
	if !gone {
		t.Fatal("Forget kept the run")
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
