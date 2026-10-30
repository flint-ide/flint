package testrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Output of node 22 `node --test --test-reporter=tap` (paths shortened).
const nodeTAP = `TAP version 13
# hello out
# Subtest: suite one
    # Subtest: passes
    ok 1 - passes
      ---
      duration_ms: 0.2465
      type: 'test'
      ...
    # Subtest: fails
    not ok 2 - fails
      ---
      duration_ms: 0.640167
      type: 'test'
      location: '/nope/a.test.mjs:5:3'
      failureType: 'testCodeFailure'
      error: |-
        Expected values to be strictly equal:

        1 !== 2

      code: 'ERR_ASSERTION'
      name: 'AssertionError'
      expected: 2
      actual: 1
      operator: 'strictEqual'
      stack: |-
        TestContext.<anonymous> (file://SRC:5:30)
        Test.runInAsyncScope (node:async_hooks:214:14)
      ...
    # Subtest: inner
        # Subtest: deep
        ok 1 - deep
          ---
          duration_ms: 0.058625
          type: 'test'
          ...
        1..1
    ok 3 - inner
      ---
      duration_ms: 0.16275
      type: 'suite'
      ...
    1..3
not ok 1 - suite one
  ---
  duration_ms: 1.428583
  type: 'suite'
  location: '/nope/a.test.mjs:3:1'
  failureType: 'subtestsFailed'
  error: '1 subtest failed'
  code: 'ERR_TEST_FAILURE'
  ...
# Subtest: top skip
ok 2 - top skip # SKIP not now
  ---
  duration_ms: 0.0905
  type: 'test'
  ...
# Subtest: top \# todo
ok 3 - top \# todo # TODO
  ---
  duration_ms: 0.039459
  type: 'test'
  ...
# Subtest: top skip
ok 4 - top skip
  ---
  duration_ms: 0.2
  type: 'test'
  ...
# Subtest: never finished
1..5
# tests 7
# pass 4
# fail 1
`

func TestTAPParser(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.test.ts")
	if err := os.WriteFile(src, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nodes, console := collect(NewTAPParser(dir), strings.ReplaceAll(nodeTAP, "SRC", src), 1)

	fails := nodes["suite one › fails"]
	if fails.Status != Failed || fails.Parent != "suite one" || fails.Test != "suite one fails" || fails.ElapsedMs != 0.640167 {
		t.Errorf("fails: %+v", fails)
	}
	if !strings.Contains(fails.Output, "1 !== 2") || !strings.Contains(fails.Output, "expected: 2") || strings.Contains(fails.Output, "file://") {
		t.Errorf("fails output: %q", fails.Output)
	}
	if fails.Location == nil || fails.Location.Path != src || fails.Location.Line != 5 || fails.Location.Column != 30 {
		t.Errorf("fails location: %+v", fails.Location)
	}
	if deep := nodes["suite one › inner › deep"]; deep.Status != Passed || deep.Test != "suite one inner deep" {
		t.Errorf("deep: %+v", deep)
	}
	if inner := nodes["suite one › inner"]; inner.Kind != KindSuite || inner.Status != Passed {
		t.Errorf("inner: %+v", inner)
	}
	if suite := nodes["suite one"]; suite.Status != Failed || suite.Kind != KindSuite {
		t.Errorf("suite: %+v", suite)
	}
	if skip := nodes["top skip"]; skip.Status != Skipped || skip.Output != "SKIP not now" {
		t.Errorf("skip: %+v", skip)
	}
	if todo := nodes["top # todo"]; todo.Status != Skipped {
		t.Errorf("todo (escaped name): %+v", todo)
	}
	if dup := nodes["top skip (2)"]; dup.Status != Passed {
		t.Errorf("same name from another file: %+v", dup)
	}
	if n := nodes["never finished"]; n.Status != Failed {
		t.Errorf("unfinished: %+v", n)
	}
	for _, want := range []string{"hello out\n", "✓\x1b[0m passes", "✗ fails", "1 !== 2", "↷", "tests 7"} {
		if !strings.Contains(console, want) {
			t.Errorf("console lacks %q:\n%s", want, console)
		}
	}
	for _, unwanted := range []string{"TAP version", "# Subtest", "duration_ms", "1..3"} {
		if strings.Contains(console, unwanted) {
			t.Errorf("console shows raw TAP %q:\n%s", unwanted, console)
		}
	}
}
