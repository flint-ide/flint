package workflows

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/feature"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// What the user approves for start_workflow says what each script step
// runs and whether the sandbox confines it.
func TestStartSummaryShowsWhatScriptsRun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sandbox string
		support bool
		want    string
	}{
		{"off", SandboxOff, true, "They run WITHOUT a sandbox"},
		{"on", SandboxOn, true, "They run in the sandbox"},
		{"unavailable", SandboxOn, false, "The sandbox is on but this system has none, so they will fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.allowTools(true)
			writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
			f.settings(base(func(s *Settings) { s.Sandbox = tc.sandbox }))
			f.allowTools(true)
			f.h.SetScriptSandbox(feature.SandboxSupport{Available: tc.support})
			f.allowUnsandboxed() // an agent is never the first (unsandboxed_test.go)
			f.h.SetRunLaunch(f.root, "test", feature.RunLaunch{Name: "test", Cwd: ".", Commands: []feature.RunCommand{{Line: "npm run test", Program: "npm", Args: []string{"run", "test"}}}})
			ctx := mcpserver.WithCaller(t.Context(), "chat-1")
			p, err := f.tool(mcptools.ToolWorkflowStart).Plan(ctx, f.root, json.RawMessage(`{"workflow":"check","task":"Fix it"}`))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"run on the agent's code", `- Test: run configuration "test": npm run test`, tc.want} {
				if !strings.Contains(p.Summary, want) {
					t.Errorf("summary lacks %q:\n%s", want, p.Summary)
				}
			}
			l, err := f.api.List(f.root)
			if err != nil {
				t.Fatal(err)
			}
			wantState := tc.sandbox
			if !tc.support {
				wantState = SandboxUnavailable
			}
			if l.Sandbox != wantState {
				t.Errorf("Listing.Sandbox %q, want %q", l.Sandbox, wantState)
			}
			for _, w := range l.Workflows {
				for _, st := range w.Steps {
					if w.Name == "check" && st.Kind == "script" && (st.Command != "npm run test" || st.CommandProblem != "") {
						t.Errorf("script step %+v", st)
					}
					if st.Kind != "script" && st.Command != "" {
						t.Errorf("a %s step has a command", st.Kind)
					}
				}
			}
		})
	}
}

// A run configuration the checkout does not have is a problem of the step,
// not a failure of the list.
func TestListScriptWithoutConfiguration(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	l, err := f.api.List(f.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range l.Workflows {
		for _, st := range w.Steps {
			if w.Name == "check" && st.Kind == "script" && (st.Command != "" || !strings.Contains(st.CommandProblem, `no run configuration named "test"`)) {
				t.Errorf("script step %+v", st)
			}
		}
	}
}
