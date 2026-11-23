package workflows

import (
	"slices"

	"flint/internal/agenttask"
)

// The actions a run view offers (RunView.Actions), as the UI names its
// buttons.
const (
	ActionApprove  = "approve"
	ActionSendBack = "sendBack"
	ActionRecheck  = "recheck"
	ActionRetry    = "retry"
	ActionSkip     = "skip"
	ActionPause    = "pause"
	ActionResume   = "resume"
	ActionStop     = "stop"
	ActionRemove   = "remove"
)

// Listing is what List offers for a workspace folder.
type Listing struct {
	// Workflows are sorted by name; one with a Problem is shown, not offered.
	Workflows []WorkflowView `json:"workflows"`
	// Problems are files that could not be read ("name.md: reason").
	Problems []string `json:"problems"`
	// Restricted: the folder is in Restricted Mode, so its repository's
	// workflows are left out; Hidden counts their files.
	Restricted bool `json:"restricted"`
	Hidden     int  `json:"hidden"`
	// Sandbox is how script steps run here (scripts.go): SandboxOn,
	// SandboxOff or SandboxUnavailable (on, but this system has none: a
	// script step fails).
	Sandbox string `json:"sandbox"`
	// UnsandboxedOK: the user allowed script steps without a sandbox in
	// this folder's checkout (unsandboxed.go); ForgetUnsandboxed takes it
	// back.
	UnsandboxedOK bool `json:"unsandboxedOK"`
}

// WorkflowView is a workflow as Run Workflow… shows it.
type WorkflowView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Source is "builtin", "personal" or "project"; Path the file ("" for a
	// built-in one).
	Source string `json:"source"`
	Path   string `json:"path"`
	// Steps are the steps a run would take, with the workflows it names put
	// in (empty when Problem is set).
	Steps []StepView `json:"steps"`
	// Problem says why the workflow cannot run ("" when it can).
	Problem string `json:"problem"`
}

// StepView is a step, and in a run what happened to it.
type StepView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Kind is agent, script, review or gate.
	Kind   string `json:"kind"`
	Agent  string `json:"agent"`
	Run    string `json:"run"`
	OnFail string `json:"onFail"`
	Text   string `json:"text"`
	// The step's record in a run ("" / 0 in a WorkflowView): State is
	// pending, running, done or failed; Chat the chat of its latest attempt,
	// RunTab the run tab of a script's; Output the end of what it said.
	State    string `json:"state"`
	Attempts int    `json:"attempts"`
	Chat     string `json:"chat"`
	RunTab   string `json:"runTab"`
	Output   string `json:"output"`
	Skipped  bool   `json:"skipped"`
	// TakenBy is the agent that takes the step instead of its own (the
	// fallback after a usage limit), "" when none.
	TakenBy string `json:"takenBy"`
	// Command is what a script step runs, as the folder's checkout
	// resolves its run configuration ("npm run test"); CommandProblem why
	// it cannot be resolved. Set by List only (scripts.go).
	Command        string `json:"command,omitempty"`
	CommandProblem string `json:"commandProblem,omitempty"`
}

// RunView is a run as the run view and the Workflows panel show it.
type RunView struct {
	ID       string `json:"id"`
	Workflow string `json:"workflow"`
	Title    string `json:"title"`
	// Description is the task the user wrote.
	Description string `json:"description"`
	// Task is the run's agent task; Dir the checkout it started from; Agent
	// the run's agent.
	Task  string `json:"task"`
	Dir   string `json:"dir"`
	Agent string `json:"agent"`
	// Origin says who started the run: "" the user, "schedule:<name>" a
	// schedule, "item" a project item's Delegate, "agent" an agent's
	// start_workflow call.
	Origin string `json:"origin"`
	// State is running, waiting, failed, paused, done or stopped; Error
	// says why it failed.
	State string `json:"state"`
	Error string `json:"error"`
	// Current is the index of the current step (len(Steps) when done).
	Current int `json:"current"`
	// Paused: a pause was asked for; the run holds before the next step.
	Paused bool `json:"paused"`
	// Feedback is the note the current step's agent gets.
	Feedback string `json:"feedback"`
	// TaskState is the agent task's state ("" when the task is gone);
	// Queued: the first step waits for a free slot (agentTasks.maxParallel).
	TaskState string `json:"taskState"`
	Queued    bool   `json:"queued"`
	// NeedsUser: the run waits for the user (a gate, a failure, a hold
	// that does not resume by itself).
	NeedsUser bool `json:"needsUser"`
	// Hold is set while the run is paused by a limit (null otherwise).
	Hold *Hold `json:"hold"`
	// Actions are what the user can do now: approve, sendBack, recheck,
	// retry, skip, pause, resume, stop, remove.
	Actions []string   `json:"actions"`
	Steps   []StepView `json:"steps"`
	Log     []LogEntry `json:"log"`
	// Created and Updated are unix seconds.
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// Actions are what the user can do with r now (the table of
// docs/design/workflows.md), in the order the buttons show.
func Actions(r Run) []string {
	out := []string{}
	add := func(ok bool, action string) {
		if ok {
			out = append(out, action)
		}
	}
	add(r.State == StateWaiting, ActionApprove)
	add(r.State == StateWaiting || r.State == StateFailed || r.State == StatePaused, ActionSendBack)
	add(r.Recheckable(), ActionRecheck)
	add(r.State == StateFailed, ActionRetry)
	add(r.State == StateFailed, ActionSkip)
	add(r.State == StateRunning && !r.Paused, ActionPause)
	add(r.Paused && (r.State == StateRunning || r.State == StatePaused), ActionResume) // never on a failed run
	add(!r.Over(), ActionStop)
	add(r.Over(), ActionRemove)
	return out
}

func stepView(s Step) StepView {
	return StepView{ID: s.ID, Title: s.Title, Kind: string(s.Kind), Agent: s.Agent, Run: s.Run, OnFail: s.OnFail, Text: s.Text}
}

func stepViews(steps []Step) []StepView {
	out := make([]StepView, len(steps))
	for i, s := range steps {
		out[i] = stepView(s)
	}
	return out
}

// runView shows r; taskState is its agent task's state ("" when gone).
func runView(r Run, taskState string) RunView {
	v := RunView{
		ID: r.ID, Workflow: r.Workflow, Title: r.Title, Description: r.Description,
		Task: r.Task, Dir: r.Dir, Agent: r.Agent, Origin: r.Origin, State: r.State, Error: r.Error,
		Current: r.Current, Paused: r.Paused, Feedback: r.Feedback,
		TaskState: taskState,
		Queued:    byTask(r) && taskState == agenttask.Queued,
		NeedsUser: r.NeedsUser() || (r.Hold != nil && r.Hold.Until == 0), Actions: Actions(r),
		Steps: make([]StepView, len(r.Steps)), Log: slices.Clone(r.Log),
		Created: r.Created, Updated: r.Updated,
	}
	if r.Hold != nil {
		h := *r.Hold
		v.Hold = &h
	}
	if v.Log == nil {
		v.Log = []LogEntry{}
	}
	for i, s := range r.Steps {
		sv := stepView(s)
		if i < len(r.Records) {
			rec := r.Records[i]
			sv.State, sv.Attempts, sv.Chat, sv.RunTab, sv.Output, sv.Skipped = rec.State, rec.Attempts, rec.Chat, rec.RunTab, rec.Output, rec.Skipped
			sv.TakenBy = rec.Agent
		}
		v.Steps[i] = sv
	}
	return v
}
