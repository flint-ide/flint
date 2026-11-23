package workflows

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/feature/featuretest"
	"flint/internal/settings"
)

// fixture is the API over a fake host with one open, trusted workspace
// folder that is a checkout, the agents claude (default) and codex, a
// clock the test moves and timers the test fires.
type fixture struct {
	t       *testing.T
	h       *featuretest.Host
	api     *API
	e       *engine
	root    string
	clock   *atomic.Int64
	timers  *fakeTimers
	changes chan struct{}
}

func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	h := featuretest.New(t.Context())
	h.Dir = tempDir(t)
	root := filepath.Join(tempDir(t), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	h.SetRoots(root)
	h.SetRepository(root)
	h.SetTrusted(root)
	h.SetAgents("claude", "claude", "codex")
	f := &fixture{t: t, h: h, root: root, clock: &atomic.Int64{}, timers: &fakeTimers{}, changes: make(chan struct{}, 1)}
	f.clock.Store(1000)
	f.api = newAPI(h)
	f.e = f.wire(f.api.engine)
	f.e.start(t.Context())
	t.Cleanup(func() { f.e.close(); f.e.wg.Wait() })
	return f
}

// wire gives an engine the fixture's clock, timers and change signal.
func (f *fixture) wire(e *engine) *engine {
	e.now = f.clock.Load
	e.afterFunc = f.timers.after
	e.changed = func() {
		select {
		case f.changes <- struct{}{}:
		default:
		}
	}
	return e
}

// restart builds a new engine on the same host, as Flint's next launch.
func (f *fixture) restart() {
	f.t.Helper()
	f.e.close()
	f.e.wg.Wait()
	f.api = newAPI(f.h)
	f.e = f.wire(f.api.engine)
	f.e.start(f.t.Context())
	f.t.Cleanup(func() { f.e.close(); f.e.wg.Wait() })
}

// allowUnsandboxed accepts script steps without a sandbox in the
// fixture's checkout, as the user's "Run without a sandbox" does.
func (f *fixture) allowUnsandboxed() { f.api.rememberUnsandboxed(f.root) }

// sandboxOff switches workflows.sandbox off explicitly (it is on by
// default): script steps then run as the user's own and ask for the user's OK.
func (f *fixture) sandboxOff() {
	f.settings(base(func(s *Settings) { s.Sandbox = SandboxOff }))
}

func (f *fixture) settings(s Settings) {
	f.h.SetSettings(Section.With(settings.Default(), s))
}

// start starts a run of workflow and returns it.
func (f *fixture) start(workflow string) RunView {
	f.t.Helper()
	v, err := f.api.Start(StartRequest{Dir: f.root, Workflow: workflow, Description: "Add a --verbose flag"})
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

// waitFor waits until the run satisfies cond (no sleeps: every applied
// change signals).
func (f *fixture) waitFor(id, what string, cond func(Run) bool) Run {
	f.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		r, ok := f.e.get(id)
		if ok && cond(r) {
			return r
		}
		select {
		case <-f.changes:
		case <-deadline:
			f.t.Fatalf("waiting for %s: the run is %+v", what, r)
		}
	}
}

// finishFirst ends the first step's turn, taken by the task's own chat,
// with output.
func (f *fixture) finishFirst(id, output string) Run {
	f.t.Helper()
	chat := f.taskChat(id, "task-chat-"+id)
	f.h.SetChatOutput(chat, output)
	r, _ := f.e.get(id)
	f.h.UpdateAgentTask(r.Task, func(t *feature.AgentTask) { t.State = agenttask.Finished })
	f.e.kickTasks()
	return f.waitFor(id, "the first step's end", func(r Run) bool { return !byTask(r) })
}

// taskChat plays Flint opening chat for the first step of run id, as its
// agent task's start does (Instance.TaskChatStarted), and returns it.
func (f *fixture) taskChat(id, chat string) string {
	f.t.Helper()
	r, _ := f.e.get(id)
	f.h.SetTaskChat(r.Task, chat)
	f.e.taskChatStarted(feature.TaskStart{Origin: originPrefix + id, TaskID: r.Task}, chat)
	return chat
}

// chatOf waits for the chat of step i to be opened and returns it.
func (f *fixture) chatOf(id string, i int) string {
	f.t.Helper()
	r := f.waitFor(id, "a chat for step "+strconv(i), func(r Run) bool {
		return r.Current == i && r.Records[i].State == StepRunning && r.Records[i].Chat != ""
	})
	return r.Records[i].Chat
}

// finishChat ends the turn of step i's chat with output.
func (f *fixture) finishChat(id string, i int, output string) Run {
	f.t.Helper()
	return f.endChat(id, i, feature.ChatResult{State: agenttask.Finished}, output)
}

func (f *fixture) endChat(id string, i int, res feature.ChatResult, output string) Run {
	f.t.Helper()
	chat := f.chatOf(id, i)
	f.h.SetChatOutput(chat, output)
	f.h.FinishChat(chat, res)
	return f.waitFor(id, "the end of "+chat, func(r Run) bool {
		return r.Records[i].Chat != chat || r.Records[i].State != StepRunning
	})
}

func strconv(i int) string { return string(rune('0' + i)) }

// savedRuns decodes what the engine saved.
func (f *fixture) savedRuns() []Run {
	f.t.Helper()
	var runs []Run
	if raw := f.h.State(stateRuns); raw != nil {
		if err := json.Unmarshal(raw, &runs); err != nil {
			f.t.Fatal(err)
		}
	}
	return runs
}

// fakeTimers records the timers the engine arms; the test fires them.
type fakeTimers struct {
	mu    sync.Mutex
	armed []*fakeTimer
}

type fakeTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

func (ft *fakeTimers) after(d time.Duration, f func()) func() bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	t := &fakeTimer{d: d, f: f}
	ft.armed = append(ft.armed, t)
	return func() bool {
		ft.mu.Lock()
		defer ft.mu.Unlock()
		was := !t.stopped
		t.stopped = true
		return was
	}
}

// live are the timers armed and not stopped.
func (ft *fakeTimers) live() []*fakeTimer {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	var out []*fakeTimer
	for _, t := range ft.armed {
		if !t.stopped {
			out = append(out, t)
		}
	}
	return out
}

// fire runs the only live timer, as its time came.
func (ft *fakeTimers) fire(t *testing.T) time.Duration {
	t.Helper()
	live := ft.live()
	if len(live) != 1 {
		t.Fatalf("%d live timers, want 1", len(live))
	}
	ft.mu.Lock()
	live[0].stopped = true
	ft.mu.Unlock()
	live[0].f()
	return live[0].d
}

// ---- whole runs

func TestFeatureWorkflowRunsToDone(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	if v.State != StateRunning || v.Task == "" || v.Steps[0].State != StepRunning || v.Steps[0].Attempts != 1 {
		t.Fatalf("started run = %+v", v)
	}
	req := f.h.Requested()
	if len(req) != 1 || req[0][0].Origin != "workflow:"+v.ID || req[0][0].Agent != "claude" || req[0][0].Description != "Add a --verbose flag" {
		t.Fatalf("CreateAgentTasks got %+v", req)
	}
	// Flint starts the task's chat and asks for the text before the description.
	text := f.e.taskContext(t.Context(), feature.TaskStart{Origin: "workflow:" + v.ID})
	if !strings.HasSuffix(text, "===== The task =====") || !strings.Contains(text, `step 1 of 4, "Plan"`) {
		t.Errorf("first step's context = %q", text)
	}
	f.waitFor(v.ID, "the task seen working", func(r Run) bool { return r.Records[0].Seen })
	r := f.finishFirst(v.ID, "Wrote PLAN.md\nRESULT: DONE")
	if r.Records[0].State != StepDone || r.Records[0].Chat != "task-chat-"+v.ID || r.Current != 1 {
		t.Fatalf("after the plan: %+v", r.Records)
	}
	r = f.finishChat(v.ID, 1, "Implemented.\n**RESULT: DONE**")
	if r.Records[1].State != StepDone {
		t.Fatalf("implement: %+v", r.Records[1])
	}
	r = f.finishChat(v.ID, 2, "Looks good.\nRESULT: PASS")
	if r.State != StateWaiting || r.Current != 3 {
		t.Fatalf("at the gate: %s %d", r.State, r.Current)
	}
	calls := f.h.StepChats()
	if len(calls) != 2 || calls[0].Agent != "claude" || calls[0].Task != v.Task ||
		!strings.Contains(calls[0].Prompt, "===== Your step =====") || !strings.HasSuffix(calls[0].Prompt, "Add a --verbose flag") ||
		!strings.Contains(calls[1].Prompt, "You are reviewing") {
		t.Errorf("step chats = %+v", calls)
	}
	if err := f.api.Approve(v.ID); err != nil {
		t.Fatal(err)
	}
	r = f.waitFor(v.ID, "done", func(r Run) bool { return r.State == StateDone })
	if saved := f.savedRuns(); len(saved) != 1 || saved[0].State != StateDone {
		t.Errorf("saved %+v", saved)
	}
	if !slices.ContainsFunc(f.h.Events(), func(e featuretest.Event) bool { return e.Name == EventChanged && e.Data[0] == v.ID }) {
		t.Error("no workflows:changed event")
	}
	if got := f.e.taskContext(t.Context(), feature.TaskStart{Origin: "workflow:" + v.ID}); got != "" {
		t.Errorf("a done run answered the task's context: %q", got)
	}
}

func TestTaskContextAnswersOnlyItsFirstStep(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	for _, origin := range []string{"", "project:" + v.ID, "workflow:0000000000000000", "workflow:" + v.ID + "x", "workflow:../x"} {
		if got := f.e.taskContext(t.Context(), feature.TaskStart{Origin: origin}); got != "" {
			t.Errorf("origin %q answered %q", origin, got)
		}
	}
	if f.e.taskContext(t.Context(), feature.TaskStart{Origin: "workflow:" + v.ID}) == "" {
		t.Error("its own origin got no answer")
	}
	f.finishFirst(v.ID, "RESULT: DONE")
	if got := f.e.taskContext(t.Context(), feature.TaskStart{Origin: "workflow:" + v.ID}); got != "" {
		t.Errorf("step 1 answered: %q", got)
	}
}

func TestReviewFailSendsBackUntilMaxAttempts(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 2})
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.finishChat(v.ID, 1, "RESULT: DONE")
	r := f.finishChat(v.ID, 2, "The flag is not parsed.\nRESULT: FAIL")
	if r.Current != 1 || r.State != StateRunning || !strings.Contains(r.Feedback, `The step "Review" failed: the review found problems`) {
		t.Fatalf("after the first FAIL: current %d %s %q", r.Current, r.State, r.Feedback)
	}
	f.chatOf(v.ID, 1)
	calls := f.h.StepChats()
	if p := calls[len(calls)-1].Prompt; !strings.Contains(p, "===== Sent back to this step =====") || !strings.Contains(p, "The flag is not parsed.") {
		t.Errorf("the implement prompt lacks the feedback: %q", p)
	}
	f.finishChat(v.ID, 1, "RESULT: DONE")
	r = f.finishChat(v.ID, 2, "Still broken.\nRESULT: FAIL")
	if r.State != StateFailed || r.Current != 2 || r.Records[2].Attempts != 2 || r.Error != reasonReview {
		t.Errorf("after MaxAttempts: %s current %d attempts %d %q", r.State, r.Current, r.Records[2].Attempts, r.Error)
	}
}

func TestBlockedAndNoResultWaitForTheUser(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	r := f.finishChat(v.ID, 1, "Which flag library?\nRESULT: BLOCKED")
	if r.State != StateFailed || r.Error != reasonBlocked || !r.NeedsUser() {
		t.Fatalf("blocked: %s %q", r.State, r.Error)
	}
	if err := f.api.Retry(v.ID); err != nil {
		t.Fatal(err)
	}
	r = f.finishChat(v.ID, 1, "I think it is done now.")
	if r.State != StateFailed || r.Error != reasonNoResult || r.Records[1].Attempts != 2 {
		t.Fatalf("no result: %s %q attempts %d", r.State, r.Error, r.Records[1].Attempts)
	}
	if err := f.api.Skip(v.ID); err != nil {
		t.Fatal(err)
	}
	r = f.waitFor(v.ID, "the review", func(r Run) bool { return r.Current == 2 && r.Records[2].Chat != "" })
	if !r.Records[1].Skipped {
		t.Error("the skipped step is not marked")
	}
}

// scriptWorkflow is a project workflow: plan, a script step that sends the
// work back to plan, a gate.
const scriptWorkflow = `## Plan

Plan it.

## Test
type: script
run: test
on-fail: Plan

## Approve
type: gate
`

func TestScriptSteps(t *testing.T) {
	cases := []struct {
		name  string
		res   *feature.RunResult // nil: no configuration named test
		state string
		cur   int
		error string
	}{
		{"pass", &feature.RunResult{ID: "run-1", Code: 0, Output: "ok"}, StateWaiting, 2, ""},
		{"fail sends back", &feature.RunResult{ID: "run-1", Code: 1, Output: "FAIL TestFlag"}, StateRunning, 0, ""},
		{"stopped in its tab", &feature.RunResult{ID: "run-1", Code: -1, Stopped: true}, StateFailed, 1, "it was stopped"},
		{"host error", nil, StateFailed, 1, `no run configuration named "test": add it to .flint/run.json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
			// The sandbox is on by default: no consent is needed and the
			// step is started confined, without network or extra folders.
			if c.res != nil {
				f.h.SetRunResult("test", *c.res, nil)
			}
			v := f.start("check")
			f.finishFirst(v.ID, "RESULT: DONE")
			// The final state of the transition, not a state on the way.
			r := f.waitFor(v.ID, "the script's end", func(r Run) bool {
				return r.State == c.state && r.Current == c.cur && r.Records[1].State != StepRunning
			})
			if r.State != c.state || r.Current != c.cur || r.Error != c.error {
				t.Fatalf("state %s current %d error %q", r.State, r.Current, r.Error)
			}
			runs := f.h.Runs()
			if len(runs) != 1 || runs[0].Task != v.Task || runs[0].Name != "test" || runs[0].Sandbox == nil || runs[0].Sandbox.Network || len(runs[0].Sandbox.Writable) != 0 {
				t.Errorf("runs %+v", runs)
			}
			if c.res != nil && r.Records[1].RunTab != "run-1" {
				t.Errorf("run tab %q", r.Records[1].RunTab)
			}
			if c.name == "fail sends back" {
				f.chatOf(v.ID, 0) // the plan is taken again in a new chat
				calls := f.h.StepChats()
				if !strings.Contains(calls[0].Prompt, "it exited with code 1") || !strings.Contains(calls[0].Prompt, "FAIL TestFlag") {
					t.Errorf("the plan's prompt lacks the script's failure: %q", calls[0].Prompt)
				}
			}
		})
	}
}

// With the sandbox switched off explicitly the step starts unconfined.
func TestScriptStepWithSandboxOff(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.sandboxOff()
	f.allowUnsandboxed()
	f.h.SetRunResult("test", feature.RunResult{ID: "run-1", Code: 1, Output: "FAIL"}, nil)
	v := f.start("check")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.chatOf(v.ID, 0) // sent back
	if runs := f.h.Runs(); len(runs) != 1 || runs[0] != (featuretest.RunCall{Task: v.Task, Name: "test"}) {
		t.Errorf("runs %+v", runs)
	}
	r, _ := f.e.get(v.ID)
	for _, e := range r.Log {
		if strings.Contains(e.Text, "This step ran in a sandbox") {
			t.Errorf("an unsandboxed failure got the sandbox hint: %q", e.Text)
		}
	}
}

// A sandboxed step that fails: the hint is in the log, not in the agent's feedback.
func TestScriptStepSandboxedFailureHint(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.h.SetRunResult("test", feature.RunResult{ID: "run-1", Code: 1, Output: "FAIL"}, nil)
	v := f.start("check")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.chatOf(v.ID, 0)
	r, _ := f.e.get(v.ID)
	if !slices.ContainsFunc(r.Log, func(e LogEntry) bool { return e.Text == sandboxHintText }) {
		t.Errorf("log lacks the hint: %+v", r.Log)
	}
	if strings.Contains(r.Feedback, "sandbox") || strings.Contains(r.Error, "sandbox") {
		t.Errorf("hint leaked: feedback %q error %q", r.Feedback, r.Error)
	}
}

func TestScriptStepSandbox(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.settings(base(func(s *Settings) {
		s.Sandbox, s.SandboxWritable = SandboxOn, []string{"/opt/cache"}
	}))
	// The host refuses a sandbox it cannot set up: the step fails and waits.
	f.h.SetRunResult("test", feature.RunResult{}, errors.New("the sandbox is not available here (no Landlock): switch off “Run script steps in a sandbox” in Settings ▸ Workflows to run it without one (Flint asks you once per folder)"))
	v := f.start("check")
	f.finishFirst(v.ID, "RESULT: DONE")
	r := f.waitFor(v.ID, "the script's end", func(r Run) bool { return r.State == StateFailed })
	if r.Current != 1 || !strings.Contains(r.Error, "the sandbox is not available here") {
		t.Fatalf("state %s current %d error %q", r.State, r.Current, r.Error)
	}
	runs := f.h.Runs()
	if len(runs) != 1 || runs[0].Sandbox == nil || runs[0].Sandbox.Network || !slices.Equal(runs[0].Sandbox.Writable, []string{"/opt/cache"}) {
		t.Errorf("runs %+v", runs)
	}
}

func TestPauseHoldsBeforeTheNextStep(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.chatOf(v.ID, 1)
	if err := f.api.Pause(v.ID); err != nil {
		t.Fatal(err)
	}
	r := f.finishChat(v.ID, 1, "RESULT: DONE")
	if r.State != StatePaused || r.Current != 2 || r.Records[2].State != StepPending {
		t.Fatalf("paused: %s current %d", r.State, r.Current)
	}
	f.e.wg.Wait()
	if n := len(f.h.StepChats()); n != 1 {
		t.Fatalf("%d chats started while paused", n)
	}
	if err := f.api.Resume(v.ID); err != nil {
		t.Fatal(err)
	}
	f.chatOf(v.ID, 2)
}

func TestStopStopsTheChatAndIgnoresItsEnd(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	chat := f.chatOf(v.ID, 1)
	if err := f.api.Stop(v.ID); err != nil {
		t.Fatal(err)
	}
	f.h.FinishChat(chat, feature.ChatResult{State: agenttask.Finished}) // too late
	f.e.wg.Wait()
	r, _ := f.e.get(v.ID)
	if r.State != StateStopped || r.Records[1].State != StepFailed {
		t.Fatalf("stopped run: %s %+v", r.State, r.Records[1])
	}
	// Every chat the run opened that is still open: the first step's (the
	// task's own) and the running step's.
	if got := f.h.Stopped(); !slices.Equal(got, []string{"task-chat-" + v.ID, chat}) {
		t.Errorf("stopped chats %v", got)
	}
	if n := len(f.h.StepChats()); n != 1 {
		t.Errorf("%d chats after Stop", n)
	}
	if err := f.api.Stop(v.ID); err == nil {
		t.Error("a stopped run was stopped again")
	}
}

func TestStopDuringTheFirstStepStopsTheTaskChat(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.taskChat(v.ID, "task-chat")
	// Another chat opened in the worktree later (by the user, or an agent)
	// is not the step's: Stop leaves it alone.
	f.h.SetTaskChat(v.Task, "users-own-chat")
	if err := f.api.Stop(v.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.h.Stopped(); !slices.Equal(got, []string{"task-chat"}) {
		t.Errorf("stopped chats %v", got)
	}
}

func TestSendBackFromTheGate(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.finishChat(v.ID, 1, "RESULT: DONE")
	f.finishChat(v.ID, 2, "RESULT: PASS")
	if err := f.api.SendBack(v.ID, "plan", "Use the standard flag package."); err != nil {
		t.Fatal(err)
	}
	f.chatOf(v.ID, 0)
	calls := f.h.StepChats()
	if p := calls[len(calls)-1].Prompt; !strings.Contains(p, "Use the standard flag package.") || !strings.Contains(p, `"Plan"`) {
		t.Errorf("the plan's prompt: %q", p)
	}
	if err := f.api.SendBack(v.ID, strings.Repeat("x", maxArg+1), ""); err == nil {
		t.Error("an overlong step was taken")
	}
}

func TestRestart(t *testing.T) {
	f := newFixture(t)
	later := f.start("feature")
	f.finishFirst(later.ID, "RESULT: DONE")
	f.chatOf(later.ID, 1)
	first := f.start("bug-fix")
	f.restart()

	r, _ := f.e.get(later.ID)
	if r.State != StateFailed || r.Records[1].State != StepFailed || !strings.Contains(r.Error, "Flint quit") {
		t.Errorf("a running later step: %s %q", r.State, r.Error)
	}
	r, _ = f.e.get(first.ID)
	if !byTask(r) || r.State != StateRunning {
		t.Fatalf("a first step taken by its task was interrupted: %s %+v", r.State, r.Records[0])
	}
	if saved := f.savedRuns(); len(saved) != 2 || saved[0].State != StateFailed {
		t.Errorf("the interruption was not saved: %+v", saved)
	}
	f.finishFirst(first.ID, "RESULT: DONE")
	f.chatOf(first.ID, 1)
}

func TestFirstStepEndsWithItsTask(t *testing.T) {
	cases := []struct {
		name   string
		state  string
		err    string
		seen   bool
		result string // the run's state after, "" = still the first step
		reason string
	}{
		{"queued", agenttask.Queued, "", false, "", ""},
		{"idle before it worked", agenttask.Idle, "", false, "", ""},
		{"idle after it worked", agenttask.Idle, "", true, StateFailed, reasonCanceled},
		{"failed", agenttask.Failed, "exit status 1", false, StateFailed, "exit status 1"},
		{"interrupted", agenttask.Interrupted, "", false, StateFailed, "its agent task was interrupted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			v := f.start("feature")
			f.waitFor(v.ID, "seen", func(r Run) bool { return r.Records[0].Seen })
			if !c.seen {
				_, _ = f.e.change(v.ID, func(r Run) (Run, error) {
					r = r.clone()
					r.Records[0].Seen = false
					return r, nil
				})
			}
			f.h.UpdateAgentTask(v.Task, func(t *feature.AgentTask) { t.State, t.Error = c.state, c.err })
			f.e.kickTasks()
			f.e.wg.Wait()
			r, _ := f.e.get(v.ID)
			if c.result == "" {
				if !byTask(r) {
					t.Fatalf("the first step ended: %s %q", r.State, r.Error)
				}
				return
			}
			if r.State != c.result || r.Error != c.reason {
				t.Errorf("state %s error %q", r.State, r.Error)
			}
		})
	}
}

func TestRemovedTaskStopsItsRun(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.h.RemoveAgentTask(v.Task)
	f.e.kickTasks()
	r := f.waitFor(v.ID, "stopped", func(r Run) bool { return r.State == StateStopped })
	if !strings.Contains(r.Log[len(r.Log)-1].Text, reasonRemoved) {
		t.Errorf("log %+v", r.Log)
	}
}

func TestStateIsPruned(t *testing.T) {
	f := newFixture(t)
	for i := range MaxRuns + 5 {
		r := NewRun(strings.Repeat("0", 14)+string(rune('a'+i/26))+string(rune('a'+i%26)), "feature", nil, "old", "claude", f.root, int64(i))
		r.Task = "t"
		f.e.add(r)
	}
	v := f.start("feature")
	saved := f.savedRuns()
	if len(saved) != MaxRuns || saved[len(saved)-1].ID != v.ID {
		t.Errorf("saved %d runs, last %q", len(saved), saved[len(saved)-1].ID)
	}
}
