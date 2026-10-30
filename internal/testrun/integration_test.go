package testrun_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/runner"
	"flint/internal/testrun"
)

// events collects what the runner reports for one run.
type events struct {
	mu      sync.Mutex
	console strings.Builder
	exited  chan runner.Result
}

func (e *events) Output(_ string, data []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.console.Write(data)
}

func (e *events) Exited(_ string, res runner.Result) { e.exited <- res }

// run executes a plan the way RunAPI.StartTests does and returns the final nodes.
func run(t *testing.T, plan testrun.Plan) (map[string]testrun.Node, string, runner.Result) {
	t.Helper()
	ev := &events{exited: make(chan runner.Result, 1)}
	parser := plan.NewParser()
	var mu sync.Mutex
	nodes := map[string]testrun.Node{}
	m := runner.NewManager(ev)
	err := m.Start("t", runner.Spec{Steps: plan.Steps, Cwd: plan.Cwd, Env: plan.Env, Filter: func(line []byte) []byte {
		shown, updates := parser.Line(line)
		mu.Lock()
		for _, n := range updates {
			nodes[n.ID] = n
		}
		mu.Unlock()
		if shown == nil {
			return line
		}
		return shown
	}})
	if err != nil {
		t.Fatal(err)
	}
	var res runner.Result
	select {
	case res = <-ev.exited:
	case <-time.After(60 * time.Second):
		t.Fatal("test run did not finish")
	}
	for _, n := range parser.Finish(res.Code, res.Stopped) {
		nodes[n.ID] = n
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	return nodes, ev.console.String(), res
}

func TestNodeTestEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	dir := t.TempDir()
	files := map[string]string{
		"package.json": `{"scripts":{"test":"node --test"}}`,
		"math.test.mjs": `import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
describe('math', () => {
  it('adds', () => assert.equal(1 + 1, 2))
  it('breaks', () => assert.equal(1 + 1, 3))
})
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "math.test.mjs")

	plan, err := testrun.PlanRun(testrun.Request{Lang: testrun.LangJS, Dirs: []string{dir}, File: file})
	if err != nil {
		t.Fatal(err)
	}
	nodes, console, res := run(t, plan)
	if res.Code == 0 {
		t.Errorf("a failing test must fail the run: %+v", res)
	}
	if nodes["math › adds"].Status != testrun.Passed || nodes["math › breaks"].Status != testrun.Failed || nodes["math"].Kind != testrun.KindSuite {
		t.Errorf("nodes: %+v", nodes)
	}
	real, _ := filepath.EvalSymlinks(file) // node reports real paths (/private/var on macOS)
	if loc := nodes["math › breaks"].Location; loc == nil || loc.Path != real || loc.Line != 5 {
		t.Errorf("failure location: %+v", loc)
	}
	if !strings.Contains(console, "✗ breaks") || strings.Contains(console, "# Subtest") {
		t.Errorf("console:\n%s", console)
	}

	// Rerun just the failing test by its full name.
	plan, err = testrun.PlanRun(testrun.Request{Lang: testrun.LangJS, Dirs: []string{dir}, File: file, Tests: []string{"math adds"}})
	if err != nil {
		t.Fatal(err)
	}
	nodes, _, res = run(t, plan)
	if res.Code != 0 || nodes["math › adds"].Status != testrun.Passed {
		t.Errorf("filtered run: %+v %+v", res, nodes)
	}
	if _, ran := nodes["math › breaks"]; ran {
		t.Errorf("the name pattern should skip other tests: %+v", nodes)
	}
}
