package testrun

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// collect feeds output through a parser and returns the final state per node.
func collect(p Parser, out string, exitCode int) (map[string]Node, string) {
	final := map[string]Node{}
	var console strings.Builder
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := append(sc.Bytes(), '\n')
		shown, updates := p.Line(line)
		if shown == nil {
			console.Write(line)
		} else {
			console.Write(shown)
		}
		for _, n := range updates {
			final[n.ID] = n
		}
	}
	for _, n := range p.Finish(exitCode, false) {
		final[n.ID] = n
	}
	return final, console.String()
}

func TestGoParserEvents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Join([]string{
		`go: downloading example.com/x v1.0.0`,
		`{"Action":"start","Package":"m/a"}`,
		`{"Action":"run","Package":"m/a","Test":"TestA"}`,
		`{"Action":"output","Package":"m/a","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"run","Package":"m/a","Test":"TestA/sub_one"}`,
		`{"Action":"output","Package":"m/a","Test":"TestA/sub_one","Output":"    a_test.go:1: boom\n"}`,
		`{"Action":"fail","Package":"m/a","Test":"TestA/sub_one","Elapsed":0.01}`,
		`{"Action":"fail","Package":"m/a","Test":"TestA","Elapsed":0.02}`,
		`{"Action":"run","Package":"m/a","Test":"TestB"}`,
		`{"Action":"output","Package":"m/a","Test":"TestB","Output":"    a_test.go:9: not today\n"}`,
		`{"Action":"skip","Package":"m/a","Test":"TestB"}`,
		`{"Action":"run","Package":"m/a","Test":"TestHang"}`,
		`{"Action":"output","Package":"m/a","Output":"FAIL\tm/a\t0.1s\n"}`,
		`{"Action":"fail","Package":"m/a","Elapsed":0.1}`,
	}, "\n")
	nodes, console := collect(NewGoParser(dir, func(string) string { return dir }), out, 1)

	sub := nodes["m/a::TestA/sub_one"]
	if sub.Status != Failed || sub.Parent != "m/a::TestA" || sub.Name != "sub_one" || sub.Test != "TestA/sub_one" || sub.ElapsedMs != 10 {
		t.Errorf("subtest: %+v", sub)
	}
	if sub.Location == nil || sub.Location.Path != filepath.Join(dir, "a_test.go") || sub.Location.Line != 1 {
		t.Errorf("subtest location: %+v", sub.Location)
	}
	if !strings.Contains(sub.Output, "boom") {
		t.Errorf("subtest output: %q", sub.Output)
	}
	if a := nodes["m/a::TestA"]; a.Parent != "m/a" || a.Kind != KindTest || strings.Contains(a.Output, "=== RUN") {
		t.Errorf("TestA: %+v", a)
	}
	if b := nodes["m/a::TestB"]; b.Status != Skipped || !strings.Contains(b.Output, "not today") {
		t.Errorf("TestB: %+v", b)
	}
	if h := nodes["m/a::TestHang"]; h.Status != Failed {
		t.Errorf("unfinished test after a failing run: %+v", h)
	}
	if pkg := nodes["m/a"]; pkg.Status != Failed || pkg.Kind != KindPackage || pkg.Dir != dir {
		t.Errorf("package: %+v", pkg)
	}
	for _, want := range []string{"go: downloading", "=== RUN   TestA", "boom", "FAIL\tm/a"} {
		if !strings.Contains(console, want) {
			t.Errorf("console lacks %q:\n%s", want, console)
		}
	}
	if strings.Contains(console, `"Action"`) {
		t.Errorf("raw JSON leaked into the console:\n%s", console)
	}
}

// TestGoParserRealRun runs a real `go test -json` on a tiny module.
func TestGoParserRealRun(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/tiny\n\ngo 1.21\n")
	write("calc/calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	write("calc/calc_test.go", `package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("bad")
	}
}

func TestTable(t *testing.T) {
	t.Run("one plus one", func(t *testing.T) {
		if Add(1, 1) != 3 {
			t.Errorf("want 3, got %d", Add(1, 1))
		}
	})
	t.Run("zero", func(t *testing.T) {})
}

func TestSkip(t *testing.T) { t.Skip("later") }
`)
	write("broken/broken_test.go", "package broken\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { undefined() }\n")

	plan, err := PlanRun(Request{Lang: LangGo, Dirs: []string{filepath.Join(root, "calc"), filepath.Join(root, "broken")}})
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Steps[0]
	cmd := exec.Command(step.Program, step.Args...)
	cmd.Dir = plan.Cwd
	cmd.Env = append(os.Environ(), "GOFLAGS=-count=1")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	runErr := cmd.Run()
	code := 0
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	}
	nodes, _ := collect(plan.NewParser(), stdout.String(), code)

	want := map[string]Status{
		"example.com/tiny/calc":                         Failed,
		"example.com/tiny/calc::TestAdd":                Passed,
		"example.com/tiny/calc::TestTable":              Failed,
		"example.com/tiny/calc::TestTable/one_plus_one": Failed,
		"example.com/tiny/calc::TestTable/zero":         Passed,
		"example.com/tiny/calc::TestSkip":               Skipped,
		"example.com/tiny/broken":                       Failed,
	}
	for id, status := range want {
		if got := nodes[id]; got.Status != status {
			t.Errorf("%s: status %q, want %q (%+v)", id, got.Status, status, got)
		}
	}
	sub := nodes["example.com/tiny/calc::TestTable/one_plus_one"]
	if sub.Location == nil || sub.Location.Path != filepath.Join(root, "calc", "calc_test.go") || sub.Location.Line != 14 {
		t.Errorf("failure location: %+v", sub.Location)
	}
	if pkg := nodes["example.com/tiny/calc"]; pkg.Dir != filepath.Join(root, "calc") {
		t.Errorf("package dir: %q", pkg.Dir)
	}
	broken := nodes["example.com/tiny/broken"]
	if !strings.Contains(broken.Output, "undefined") || broken.Location == nil || broken.Location.Line != 5 {
		t.Errorf("build failure: %+v", broken)
	}

	// A single subtest reruns precisely.
	plan, err = PlanRun(Request{Lang: LangGo, Dirs: []string{filepath.Join(root, "calc")}, Tests: []string{"TestTable/zero"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan.Steps[0].Args, " "); got != "test -json -run ^TestTable$/^zero$ ./calc" {
		t.Errorf("args: %s", got)
	}
}

func TestGoRunPattern(t *testing.T) {
	cases := map[string][]string{
		"^TestA$":           {"TestA"},
		"^TestA$/^a\\.b$":   {"TestA/a.b"},
		"^(TestA|TestB)$":   {"TestA/x", "TestB", "TestA/y"},
		"^(Test\\(x\\)|T)$": {"Test(x)", "T"},
	}
	for want, tests := range cases {
		if got := GoRunPattern(tests); got != want {
			t.Errorf("GoRunPattern(%q) = %s, want %s", tests, got, want)
		}
	}
}
