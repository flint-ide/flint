package testrun

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExtensionRunnersSkipBuiltinLanguages(t *testing.T) {
	SetExtensionRunners([]ExtensionRunner{
		{ID: "a", Languages: []string{"go", "typescript", "ruby"}},
		{ID: "b", Languages: []string{"ruby", "python"}},
	})
	t.Cleanup(func() { SetExtensionRunners(nil) })
	for lang, want := range map[string]string{"ruby": "a", "python": "b", "go": "", "typescript": ""} {
		r, ok := ExtensionRunnerFor(lang)
		if ok != (want != "") || r.ID != want {
			t.Errorf("ExtensionRunnerFor(%s) = %q, %v; want %q", lang, r.ID, ok, want)
		}
	}
}

// TestPlanExtensionRunsTAP plans an extension runner whose program prints
// TAP, runs the plan's step and parses its output.
func TestPlanExtensionRunsTAP(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "tap.sh")
	tap := "#!/bin/sh\necho \"# args: $*\"\necho 'TAP version 13'\necho 'ok 1 - adds'\necho 'not ok 2 - subtracts'\necho '1..2'\nexit 1\n"
	if err := os.WriteFile(script, []byte(tap), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "spec", "calc_spec.rb")
	r := ExtensionRunner{ID: "fake", Label: "Fake", Languages: []string{"ruby"}, Format: FormatTAP,
		Command: []string{script, "--root=${workspaceFolder}", "${file}", "${tests}"}}

	plan, err := PlanExtension(Request{Lang: LangExt, Dirs: []string{root}, File: file, Tests: []string{"adds", "sub tracts"}}, r)
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Steps[0]
	if want := []string{"--root=" + root, file, "adds", "sub tracts"}; !slices.Equal(step.Args, want) {
		t.Errorf("args = %q, want %q", step.Args, want)
	}
	if plan.Cwd != root || plan.Format != FormatTAP || plan.Title != "2 tests" {
		t.Errorf("plan = %+v", plan)
	}

	cmd := exec.Command(step.Program, step.Args...)
	cmd.Dir = plan.Cwd
	out, _ := cmd.Output()
	p := plan.NewParser()
	status := map[string]Status{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		_, nodes := p.Line(append(sc.Bytes(), '\n'))
		for _, n := range nodes {
			status[n.Name] = n.Status
		}
	}
	for _, n := range p.Finish(1, false) {
		status[n.Name] = n.Status
	}
	if status["adds"] != Passed || status["subtracts"] != Failed {
		t.Errorf("results = %v (output %q)", status, out)
	}
}

func TestPlanExtensionMessages(t *testing.T) {
	root := t.TempDir()
	r := ExtensionRunner{ID: "rspec", Label: "RSpec", Command: []string{"flint-no-such-runner", "${file}"}}
	_, err := PlanExtension(Request{Lang: LangExt, Dirs: []string{root}, File: filepath.Join(root, "a_spec.rb")}, r)
	want := `Can't run the tests of a_spec.rb: RSpec needs the program "flint-no-such-runner", which isn't installed or isn't on your PATH. Install it, then try again.`
	if err == nil || err.Error() != want {
		t.Errorf("missing program: %v", err)
	}
	_, err = PlanExtension(Request{Lang: LangExt, Dirs: []string{root}, File: "/elsewhere/a_spec.rb"}, r)
	if err == nil || !strings.Contains(err.Error(), "is not inside the project folder") {
		t.Errorf("outside the project: %v", err)
	}
}
