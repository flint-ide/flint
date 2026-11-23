package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/features/projects"
	"flint/internal/features/workflows"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// delegated delegates item "rl" (api and web, workflow ship) as the
// coordinator and returns the two runs' tasks and first chats.
type delegatedPair struct {
	apiTask, webTask string
	apiChat, webChat string
}

func (f *dlgFixture) delegatePair(t *testing.T) delegatedPair {
	t.Helper()
	f.delegate(t, "rl")
	f.waitStarts()
	p := delegatedPair{apiTask: f.taskOf(t, f.repo), webTask: f.taskOf(t, f.web)}
	p.apiChat, p.webChat = f.firstChat(t, p.apiTask), f.firstChat(t, p.webTask)
	return p
}

// The item's status follows its runs through the real hooks: both runs at
// their approval step (the last step) put it in review; one approved and
// the other sent back (the user's actions in Workflows) put it back in
// doing. Nothing polls: every wait is on an emitted event.
func TestDelegatedItemStatusFollowsItsRunsThroughTheRealHooks(t *testing.T) {
	f := newDlgFixture(t, 0, dlgItems())
	p := f.delegatePair(t)
	if got := f.status(t, "rl"); got != projects.StatusDoing {
		t.Fatalf("after Delegate the item is %q", got)
	}

	f.finish(p.apiChat, "RESULT: DONE")
	f.finish(p.webChat, "RESULT: DONE")
	f.ev.until(t, "both runs at their approval step", func() bool {
		return f.runState(f.repo) == workflows.StateWaiting && f.runState(f.web) == workflows.StateWaiting
	})
	f.ev.until(t, "the item in review", func() bool { return f.status(t, "rl") == projects.StatusReview })

	// The user approves the api run; the web run is sent back.
	apiRun, _ := f.runIn(f.repo)
	webRun, _ := f.runIn(f.web)
	if err := f.wf.Approve(apiRun.ID); err != nil {
		t.Fatal(err)
	}
	f.ev.until(t, "the api run done", func() bool { return f.runState(f.repo) == workflows.StateDone })
	if got := f.status(t, "rl"); got != projects.StatusReview {
		t.Errorf("one run done, the other at its approval: the item is %q", got)
	}
	if err := f.wf.SendBack(webRun.ID, "Fix", "Handle the burst case"); err != nil {
		t.Fatal(err)
	}
	f.ev.until(t, "the web run running again", func() bool { return f.runState(f.web) == workflows.StateRunning })
	f.ev.until(t, "the item back in doing", func() bool { return f.status(t, "rl") == projects.StatusDoing })

	// The sent-back step runs in a new chat of the web task.
	// The run is running again before its step's chat is open, so wait for the chat.
	webChatsNow := func() []string {
		var ids []string
		for _, c := range f.ev.chats() {
			if c.TaskID == p.webTask {
				ids = append(ids, c.Info.ID)
			}
		}
		return ids
	}
	f.ev.until(t, "the sent-back step's chat", func() bool { return len(webChatsNow()) >= 2 })
	webChats := webChatsNow()
	if len(webChats) != 2 || webChats[0] != p.webChat {
		t.Errorf("chats of the web task: %v", webChats)
	}
	if v, _ := f.runIn(f.repo); v.State != workflows.StateDone {
		t.Errorf("the api run is %s", v.State)
	}

	// And the way back: the sent-back run reaches its approval step again
	// and the item is in review once more; approving it ends the run (the
	// item's delivery, a merge, is not this test's).
	f.finish(webChats[1], "RESULT: DONE")
	f.ev.until(t, "the web run at its approval step again", func() bool { return f.runState(f.web) == workflows.StateWaiting })
	f.ev.until(t, "the item in review again", func() bool { return f.status(t, "rl") == projects.StatusReview })
}

// A coordinator chat in the api repository reaches, through Flint's MCP
// server, the run that lives in the web repository because it belongs to
// its project's item: it lists it, pauses it (the user says yes) and waits
// for it. It reaches no run of a repository outside the project, and a
// chat that is a step of one of the runs is refused every workflow tool
// and delegate_project_item.
func TestCoordinatorSteersTheProjectsRunsAcrossRepositories(t *testing.T) {
	f := newDlgFixture(t, 0, dlgItems())
	other := f.addRepoT("other", "git@github.com:acme/other.git")
	commitT(t, other, filepath.Join(".flint", "workflows", "ship.md"), dlgShip, "add ship")
	p := f.delegatePair(t)
	apiRun, _ := f.runIn(f.repo)
	webRun, _ := f.runIn(f.web)

	// A run of a repository that is not part of the project.
	unrelated, err := f.wf.Start(workflows.StartRequest{Dir: other, Workflow: "ship", Description: "Not the project's", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	f.waitStarts()

	// Listing: both runs of the item (one lives in web), not the other.
	out, failed := f.call(t, "coord", f.repo, mcptools.ToolWorkflowRuns, `{}`)
	if failed || !strings.Contains(out, "## Run "+apiRun.ID) || !strings.Contains(out, "## Run "+webRun.ID) ||
		!strings.Contains(out, "folder: "+f.web) || strings.Contains(out, unrelated.ID) {
		t.Fatalf("get_workflow_runs (failed %v):\n%s", failed, out)
	}
	if out, failed = f.call(t, "coord", f.repo, mcptools.ToolWorkflowRuns, `{"run":"`+webRun.ID+`"}`); failed ||
		!strings.Contains(out, "folder: "+f.web) || strings.Contains(out, apiRun.ID) {
		t.Fatalf("get_workflow_runs of the web run (failed %v):\n%s", failed, out)
	}

	// Pausing it needs the user's yes, which is asked with the run named.
	f.setAnswer(false)
	asked := len(f.questions())
	if out, failed = f.call(t, "coord", f.repo, mcptools.ToolWorkflowSteer, `{"run":"`+webRun.ID+`","action":"pause"}`); !failed || !strings.Contains(out, mcpserver.ErrNotApproved.Error()) {
		t.Fatalf("pause without the user's yes: %q (failed %v)", out, failed)
	}
	if q := f.questions(); len(q) != asked+1 || !strings.Contains(q[asked], webRun.ID) || !strings.Contains(q[asked], f.web) {
		t.Errorf("the question: %q", q)
	}
	if v, _ := f.runIn(f.web); v.Paused {
		t.Fatal("the run was paused without the user's yes")
	}
	f.setAnswer(true)
	if out, failed = f.call(t, "coord", f.repo, mcptools.ToolWorkflowSteer, `{"run":"`+webRun.ID+`","action":"pause"}`); failed || !strings.Contains(out, "Done: pause") {
		t.Fatalf("pause: %q (failed %v)", out, failed)
	}
	if v, _ := f.runIn(f.web); !v.Paused || v.Log[len(v.Log)-1].Text != "Asked by an agent in "+f.repo+" (chat coord), approved by you" {
		t.Errorf("after pause: %+v", v)
	}

	// The web run's step ends; the run holds before its next step, and the
	// coordinator's wait returns with it.
	f.finish(p.webChat, "RESULT: DONE")
	out, failed = f.call(t, "coord", f.repo, mcptools.ToolWorkflowWait, `{"run":"`+webRun.ID+`","timeoutSeconds":30}`)
	if failed || !strings.HasPrefix(out, "timedOut: false") || !strings.Contains(out, "state: paused") || !strings.Contains(out, "folder: "+f.web) {
		t.Fatalf("wait (failed %v):\n%s", failed, out)
	}

	// No run outside the project is reached, by any of the three tools.
	for name, args := range map[string]string{
		mcptools.ToolWorkflowRuns:  `{"run":"` + unrelated.ID + `"}`,
		mcptools.ToolWorkflowWait:  `{"run":"` + unrelated.ID + `","timeoutSeconds":1}`,
		mcptools.ToolWorkflowSteer: `{"run":"` + unrelated.ID + `","action":"pause"}`,
	} {
		if out, failed := f.call(t, "coord", f.repo, name, args); !failed || !strings.Contains(out, "no workflow run") {
			t.Errorf("%s reached a run outside the project: %q (failed %v)", name, out, failed)
		}
	}
	if v, _ := f.runIn(other); v.Paused || v.State != workflows.StateRunning {
		t.Errorf("the unrelated run: %+v", v)
	}
	// ... and the other way round: a chat of that repository does not reach
	// the project's runs either.
	if out, failed := f.call(t, "outsider", other, mcptools.ToolWorkflowRuns, `{}`); failed || strings.Contains(out, apiRun.ID) || strings.Contains(out, webRun.ID) || !strings.Contains(out, unrelated.ID) {
		t.Errorf("the outsider's runs (failed %v):\n%s", failed, out)
	}

	// A chat that is a step of one of the runs gets none of the tools.
	webWorktree := f.worktreeOf(p.webTask)
	before, asked := f.world(t), len(f.questions())
	for _, who := range []struct{ name, chat, root string }{
		{"the step's own chat, in the api repository", p.webChat, f.repo},
		{"the step's own chat, in its worktree", p.webChat, webWorktree},
		{"another chat in the step's worktree", "helper", webWorktree},
		{"the api step's chat in the api repository", p.apiChat, f.repo},
	} {
		for name, args := range map[string]string{
			mcptools.ToolWorkflowList:  `{}`,
			mcptools.ToolWorkflowRuns:  `{}`,
			mcptools.ToolWorkflowWait:  `{"run":"` + webRun.ID + `","timeoutSeconds":1}`,
			mcptools.ToolWorkflowSteer: `{"run":"` + webRun.ID + `","action":"stop"}`,
			mcptools.ToolWorkflowStart: `{"workflow":"ship","task":"recurse"}`,
		} {
			if out, failed := f.call(t, who.chat, who.root, name, args); !failed || !strings.Contains(out, "a workflow step cannot start or steer workflows") {
				t.Errorf("%s by %s: %q (failed %v)", name, who.name, out, failed)
			}
		}
		if out, failed := f.call(t, who.chat, who.root, projects.DelegateToolName, `{"item":"dep"}`); !failed || !strings.Contains(out, "an agent that was handed work cannot hand out work") {
			t.Errorf("%s by %s: %q (failed %v)", projects.DelegateToolName, who.name, out, failed)
		}
	}
	f.waitStarts()
	if len(f.questions()) != asked {
		t.Errorf("a refused step was asked about: %q", f.questions()[asked:])
	}
	before.same(t, f.world(t))
	if v, _ := f.runIn(f.web); v.State != workflows.StatePaused {
		t.Errorf("a step stopped the run: %s", v.State)
	}
}

// For every tool of both modules, arguments that ask to approve, continue,
// skip or check never change a run at its approval step or a failed run;
// the user's own Approve still does.
func TestNoToolOfEitherModuleApprovesOrSkipsADelegatedRun(t *testing.T) {
	f := newDlgFixture(t, 0, dlgItems())
	p := f.delegatePair(t)
	f.finish(p.apiChat, "RESULT: DONE")
	f.finish(p.webChat, "RESULT: BLOCKED")
	f.ev.until(t, "the gate and the failure", func() bool {
		return f.runState(f.repo) == workflows.StateWaiting && f.runState(f.web) == workflows.StateFailed
	})
	// The failed step's chat now ends with a pass: a "Check again" would
	// pass the step, which no tool may do either.
	f.app.orch.Observe(as.Event{SessionID: p.webChat, Kind: as.KindMessageChunk, Text: "\nRESULT: DONE"})
	gated, _ := f.runIn(f.repo)
	failed, _ := f.runIn(f.web)

	// Through the server: the refusal says whose decision it is.
	asked := len(f.questions())
	out, bad := f.call(t, "coord", f.repo, mcptools.ToolWorkflowSteer, `{"run":"`+gated.ID+`","action":"approve"}`)
	if !bad || !strings.Contains(out, "only the user can approve") || len(f.questions()) != asked {
		t.Errorf("approve through the server: %q (failed %v), asked %d times", out, bad, len(f.questions())-asked)
	}

	tools := []string{projects.ToolName, projects.AddToolName, projects.DelegateToolName,
		mcptools.ToolWorkflowList, mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowStart, mcptools.ToolWorkflowSteer}
	actions := []string{"approve", "Approve", "APPROVE", "skip", "continue", "continue_anyway", "continueAnyway", "pass", "accept", "approved", "sendBack", "done", "", "resume",
		"recheck", "check", "check_again", "checkAgain", "Recheck"}
	for _, tool := range tools {
		f.agentTool(t, tool) // offered
	}
	for _, id := range []string{gated.ID, failed.ID} {
		for _, tool := range tools {
			for _, act := range actions {
				for _, extra := range []string{"", `,"step":"Approve"`, `,"approve":true`, `,"item":"rl"`} {
					args := `{"run":"` + id + `","action":"` + act + `"` + extra + `}`
					_, _ = f.callDirect(t, "coord", f.repo, tool, args)
				}
			}
		}
	}
	f.waitStarts()
	if v, _ := f.runIn(f.repo); v.State != workflows.StateWaiting || v.Steps[1].State == "done" || v.Current != 1 {
		t.Errorf("the gate was passed: %s, current %d, %+v", v.State, v.Current, v.Steps)
	}
	if v, _ := f.runIn(f.web); v.State != workflows.StateFailed || v.Steps[0].Skipped || v.Steps[0].State != "failed" || v.Current != 0 {
		t.Errorf("the failed step was skipped or passed: %s, current %d, %+v", v.State, v.Current, v.Steps)
	}
	if slices.Contains(f.questions(), "") {
		t.Error("an empty question")
	}

	// The same gate does pass by the user's own action (the test is not
	// vacuous).
	if err := f.wf.Approve(gated.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.runIn(f.repo); v.State != workflows.StateDone {
		t.Errorf("the user's Approve left the run %s", v.State)
	}
}

// Stopping a delegated run whose agent task waits in the queue: the task is
// taken off the queue without ever starting a chat when a slot frees (the
// real TaskStartCheck), while its sibling goes on.
func TestStoppedDelegatedRunQueuedTaskNeverStarts(t *testing.T) {
	f := newDlgFixture(t, 1, dlgItems())
	f.delegate(t, "rl")
	f.waitStarts()
	apiTask, webTask := f.taskOf(t, f.repo), f.taskOf(t, f.web)
	apiChat := f.firstChat(t, apiTask)
	if got := f.taskState(webTask); got != "queued" {
		t.Fatalf("the second repository's task is %s, want queued", got)
	}
	if got := f.status(t, "rl"); got != projects.StatusDoing {
		t.Fatalf("the item is %q", got)
	}
	webRun, _ := f.runIn(f.web)
	if !webRun.Queued {
		t.Fatalf("the web run is not marked queued: %+v", webRun)
	}

	// The user stops the queued task's run.
	if err := f.wf.Stop(webRun.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.runState(f.web); got != workflows.StateStopped {
		t.Fatalf("the web run is %s", got)
	}

	// The first one finishes its step: its slot frees, the queue moves.
	f.finish(apiChat, "RESULT: DONE")
	f.ev.until(t, "the api run at its approval step", func() bool { return f.runState(f.repo) == workflows.StateWaiting })
	f.waitStarts()
	f.app.startQueuedTasks()
	f.waitStarts()
	for _, c := range f.ev.chats() {
		if c.TaskID == webTask {
			t.Fatalf("a chat started for the stopped run's task: %+v", c)
		}
	}
	if got := f.taskState(webTask); got == "queued" || got == "working" {
		t.Errorf("the stopped run's task is %s", got)
	}
	if got := f.runState(f.web); got != workflows.StateStopped {
		t.Errorf("the web run is %s", got)
	}
	if got := f.runState(f.repo); got != workflows.StateWaiting {
		t.Errorf("the sibling run is %s", got)
	}
	// A stopped run keeps the item in doing, marked failed, not review.
	if got := f.status(t, "rl"); got != projects.StatusDoing {
		t.Errorf("the item is %q", got)
	}
}

// Stopping a delegated run whose step's chat is running ends that chat; its
// sibling's chat is not touched.
func TestStoppedDelegatedRunStopsItsChat(t *testing.T) {
	f := newDlgFixture(t, 0, dlgItems())
	p := f.delegatePair(t)
	apiRun, _ := f.runIn(f.repo)
	f.driver.waitPrompts(t, p.apiChat, 1)
	f.driver.waitPrompts(t, p.webChat, 1)

	if err := f.wf.Stop(apiRun.ID); err != nil {
		t.Fatal(err)
	}
	stopped := func() []string {
		f.driver.mu.Lock()
		defer f.driver.mu.Unlock()
		return append([]string(nil), f.driver.stopped...)
	}
	if got := stopped(); !slices.Contains(got, p.apiChat) || slices.Contains(got, p.webChat) {
		t.Errorf("chats stopped: %v (api %s, web %s)", got, p.apiChat, p.webChat)
	}
	if _, ok := f.app.orch.Get(p.apiChat); ok {
		t.Error("the stopped run's chat is still registered")
	}
	if _, ok := f.app.orch.Get(p.webChat); !ok {
		t.Error("the sibling's chat was closed")
	}
	if got := f.runState(f.repo); got != workflows.StateStopped {
		t.Errorf("the api run is %s", got)
	}
	if got := f.runState(f.web); got != workflows.StateRunning {
		t.Errorf("the web run is %s", got)
	}
}
