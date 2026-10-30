package testrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLang(t *testing.T) {
	cases := map[string]string{
		"/x/a_test.go": LangGo, "/x/a.go": "", "/x/store.test.ts": LangJS, "/x/View.test.tsx": LangJS,
		"/x/a.spec.mjs": LangJS, "/x/test.ts": "", "/x/latest.ts": "",
	}
	for path, want := range cases {
		if got := Lang(path); got != want {
			t.Errorf("Lang(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestGoMarks(t *testing.T) {
	src := "// Copyright\npackage calc\n\nimport \"testing\"\n\n" +
		"func TestAdd(t *testing.T) {\n" + // 6
		"\tt.Run(\"one plus one\", func(t *testing.T) {\n" + // 7
		"\t\tt.Run(`inner`, func(t *testing.T) {})\n" + // 8
		"\t})\n" +
		"\tt.Run(\"two\", func(t *testing.T) {})\n" + // 10
		"\tt.Run(tc.name, func(t *testing.T) {})\n" +
		"}\n" +
		"func helper(t *testing.T) { t.Run(\"x\", nil) }\n" +
		"func Testing(t *testing.T) {}\n" + // not a test: lower case after Test
		"func FuzzParse(f *testing.F) {}\n" + // 15
		"func BenchmarkX(b *testing.B) {}\n"
	got := Marks("/p/calc_test.go", src)
	want := []Mark{
		{Line: 2, Kind: MarkFile},
		{Line: 6, Kind: MarkTest, Name: "TestAdd"},
		{Line: 7, Kind: MarkSubtest, Name: "TestAdd/one_plus_one"},
		{Line: 8, Kind: MarkSubtest, Name: "TestAdd/one_plus_one/inner"},
		{Line: 10, Kind: MarkSubtest, Name: "TestAdd/two"},
		{Line: 15, Kind: MarkTest, Name: "FuzzParse"},
	}
	if len(got) != len(want) {
		t.Fatalf("marks = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mark %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestJSMarks(t *testing.T) {
	src := "import { describe, it, test } from 'node:test'\n" +
		"describe('store', () => {\n" + // 2
		"  it(\"keeps 'quotes'\", () => {})\n" + // 3
		"  describe.skip(`nested`, () => {\n" + // 4
		"    test('it\\'s deep', async () => {})\n" + // 5
		"  })\n" +
		"  it(`dynamic ${x}`, () => {})\n" +
		"})\n" +
		"test('top', () => {})\n" // 9
	got := Marks("/p/store.test.ts", src)
	want := []Mark{
		{Line: 1, Kind: MarkFile},
		{Line: 2, Kind: MarkSuite, Name: "store"},
		{Line: 3, Kind: MarkTest, Name: "store keeps 'quotes'"},
		{Line: 4, Kind: MarkSuite, Name: "store nested"},
		{Line: 5, Kind: MarkTest, Name: "store nested it's deep"},
		{Line: 9, Kind: MarkTest, Name: "top"},
	}
	if len(got) != len(want) {
		t.Fatalf("marks = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mark %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPlanJS(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) string {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mk("web/package.json", `{"scripts":{"test":"go run ../cmd/webbuild --test"}}`)
	file := mk("web/src/lib/a.test.ts", "")
	plan, err := PlanRun(Request{Lang: LangJS, Dirs: []string{filepath.Dir(file)}, File: file, Tests: []string{"suite it's"}})
	if err != nil {
		t.Fatal(err)
	}
	line := plan.Steps[0].Args[1]
	if plan.Format != FormatTAP || plan.Cwd != filepath.Join(root, "web") ||
		line != `go run ../cmd/webbuild --test -- --test-reporter=tap '--test-name-pattern=^(?:suite it'\''s)$' `+file {
		t.Errorf("webbuild plan: %s (%+v)", line, plan)
	}
	if !strings.HasPrefix(plan.Env[0], "PATH="+filepath.Join(root, "web", "node_modules", ".bin")+":") {
		t.Errorf("env: %v", plan.Env)
	}

	mk("node/package.json", `{"scripts":{"test":"node --import tsx --test src/"}}`)
	nfile := mk("node/src/x.test.ts", "")
	plan, err = PlanRun(Request{Lang: LangJS, Dirs: []string{filepath.Join(root, "node")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Steps[0].Args[1]; got != "node --import tsx --test --test-reporter=tap src/" {
		t.Errorf("node script: %s", got)
	}
	plan, err = PlanRun(Request{Lang: LangJS, Dirs: []string{filepath.Dir(nfile)}, File: nfile})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Steps[0].Label; got != "node --import tsx --test --test-reporter=tap "+nfile {
		t.Errorf("node file: %s", got)
	}

	mk("jest/package.json", `{"scripts":{"test":"jest"}}`)
	plan, err = PlanRun(Request{Lang: LangJS, Dirs: []string{filepath.Join(root, "jest")}})
	if err != nil || plan.Format != FormatPlain {
		t.Errorf("other runner: %+v %v", plan, err)
	}
	nodes, _ := collect(plan.NewParser(), "PASS x\n", 0)
	if len(nodes) != 1 {
		t.Errorf("plain parser nodes: %+v", nodes)
	}
	for _, n := range nodes {
		if n.Status != Passed {
			t.Errorf("plain result: %+v", n)
		}
	}

	if _, err := PlanRun(Request{Lang: LangJS, Dirs: []string{"relative"}}); err == nil {
		t.Error("relative dirs must be rejected")
	}
	if _, err := PlanRun(Request{Lang: "rust", Dirs: []string{root}}); err == nil {
		t.Error("unknown language must be rejected")
	}
}
