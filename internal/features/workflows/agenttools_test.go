package workflows

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

func (f *fixture) allowTools(on bool) {
	s := f.h.Settings()
	s.MCPServer.Enabled = true
	s.MCPServer.Actions.Workflows = on
	f.h.SetSettings(s)
}

func (f *fixture) tool(name string) mcpserver.Tool {
	f.t.Helper()
	for _, tl := range f.api.agentTools() {
		if tl.Name == name {
			return tl
		}
	}
	f.t.Fatalf("no tool %s", name)
	return mcpserver.Tool{}
}

// call makes one call of a tool as chat in root, the way Flint's MCP server
// does: an acting tool's Plan, then (the user said yes) its Run.
func (f *fixture) call(chat, root, name, args string) (string, error) {
	f.t.Helper()
	ctx := mcpserver.WithCaller(f.t.Context(), chat)
	tl := f.tool(name)
	if !tl.Acts {
		return tl.Call(ctx, root, json.RawMessage(args))
	}
	p, err := tl.Plan(ctx, root, json.RawMessage(args))
	if err != nil {
		return "", err
	}
	return p.Run(ctx)
}

func names(tools []mcpserver.Tool) []string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func TestAgentToolsAreOptIn(t *testing.T) {
	f := newFixture(t)
	if got := f.api.agentTools(); len(got) != 0 {
		t.Fatalf("offered by default: %v", names(got))
	}
	f.allowTools(true)
	want := []string{mcptools.ToolWorkflowList, mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowStart, mcptools.ToolWorkflowSteer}
	tools := f.api.agentTools()
	if got := names(tools); !slices.Equal(got, want) {
		t.Fatalf("offered %v", got)
	}
	for _, tl := range tools {
		var schema map[string]any
		if err := json.Unmarshal(tl.InputSchema, &schema); err != nil || schema["additionalProperties"] != false {
			t.Errorf("%s: schema %s", tl.Name, tl.InputSchema)
		}
		acts := tl.Name == mcptools.ToolWorkflowStart || tl.Name == mcptools.ToolWorkflowSteer
		if tl.Acts != acts || acts && tl.Plan == nil || !acts && tl.Call == nil || tl.Granted != nil {
			t.Errorf("%s: Acts %v", tl.Name, tl.Acts)
		}
		if !slices.Contains(mcptools.ActionNames, tl.Name) {
			t.Errorf("%s is not in mcptools.ActionNames", tl.Name)
		}
	}
	s := f.h.Settings()
	s.MCPServer.Enabled = false
	f.h.SetSettings(s)
	if got := f.api.agentTools(); len(got) != 0 {
		t.Fatalf("offered with the server off: %v", names(got))
	}
}

func TestAgentToolsRefuseUnknownArgumentsAndUntrustedFolders(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	for _, name := range []string{mcptools.ToolWorkflowList, mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowStart, mcptools.ToolWorkflowSteer} {
		if _, err := f.call("chat-1", f.root, name, `{"run":"0000000000000000","workflow":"feature","task":"x","action":"pause","sudo":true}`); err == nil ||
			!strings.Contains(err.Error(), "invalid arguments") {
			t.Errorf("%s with an unknown argument: %v", name, err)
		}
	}
	f.h.SetUntrusted(f.root)
	if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowList, `{}`); err == nil || !strings.Contains(err.Error(), "Restricted Mode") {
		t.Errorf("an untrusted folder: %v", err)
	}
}

func TestAgentListsWorkflows(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	out, err := f.call("chat-1", f.root, mcptools.ToolWorkflowList, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## feature (builtin)", "## check (project)", "(approval: the user approves)", "(script: test)"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
}

func TestAgentStartsARunWithTheUsersYes(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	task := strings.Repeat("Add a flag. ", 100)
	ctx := mcpserver.WithCaller(t.Context(), "chat-1")
	p, err := f.tool(mcptools.ToolWorkflowStart).Plan(ctx, f.root, json.RawMessage(`{"workflow":"feature","task":`+quote(task)+`,"agent":"codex"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Start the workflow "feature" in ` + f.root, "1. Plan (agent)", "(approval: the user approves)", "Agent: codex", "Approval steps wait for you", "Task:\nAdd a flag.", " […]"} {
		if !strings.Contains(p.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, p.Summary)
		}
	}
	if len(f.h.Requested()) != 0 || len(f.api.Runs()) != 0 {
		t.Fatal("Plan started something")
	}
	out, err := p.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runs := f.api.Runs()
	if len(runs) != 1 || !strings.Contains(out, runs[0].ID) || runs[0].Agent != "codex" || runs[0].Dir != f.root {
		t.Fatalf("started %q: %+v", out, runs)
	}
	r, _ := f.e.get(runs[0].ID)
	if r.Origin != OriginAgent || r.Log[0].Text != "Run started by an agent in "+f.root+" (chat chat-1), approved by you" {
		t.Errorf("origin %q, log %+v", r.Origin, r.Log)
	}
	if req := f.h.Requested(); req[0][0].Dir != f.root || req[0][0].Origin != originPrefix+r.ID {
		t.Errorf("requested %+v", req)
	}
	// The same checks as Run Workflow….
	for args, want := range map[string]string{
		`{"workflow":"nope","task":"x"}`:                   `there is no workflow named "nope" here`,
		`{"workflow":"feature","task":" "}`:                "describe the task",
		`{"workflow":"feature"}`:                           "task is required",
		`{"workflow":"feature","task":"x","agent":"nope"}`: `the agent "nope" is not available`,
	} {
		if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowStart, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", args, err, want)
		}
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestOneChatStartsAtMostTenRuns(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	for i := range maxStartsPerChat {
		if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowStart, `{"workflow":"feature","task":"run `+strconv(i)+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowStart, `{"workflow":"feature","task":"one more"}`); err == nil ||
		!strings.Contains(err.Error(), "this chat started 10 workflow runs") {
		t.Fatalf("the 11th start: %v", err)
	}
	if _, err := f.call("chat-2", f.root, mcptools.ToolWorkflowStart, `{"workflow":"feature","task":"another chat"}`); err != nil {
		t.Errorf("another chat: %v", err)
	}
	// A refused start is not counted.
	if _, err := f.call("chat-3", f.root, mcptools.ToolWorkflowStart, `{"workflow":"nope","task":"x"}`); err == nil || f.api.started["chat-3"] != 0 {
		t.Errorf("a refused start counted: %v, %d", err, f.api.started["chat-3"])
	}
}

func TestAWorkflowStepCannotStartOrSteerWorkflows(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	v := f.start("feature")
	worktree := f.h.AllAgentTasks()[v.Task].Worktree
	// The task's own chat (its origin is the run's), a later step's chat, and
	// any chat in the run's worktree (a project item's run has the
	// project's origin) are steps.
	f.h.SetChat("task-chat", v.Task)
	f.finishFirst(v.ID, "RESULT: DONE")
	step := f.chatOf(v.ID, 1)
	for _, c := range []struct{ chat, root string }{{"task-chat", f.root}, {step, f.root}, {"helper", worktree}} {
		for _, name := range []string{mcptools.ToolWorkflowStart, mcptools.ToolWorkflowSteer, mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowList} {
			f.h.SetTrusted(worktree)
			_, err := f.call(c.chat, c.root, name, argsFor(name, v.ID))
			if err == nil || err.Error() != "a workflow step cannot start or steer workflows" {
				t.Errorf("%s from %s in %s: %v", name, c.chat, c.root, err)
			}
		}
	}
	if r, _ := f.e.get(v.ID); r.Paused || len(f.api.Runs()) != 1 {
		t.Errorf("a step steered or started: %+v", r)
	}
}

// argsFor is a valid call of a tool on run id.
func argsFor(name, id string) string {
	switch name {
	case mcptools.ToolWorkflowStart:
		return `{"workflow":"feature","task":"again"}`
	case mcptools.ToolWorkflowSteer:
		return `{"run":"` + id + `","action":"pause"}`
	case mcptools.ToolWorkflowRuns:
		return `{"run":"` + id + `"}`
	case mcptools.ToolWorkflowWait:
		return `{"run":"` + id + `","timeoutSeconds":1}`
	}
	return `{}`
}

func TestAgentsReachOnlyTheRunsOfTheirFolder(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	other := filepath.Join(filepath.Dir(f.root), "other")
	writeFile(t, filepath.Join(other, "x.txt"), "x")
	f.h.SetRoots(f.root, other)
	f.h.SetRepository(other)
	f.h.SetTrusted(other)
	mine := f.start("feature")
	theirs, err := f.api.Start(StartRequest{Dir: other, Workflow: "feature", Description: "Their run token ghp_abcdefghijklmnopqrstuvwxyz0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.call("chat-1", f.root, mcptools.ToolWorkflowRuns, `{}`)
	if err != nil || !strings.Contains(out, "## Run "+mine.ID) || strings.Contains(out, theirs.ID) {
		t.Fatalf("runs of the first folder: %v\n%s", err, out)
	}
	for _, name := range []string{mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowSteer} {
		if _, err := f.call("chat-1", f.root, name, argsFor(name, theirs.ID)); err == nil || !strings.Contains(err.Error(), "no workflow run") {
			t.Errorf("%s reached another folder's run: %v", name, err)
		}
	}
	out, err = f.call("chat-2", other, mcptools.ToolWorkflowRuns, `{}`)
	if err != nil || !strings.Contains(out, theirs.ID) || strings.Contains(out, "ghp_abc") || !strings.Contains(out, "Run started") {
		t.Errorf("runs of the other folder (redacted): %v\n%s", err, out)
	}
}

func TestAgentSteersButNeverApprovesOrSkips(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	v := f.start("feature")
	steer := func(args string) (string, error) { return f.call("chat-1", f.root, mcptools.ToolWorkflowSteer, args) }
	if _, err := steer(`{"run":"` + v.ID + `","action":"pause"}`); err != nil {
		t.Fatal(err)
	}
	r, _ := f.e.get(v.ID)
	if !r.Paused || !strings.HasPrefix(r.Log[len(r.Log)-1].Text, "Asked by an agent in "+f.root+" (chat chat-1), approved by you") {
		t.Fatalf("after pause: %+v", r)
	}
	if _, err := steer(`{"run":"` + v.ID + `","action":"resume"}`); err != nil {
		t.Fatal(err)
	}
	for args, want := range map[string]string{
		`{"run":"` + v.ID + `","action":"approve"}`:                    "only the user can approve",
		`{"run":"` + v.ID + `","action":"skip"}`:                       "only the user can approve",
		`{"run":"` + v.ID + `","action":"Continue anyway"}`:            "only the user can approve",
		`{"run":"` + v.ID + `","action":"remove"}`:                     "action must be one of pause, resume, retry, send_back, stop",
		`{"run":"` + v.ID + `","action":"retry"}`:                      "the run has not failed",
		`{"run":"` + v.ID + `","action":"send_back"}`:                  "send_back needs step",
		`{"run":"` + v.ID + `","action":"pause","note":"x"}`:           "step and note go only with send_back",
		`{"run":"zzz","action":"pause"}`:                               "no workflow run",
		`{"run":"` + v.ID + `","action":"send_back","step":"Approve"}`: "a run can be sent back only while it waits",
	} {
		if _, err := steer(args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", args, err, want)
		}
	}
	p, err := f.tool(mcptools.ToolWorkflowSteer).Plan(mcpserver.WithCaller(t.Context(), "chat-1"), f.root, json.RawMessage(`{"run":"`+v.ID+`","action":"stop"}`))
	if err != nil || !strings.Contains(p.Summary, "Stop the workflow run") || !strings.Contains(p.Summary, v.ID) {
		t.Fatalf("stop's summary %q, %v", p.Summary, err)
	}
	if _, err := p.Run(mcpserver.WithCaller(t.Context(), "chat-1")); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.e.get(v.ID); r.State != StateStopped || !strings.Contains(r.Log[len(r.Log)-1].Text, "Stopped: Asked by an agent") {
		t.Errorf("after stop: %s %+v", r.State, r.Log)
	}
}

// Nothing in the agent tools' files names Run.Approve, Run.Skip, API.Approve,
// API.Skip or their actions: no tool has a path to them.
func TestNoToolReachesApproveOrSkip(t *testing.T) {
	// The workflow tools, and every agent tool of Projects (get_project,
	// add_project_item, delegate_project_item and the Delegate it runs):
	// no tool of either feature has a path to an approval, a "Continue
	// anyway" or a "Check again". The hooks between the features too.
	for _, file := range []string{"agenttools.go", "agentsteer.go", "coordinate.go", "taskrun.go",
		"../projects/tool.go", "../projects/additem.go", "../projects/delegatetool.go", "../projects/delegate.go", "../../feature/feature.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		node, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && slices.Contains([]string{"Approve", "Skip", "Recheck", "Rechecked", "ActionApprove", "ActionSkip", "ActionRecheck"}, id.Name) {
				t.Errorf("%s names %s", file, id.Name)
			}
			return true
		})
	}

	// And by behaviour: whatever a tool is called with, a run at its gate
	// stays there and a failed run stays failed.
	f := newFixture(t)
	f.allowTools(true)
	writeFile(t, filepath.Join(ProjectDir(f.root), "gated.md"), "## Do\n\nDo it.\n\n## Approve\ntype: gate\n")
	gated := f.start("gated")
	f.finishFirst(gated.ID, "RESULT: DONE")
	f.waitFor(gated.ID, "the gate", func(r Run) bool { return r.State == StateWaiting })
	failed := f.start("gated")
	f.finishFirst(failed.ID, "RESULT: BLOCKED")
	f.waitFor(failed.ID, "the failure", func(r Run) bool { return r.State == StateFailed })
	// Its chat now ends with a pass: a "Check again" would pass the step,
	// which no tool may do either.
	f.h.SetChatOutput("task-chat-"+failed.ID, "RESULT: DONE")
	actions := []string{"approve", "Approve", "APPROVE", "skip", "continue", "continue_anyway", "continueAnyway", "pass", "accept", "approved", "sendBack", "done", "", "resume",
		"recheck", "check", "check_again", "checkAgain", "Recheck"}
	for _, id := range []string{gated.ID, failed.ID} {
		for _, tl := range f.api.agentTools() {
			for _, act := range actions {
				for _, extra := range []string{"", `,"step":"Approve"`, `,"approve":true`} {
					args := `{"run":"` + id + `","action":"` + act + `"` + extra + `}`
					_, _ = f.call("chat-1", f.root, tl.Name, args)
				}
			}
		}
	}
	if r, _ := f.e.get(gated.ID); r.State != StateWaiting || r.Records[1].State == StepDone {
		t.Errorf("the gate was passed: %s %+v", r.State, r.Records)
	}
	if r, _ := f.e.get(failed.ID); r.State != StateFailed || r.Records[0].Skipped {
		t.Errorf("the failed step was skipped: %s %+v", r.State, r.Records)
	}
}

func TestWaitForWorkflowRun(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	v := f.start("feature")
	out, err := f.call("chat-1", f.root, mcptools.ToolWorkflowWait, `{"run":"`+v.ID+`","timeoutSeconds":1}`)
	if err != nil || !strings.HasPrefix(out, "timedOut: true") {
		t.Fatalf("a running run: %v\n%s", err, out)
	}
	if err := f.api.Pause(v.ID); err != nil {
		t.Fatal(err)
	}
	f.finishFirst(v.ID, "RESULT: DONE")
	out, err = f.call("chat-1", f.root, mcptools.ToolWorkflowWait, `{"run":"`+v.ID+`"}`)
	if err != nil || !strings.HasPrefix(out, "timedOut: false") || !strings.Contains(out, "state: paused") || !strings.Contains(out, "you may ask for (steer_workflow_run): send_back, resume, stop") {
		t.Fatalf("a paused run: %v\n%s", err, out)
	}
	if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowWait, `{"run":"`+v.ID+`","timeoutSeconds":0}`); err == nil {
		t.Error("timeoutSeconds 0 accepted")
	}
	// Capped by orchestration.maxWaitSeconds.
	s := f.h.Settings()
	s.Orchestration.MaxWaitSeconds = 30
	f.h.SetSettings(s)
	if tl := f.tool(mcptools.ToolWorkflowWait); tl.Timeout != 30*time.Second+waitSlack || !strings.Contains(string(tl.InputSchema), `"maximum":30`) {
		t.Errorf("wait tool %v %s", tl.Timeout, tl.InputSchema)
	}
}

func TestCoordinatePrompt(t *testing.T) {
	f := newFixture(t)
	p, err := f.api.CoordinatePrompt(f.root, "  Ship the export feature  ")
	if err != nil {
		t.Fatal(err)
	}
	if p.ToolOn {
		t.Error("toolOn with the switch off")
	}
	for _, want := range []string{"Ship the export feature", f.root, "get_project", "list_workflows", "start_workflow", "wait_for_workflow_run", "Never try to approve", "as soon as I say so", "bug-fix, feature", `"Run and steer workflows"`} {
		if !strings.Contains(p.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p.Prompt)
		}
	}
	f.allowTools(true)
	if p, _ := f.api.CoordinatePrompt(f.root, "x"); !p.ToolOn {
		t.Error("toolOn off with the switch on")
	}
	for goal, want := range map[string]string{
		" ":                            "say what the goal is",
		strings.Repeat("x", MaxGoal+1): "longer than 2000 characters",
		"do it\u202e":                  "the goal contains invisible characters",
	} {
		if _, err := f.api.CoordinatePrompt(f.root, goal); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", goal, err, want)
		}
	}
	f.h.SetUntrusted(f.root)
	if _, err := f.api.CoordinatePrompt(f.root, "x"); err == nil || !strings.Contains(err.Error(), "Restricted Mode") {
		t.Errorf("untrusted: %v", err)
	}
}
