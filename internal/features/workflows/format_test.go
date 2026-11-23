package workflows

import (
	"flint/internal/agentdefs"
	"strings"
	"testing"
)

const designExample = `---
description: Plan, implement, test, review, then wait for approval
---

## Plan
agent: planner

Write the plan to PLAN.md. Do not change code.

## Implement

Implement PLAN.md. Commit your work.

## Test
type: script
run: test
on-fail: Implement

## Review
type: review
on-fail: Implement

## Approve
type: gate

Check the diff and the test run.
`

func TestParseDesignExample(t *testing.T) {
	w, err := Parse("full", []byte(designExample))
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "full" || w.Description != "Plan, implement, test, review, then wait for approval" {
		t.Errorf("header = %q, %q", w.Name, w.Description)
	}
	want := []Step{
		{ID: "plan", Title: "Plan", Kind: KindAgent, Agent: "planner", Text: "Write the plan to PLAN.md. Do not change code."},
		{ID: "implement", Title: "Implement", Kind: KindAgent, Text: "Implement PLAN.md. Commit your work."},
		{ID: "test", Title: "Test", Kind: KindScript, Run: "test", OnFail: "implement"},
		{ID: "review", Title: "Review", Kind: KindReview, OnFail: "implement"},
		{ID: "approve", Title: "Approve", Kind: KindGate, Text: "Check the diff and the test run."},
	}
	if len(w.Steps) != len(want) {
		t.Fatalf("%d steps, want %d: %+v", len(w.Steps), len(want), w.Steps)
	}
	for i := range want {
		if w.Steps[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, w.Steps[i], want[i])
		}
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		check      func(t *testing.T, w Workflow)
	}{
		{"default kind is agent", "## A\n\ndo it\n", func(t *testing.T, w Workflow) {
			if w.Steps[0].Kind != KindAgent {
				t.Errorf("kind = %q", w.Steps[0].Kind)
			}
		}},
		{"text right under the heading", "## A\nWrite the plan: carefully\n", func(t *testing.T, w Workflow) {
			// "Write the plan" is not a key shape: the line is text.
			if w.Steps[0].Text != "Write the plan: carefully" {
				t.Errorf("text = %q", w.Steps[0].Text)
			}
		}},
		{"key block ends at the first empty line", "## A\nagent: claude\n\nrun: not a key\n", func(t *testing.T, w Workflow) {
			s := w.Steps[0]
			if s.Agent != "claude" || s.Text != "run: not a key" {
				t.Errorf("step = %+v", s)
			}
		}},
		{"keys are case-insensitive, type too", "## A\n\nx\n\n## B\nType: GATE\n", func(t *testing.T, w Workflow) {
			if w.Steps[1].Kind != KindGate {
				t.Errorf("kind = %q", w.Steps[1].Kind)
			}
		}},
		{"## in a code fence is text", "## A\n\n```md\n## Not a step\n```\n\n## B\n\ntwo\n", func(t *testing.T, w Workflow) {
			if len(w.Steps) != 2 || !strings.Contains(w.Steps[0].Text, "## Not a step") {
				t.Errorf("steps = %+v", w.Steps)
			}
		}},
		{"text before the first step is a note", "Some notes.\n\n## A\n\ndo\n", func(t *testing.T, w Workflow) {
			if len(w.Steps) != 1 || w.Steps[0].Text != "do" {
				t.Errorf("steps = %+v", w.Steps)
			}
		}},
		{"on-fail in any case is stored as a slug", "## Fix Bug\n\nx\n\n## Check\ntype: review\non-fail: FIX bug\n", func(t *testing.T, w Workflow) {
			if w.Steps[1].OnFail != "fix-bug" {
				t.Errorf("on-fail = %q", w.Steps[1].OnFail)
			}
		}},
		{"workflow step", "## A\n\nx\n\n## Sub\ntype: workflow\nworkflow: other\n", func(t *testing.T, w Workflow) {
			if s := w.Steps[1]; s.Kind != KindWorkflow || s.Workflow != "other" {
				t.Errorf("step = %+v", s)
			}
		}},
		{"CRLF", "## A\r\n\r\ndo it\r\n", func(t *testing.T, w Workflow) {
			if w.Steps[0].Text != "do it" {
				t.Errorf("text = %q", w.Steps[0].Text)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := Parse("w", []byte(tc.file))
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, w)
		})
	}
}

func TestParseErrors(t *testing.T) {
	long := strings.Repeat("x", MaxText+1)
	var many strings.Builder
	for i := range MaxSteps + 1 {
		many.WriteString("## S" + strings.Repeat("a", i) + "\n\ntext\n\n")
	}
	for _, tc := range []struct {
		name, wname, file, want string
	}{
		{"bad file name", "Bad Name", "## A\n\nx\n", "file name"},
		{"not utf-8", "w", "## A\n\n\xff\n", "UTF-8"},
		{"no steps", "w", "just text\n", "no steps"},
		{"unknown key", "w", "## A\nmodel: x\n\ndo\n", `unknown key "model"`},
		{"unknown type", "w", "## A\ntype: magic\n", "magic"},
		{"duplicate titles", "w", "## Go\n\na\n\n## go\n\nb\n", "two steps are named"},
		{"duplicate by slug", "w", "## Fix it\n\na\n\n## Fix-it\n\nb\n", "two steps are named"},
		{"empty title", "w", "## \n\ndo\n", "no title"},
		{"title without letters", "w", "## ---\n\ndo\n", "letter or a digit"},
		{"title too long", "w", "## " + strings.Repeat("a", MaxTitle+1) + "\n\ndo\n", "title is longer"},
		{"text too long", "w", "## A\n\n" + long + "\n", "text is longer"},
		{"description too long", "w", "---\ndescription: " + strings.Repeat("d", MaxDescription+1) + "\n---\n## A\n\nx\n", "description"},
		{"too many steps", "w", many.String(), "more than"},
		{"agent name", "w", "## A\nagent: Not Valid\n\nx\n", "not an agent's name"},
		{"empty run", "w", "## A\ntype: script\nrun:\n", "run needs"},
		{"long run", "w", "## A\ntype: script\nrun: " + strings.Repeat("r", MaxRunName+1) + "\n", "run needs"},
		{"empty on-fail", "w", "## A\n\nx\n\n## B\ntype: review\non-fail:\n", "on-fail needs"},
		{"bad workflow name", "w", "## A\ntype: workflow\nworkflow: No Way\n", "not a workflow's name"},
		{"agent step without text", "w", "## A\ntype: agent\n", "needs text"},
		{"agent step with run", "w", "## A\nrun: x\n\ndo\n", "no run"},
		{"agent step with on-fail", "w", "## A\n\nx\n\n## B\non-fail: A\n\ndo\n", "no on-fail"},
		{"agent step with workflow", "w", "## A\nworkflow: x\n\ndo\n", "no workflow"},
		{"script without run", "w", "## A\n\nx\n\n## B\ntype: script\n", "needs run"},
		{"script with agent", "w", "## A\n\nx\n\n## B\ntype: script\nrun: t\nagent: claude\n", "no agent"},
		{"script with workflow", "w", "## A\n\nx\n\n## B\ntype: script\nrun: t\nworkflow: x\n", "no workflow"},
		{"review with run", "w", "## A\n\nx\n\n## B\ntype: review\nrun: t\n", "no run"},
		{"gate with agent", "w", "## A\n\nx\n\n## B\ntype: gate\nagent: claude\n", "no agent"},
		{"gate with run", "w", "## A\n\nx\n\n## B\ntype: gate\nrun: t\n", "no run"},
		{"gate with on-fail", "w", "## A\n\nx\n\n## B\ntype: gate\non-fail: A\n", "no on-fail"},
		{"workflow step without workflow", "w", "## A\ntype: workflow\n", "needs workflow"},
		{"workflow step with text", "w", "## A\ntype: workflow\nworkflow: x\n\ntext\n", "no text"},
		{"workflow step with agent", "w", "## A\ntype: workflow\nworkflow: x\nagent: claude\n", "no agent"},
		{"on-fail unknown step", "w", "## A\n\nx\n\n## B\ntype: review\non-fail: Nope\n", "names no step"},
		{"on-fail later step", "w", "## A\ntype: review\non-fail: B\n\n## B\n\nx\n", "earlier"},
		{"on-fail itself", "w", "## A\n\nx\n\n## B\ntype: review\non-fail: B\n", "earlier"},
		{"on-fail not an agent step", "w", "## A\ntype: gate\n\n## B\ntype: review\non-fail: A\n", "agent step"},
		{"zero-width in text", "w", "## A\n\nhi​there\n", "invisible"},
		{"bidi in text", "w", "## A\n\nhi‮there\n", "invisible"},
		{"tag character", "w", "## A\n\nhi\U000E0041\n", "invisible"},
		{"control character", "w", "## A\n\nhi\x07\n", "control character"},
		{"line separator", "w", "## A\n\nhi there\n", "separator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.wname, []byte(tc.file))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseLimitsAtTheEdge(t *testing.T) {
	var b strings.Builder
	for i := range MaxSteps {
		b.WriteString("## Step " + strings.Repeat("a", i) + "\n\ntext\n\n")
	}
	w, err := Parse("w", []byte(b.String()))
	if err != nil || len(w.Steps) != MaxSteps {
		t.Fatalf("%d steps, err = %v", len(w.Steps), err)
	}
	text := strings.Repeat("é", MaxText) // characters, not bytes
	if _, err := Parse("w", []byte("## A\n\n"+text+"\n")); err != nil {
		t.Errorf("MaxText characters: %v", err)
	}
}

// set builds a workflow list from file texts, failing the test on a parse error.
func set(t *testing.T, files map[string]string) []Workflow {
	t.Helper()
	var list []Workflow
	for name, text := range files {
		w, err := Parse(name, []byte(text))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		list = append(list, w)
	}
	return list
}

func TestResolve(t *testing.T) {
	list := set(t, map[string]string{
		"main":   "## Start\n\nbegin\n\n## Sub\ntype: workflow\nworkflow: inner\n\n## End\ntype: gate\n",
		"inner":  "## Code\n\ncode\n\n## Check\ntype: review\non-fail: Code\n",
		"a":      "## X\ntype: workflow\nworkflow: b\n",
		"b":      "## Y\ntype: workflow\nworkflow: a\n",
		"self":   "## S\ntype: workflow\nworkflow: self\n",
		"d1":     "## S\ntype: workflow\nworkflow: d2\n",
		"d2":     "## S\ntype: workflow\nworkflow: d3\n",
		"d3":     "## S\ntype: workflow\nworkflow: d4\n",
		"d4":     "## Do\n\nwork\n",
		"d3ok":   "## S\ntype: workflow\nworkflow: d4\n",
		"d2ok":   "## S\ntype: workflow\nworkflow: d3ok\n",
		"lost":   "## S\ntype: workflow\nworkflow: missing\n",
		"gate1":  "## G\ntype: gate\n\n## A\n\nx\n",
		"clash":  "## Sub\ntype: workflow\nworkflow: d4\n\n## Sub Do\n\nx\n",
		"script": "## A\n\nx\n\n## T\ntype: script\nrun: t\n",
	})
	find := Find(list)
	get := func(n string) Workflow { w, _ := find(n); return w }

	t.Run("sub-workflow steps are inlined with prefixes", func(t *testing.T) {
		steps, err := Resolve(get("main"), find)
		if err != nil {
			t.Fatal(err)
		}
		var ids, titles []string
		for _, s := range steps {
			ids = append(ids, s.ID)
			titles = append(titles, s.Title)
		}
		if got, want := strings.Join(ids, ","), "start,sub-code,sub-check,end"; got != want {
			t.Errorf("ids = %s, want %s", got, want)
		}
		if got, want := titles[1]+"|"+titles[2], "Sub ▸ Code|Sub ▸ Check"; got != want {
			t.Errorf("titles = %s, want %s", got, want)
		}
		if steps[2].OnFail != "sub-code" {
			t.Errorf("on-fail = %q", steps[2].OnFail)
		}
		if steps[2].Kind != KindReview || steps[3].Kind != KindGate {
			t.Errorf("kinds changed: %+v", steps)
		}
	})
	t.Run("a workflow with no sub-workflows resolves to itself", func(t *testing.T) {
		steps, err := Resolve(get("script"), find)
		if err != nil || len(steps) != 2 {
			t.Errorf("steps = %+v, err = %v", steps, err)
		}
	})
	t.Run("Resolve leaves the workflow alone", func(t *testing.T) {
		w := get("main")
		before := w.Steps[1]
		if _, err := Resolve(w, find); err != nil {
			t.Fatal(err)
		}
		if w.Steps[1] != before || len(w.Steps) != 3 {
			t.Errorf("workflow changed: %+v", w.Steps)
		}
	})
	for _, tc := range []struct{ name, workflow, want string }{
		{"cycle of two", "a", "cycle"},
		{"cycle with itself", "self", "cycle"},
		{"nested too deep", "d1", "nested more than"},
		{"unknown workflow", "lost", `no workflow named "missing"`},
		{"first step must be an agent step", "gate1", "first step must be an agent step"},
		{"duplicate ids after inlining", "clash", "two steps are named"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(get(tc.workflow), find)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	t.Run("depth exactly MaxDepth is fine", func(t *testing.T) {
		if _, err := Resolve(get("d2ok"), find); err != nil {
			t.Errorf("err = %v", err)
		}
	})
}

func TestResolveTooManySteps(t *testing.T) {
	// "inner" has 20 steps; "wide" names it twice: 40 > MaxSteps.
	var b strings.Builder
	for i := range 20 {
		b.WriteString("## S" + strings.Repeat("a", i) + "\n\nx\n\n")
	}
	inner, err := Parse("inner", []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	wide, err := Parse("wide", []byte("## One\ntype: workflow\nworkflow: inner\n\n## Two\ntype: workflow\nworkflow: inner\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Resolve(wide, Find([]Workflow{inner, wide}))
	if err == nil || !strings.Contains(err.Error(), "more than 30 steps") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveEmptySubWorkflow(t *testing.T) {
	empty := Workflow{Name: "empty"}
	w := Workflow{Name: "w", Steps: []Step{{ID: "s", Title: "S", Kind: KindWorkflow, Workflow: "empty"}}}
	_, err := Resolve(w, Find([]Workflow{empty, w}))
	if err == nil || !strings.Contains(err.Error(), "no steps") {
		t.Errorf("err = %v", err)
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"feature":               true,
		"bug-fix":               true,
		"2fast":                 true,
		"a":                     true,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
		"":                      false,
		"Feature":               false,
		"bug_fix":               false,
		"-bug":                  false,
		"bug fix":               false,
		"bug.fix":               false,
		"../x":                  false,
		"bug-fix\n":             false,
		"żółw":                  false,
	} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

// One rule for names, in the words its messages use: workflow files,
// the workflow key of a step, schedules.
func TestNameRuleEverywhere(t *testing.T) {
	const wantMsg = "the file name must be lowercase letters, digits and dashes"
	for _, name := range []string{"Upper", "under_score", "has space"} {
		if _, err := Parse(name, []byte("## A\n\ndo it\n")); err == nil || err.Error() != wantMsg {
			t.Errorf("Parse(%q) = %v", name, err)
		}
	}
	if _, err := Parse("ok-name", []byte("## A\n\ndo it\n\n## B\ntype: workflow\nworkflow: Other_One\n")); err == nil || !strings.Contains(err.Error(), "is not a workflow's name") {
		t.Errorf("a workflow step naming Other_One: %v", err)
	}
	valid := Schedule{Name: "nightly", Workflow: "feature", Folder: "/abs/repo", Task: "Update.", At: "02:30"}
	for _, tc := range []struct {
		name   string
		change func(s *Schedule)
		err    string
	}{
		{"upper case name", func(s *Schedule) { s.Name = "Nightly" }, "name must be lowercase letters, digits and dashes"},
		{"underscore name", func(s *Schedule) { s.Name = "night_ly" }, "name must be lowercase letters, digits and dashes"},
		{"upper case workflow", func(s *Schedule) { s.Workflow = "Feature" }, "(lowercase letters, digits and dashes)"},
		{"underscore workflow", func(s *Schedule) { s.Workflow = "bug_fix" }, "(lowercase letters, digits and dashes)"},
		{"valid", func(s *Schedule) {}, ""},
	} {
		s := valid
		tc.change(&s)
		err := checkSchedules([]Schedule{s})
		if (tc.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.err)
		}
	}
}

func TestAgentNameIsTheDefinedAgentsRule(t *testing.T) {
	for name, want := range map[string]bool{
		"claude": true, "codex": true, "my-agent2": true, strings.Repeat("a", 32): true,
		strings.Repeat("a", 33): false, "": false, "2agent": false, "Claude": false, "a_b": false, "a b": false,
	} {
		if got := agentdefs.ValidName(name); got != want {
			t.Errorf("agent %q = %v, want %v", name, got, want)
		}
	}
}
