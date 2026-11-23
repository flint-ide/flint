package workflows

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"flint/internal/agenttask"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// The workflow tools that act: start_workflow and steer_workflow_run. Each
// call is checked by its Plan and needs the user's yes to that one call
// (mcpserver.Tool.Acts); what runs after the yes checks everything again.
// There is deliberately no way here to approve a step or to continue past a
// failed step: those are the user's decisions, and the whole point of an
// approval step.

// The actions steer_workflow_run takes, and nothing else.
const (
	steerPause    = "pause"
	steerResume   = "resume"
	steerRetry    = "retry"
	steerSendBack = "send_back"
	steerStop     = "stop"
)

var steerActions = []string{steerPause, steerResume, steerRetry, steerSendBack, steerStop}

// errUserOnly answers an agent that asks to approve a step or to continue
// past a failed step.
var errUserOnly = errors.New("only the user can approve a workflow's approval step, check a failed step again or continue past it (\"Continue anyway\"): " +
	"that is their decision. Tell the user the run waits for them in Flint's Workflows panel; you may pause, resume, retry, send back or stop a run")

// ---- start_workflow

func (a *API) startTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:  mcptools.ToolWorkflowStart,
		Title: "Start a workflow run",
		Description: "Starts a run of a Flint workflow (" + mcptools.ToolWorkflowList + " lists them) in this folder: Flint makes an agent task (a branch and a worktree) and its steps' agents take the steps in order. " +
			"The user is asked before each run starts. Follow the run with " + mcptools.ToolWorkflowWait + ". An approval step waits for the user: you cannot approve it. " +
			fmt.Sprintf("One chat starts at most %d runs.", maxStartsPerChat),
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
			`"workflow":{"type":"string","maxLength":%d,"description":"The workflow's name."},`+
			`"task":{"type":"string","maxLength":%d,"description":"What the run should deliver: every step's agent gets it."},`+
			`"agent":{"type":"string","maxLength":%d,"description":"The agent for the steps that name none (default: the user's default agent)."}`+
			`},"required":["workflow","task"],"additionalProperties":false}`, maxArg, agenttask.MaxPrompt, maxArg)),
		Acts:    true,
		Plan:    a.planStart,
		Timeout: createTimeout,
	}
}

type startArgs struct {
	Workflow string  `json:"workflow"`
	Task     *string `json:"task"`
	Agent    string  `json:"agent"`
}

// planStart checks a start_workflow call with every check of Run
// Workflow… and says what the user approves.
func (a *API) planStart(ctx context.Context, root string, raw json.RawMessage) (mcpserver.Planned, error) {
	var in startArgs
	if err := decodeArgs(raw, &in); err != nil {
		return mcpserver.Planned{}, err
	}
	if in.Task == nil {
		return mcpserver.Planned{}, errors.New("task is required: what the run should deliver")
	}
	if len(in.Workflow) > maxArg || len(in.Agent) > maxArg {
		return mcpserver.Planned{}, errors.New("invalid arguments: workflow and agent are names")
	}
	c, err := a.startCaller(ctx, root)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	req := runRequest{dir: c.root, workflow: in.Workflow, description: *in.Task, agent: in.Agent, origin: OriginAgent, by: startedBy(c), consent: consentAgent}
	checked, err := a.checkRun(req)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	// Refused before the user is asked to approve the call: an agent is
	// never the first to run scripts without a sandbox in a folder.
	if err := a.scriptConsent(checked); err != nil {
		return mcpserver.Planned{}, err
	}
	return mcpserver.Planned{
		Summary: startSummary(checked, a.scriptLines(checked)),
		Run:     func(ctx context.Context) (string, error) { return a.agentStart(ctx, root, req) },
	}, nil
}

// startCaller is caller for start_workflow: a workspace folder (not an
// agent task's worktree), and a chat that has room for one more run.
func (a *API) startCaller(ctx context.Context, root string) (toolCaller, error) {
	c, err := a.caller(ctx, root)
	if err != nil {
		return toolCaller{}, err
	}
	if c.worktree {
		return toolCaller{}, errors.New("start_workflow starts runs from a workspace folder, not from an agent task's worktree")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started[c.counter()] >= maxStartsPerChat {
		return toolCaller{}, fmt.Errorf("this chat started %d workflow runs, the most one chat may: the user can start more in Flint", maxStartsPerChat)
	}
	return c, nil
}

// agentStart starts the run after the user's yes, through the same path as
// Run Workflow… (startRun, every check again). The chat's start is counted
// before the run is made, so two calls at once cannot pass the limit.
func (a *API) agentStart(ctx context.Context, root string, req runRequest) (string, error) {
	c, err := a.startCaller(ctx, root)
	if err != nil {
		return "", err
	}
	key := c.counter()
	a.mu.Lock()
	if a.started[key] >= maxStartsPerChat {
		a.mu.Unlock()
		return "", fmt.Errorf("this chat started %d workflow runs, the most one chat may", maxStartsPerChat)
	}
	if _, ok := a.started[key]; !ok && !roomForCounter(a.started) {
		a.mu.Unlock()
		return "", fmt.Errorf("%d chats started workflow runs since Flint started: the user can start more in Flint", maxStartCounters)
	}
	a.started[key]++
	a.mu.Unlock()
	v, err := a.startRun(req)
	if err != nil {
		a.mu.Lock()
		a.started[key]--
		a.mu.Unlock()
		return "", err
	}
	return fmt.Sprintf("Started the run %s of the workflow %q (agent task %s, %s). "+
		"Follow it with %s {\"run\": %q}. When it waits at an approval step, tell the user: only they can approve it.",
		v.ID, v.Workflow, v.Task, v.State, mcptools.ToolWorkflowWait, v.ID), nil
}

// roomForCounter makes room for one more chat in counts (at most
// maxStartCounters): it forgets the chat with the fewest starts, never one
// at maxStartsPerChat, so a chat that used its starts cannot get them back
// by making the map overflow. False when every counted chat is at the limit.
// Callers hold a.mu.
func roomForCounter(counts map[string]int) bool {
	if len(counts) < maxStartCounters {
		return true
	}
	victim, least := "", maxStartsPerChat
	for k, n := range counts {
		if n < least {
			victim, least = k, n
		}
	}
	if victim == "" {
		return false
	}
	delete(counts, victim)
	return true
}

// startedBy is the log line of a run an agent started.
func startedBy(c toolCaller) string {
	return fmt.Sprintf("Run started by an agent in %s (chat %s), approved by you", c.root, cut(cmp.Or(c.chat, "unknown"), 80))
}

// startSummary is what the user approves: the workflow, its steps, the
// agents, the folder and the beginning of the task.
func startSummary(c checkedRun, scripts string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Start the workflow %q in %s (a new agent task: branch and worktree).\n", c.workflow.Name, c.dir)
	fmt.Fprintf(&b, "Steps: %s\n", stepsLine(stepViews(c.steps)))
	fmt.Fprintf(&b, "Agent: %s", c.agent)
	if first := c.firstAgent(); first != c.agent {
		fmt.Fprintf(&b, " (the first step: %s)", first)
	}
	b.WriteString("\nApproval steps wait for you; the agent cannot approve them.\n")
	b.WriteString(scripts)
	task := c.desc
	if utf8.RuneCountInString(task) > maxSummaryText {
		task = string([]rune(task)[:maxSummaryText]) + " […]"
	}
	b.WriteString("\nTask:\n" + task)
	return b.String()
}

// ---- steer_workflow_run

func (a *API) steerTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:  mcptools.ToolWorkflowSteer,
		Title: "Steer a workflow run",
		Description: "Pauses, resumes, retries, sends back or stops a Flint workflow run you can reach (this folder's, or one of its projects' items', as " + mcptools.ToolWorkflowRuns + " lists them). The user is asked before each call. " +
			"pause: pause before the next step; resume: go on after a pause; retry: take a failed step again; send_back: take the steps from step (an earlier or the current one) again, with note for its agent; stop: end the run (its agent task stays). " +
			"Approving a step, checking a failed step again and continuing anyway are the user's alone: ask the user.",
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
			`"run":{"type":"string","maxLength":16,"description":"The run's id."},`+
			`"action":{"type":"string","enum":["pause","resume","retry","send_back","stop"]},`+
			`"step":{"type":"string","maxLength":%d,"description":"send_back only: the step's id or title."},`+
			`"note":{"type":"string","maxLength":%d,"description":"send_back only: what the step's agent should do differently."}`+
			`},"required":["run","action"],"additionalProperties":false}`, maxArg, MaxNote*4)),
		Acts: true,
		Plan: a.planSteer,
	}
}

type steerArgs struct {
	Run    string `json:"run"`
	Action string `json:"action"`
	Step   string `json:"step"`
	Note   string `json:"note"`
}

// checkAction refuses anything but the five actions, and says why an
// approval or a "Continue anyway" is refused.
func checkAction(in steerArgs) error {
	norm := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(in.Action)))
	switch {
	case strings.Contains(norm, "approve") || strings.Contains(norm, "skip") || strings.Contains(norm, "continue") || strings.Contains(norm, "check") || norm == "pass" || norm == "accept":
		return errUserOnly
	case !isSteerAction(in.Action):
		return fmt.Errorf("action must be one of %s", strings.Join(steerActions, ", "))
	case in.Action != steerSendBack && (in.Step != "" || in.Note != ""):
		return errors.New("step and note go only with send_back")
	case in.Action == steerSendBack && strings.TrimSpace(in.Step) == "":
		return errors.New("send_back needs step: the id or title of the step to take again")
	case len(in.Step) > maxArg || len(in.Note) > 4*MaxNote:
		return errors.New("step or note is too long")
	}
	return nil
}

func isSteerAction(action string) bool {
	for _, s := range steerActions {
		if s == action {
			return true
		}
	}
	return false
}

// transition is the change action makes to a run; it never approves,
// checks again nor continues anyway.
func transition(in steerArgs) func(Run, int64) (Run, error) {
	switch in.Action {
	case steerPause:
		return Run.Pause
	case steerResume:
		return Run.Resume
	case steerRetry:
		return Run.Retry
	case steerSendBack:
		return func(r Run, now int64) (Run, error) { return r.SendBack(in.Step, in.Note, now) }
	case steerStop:
		return func(r Run, now int64) (Run, error) { return r.Stop("", now) }
	}
	return func(r Run, _ int64) (Run, error) { return r, errUserOnly }
}

// planSteer checks a steer_workflow_run call against the run as it is now
// and says what the user approves.
func (a *API) planSteer(ctx context.Context, root string, raw json.RawMessage) (mcpserver.Planned, error) {
	var in steerArgs
	if err := decodeArgs(raw, &in); err != nil {
		return mcpserver.Planned{}, err
	}
	if err := checkAction(in); err != nil {
		return mcpserver.Planned{}, err
	}
	c, err := a.caller(ctx, root)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	r, err := a.toolRun(c, in.Run)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	if _, err := transition(in)(r, a.engine.now()); err != nil {
		return mcpserver.Planned{}, err
	}
	return mcpserver.Planned{
		Summary: steerSummary(r, in),
		Run:     func(ctx context.Context) (string, error) { return a.agentSteer(ctx, root, in) },
	}, nil
}

// agentSteer steers the run after the user's yes, checking the caller and
// the run again.
func (a *API) agentSteer(ctx context.Context, root string, in steerArgs) (string, error) {
	c, err := a.caller(ctx, root)
	if err != nil {
		return "", err
	}
	if _, err := a.toolRun(c, in.Run); err != nil {
		return "", err
	}
	by := fmt.Sprintf("Asked by an agent in %s (chat %s), approved by you", c.root, cut(cmp.Or(c.chat, "unknown"), 80))
	if in.Action == steerStop {
		if err := a.engine.stop(in.Run, by); err != nil {
			return "", err
		}
	} else {
		fn := transition(in)
		if err := a.act(in.Run, func(r Run, now int64) (Run, error) {
			next, err := fn(r, now)
			if err != nil {
				return r, err
			}
			return next.logged(now, -1, by), nil
		}); err != nil {
			return "", err
		}
	}
	r, _ := a.engine.get(in.Run)
	return fmt.Sprintf("Done: %s. The run %s is %s now.", in.Action, in.Run, r.State), nil
}

// steerSummary is what the user approves.
func steerSummary(r Run, in steerArgs) string {
	what := map[string]string{
		steerPause: "Pause", steerResume: "Resume", steerRetry: "Retry the failed step of",
		steerSendBack: "Send back", steerStop: "Stop",
	}[in.Action]
	s := fmt.Sprintf("%s the workflow run %q (%s, workflow %q, now %s) in %s.", what, r.Title, r.ID, r.Workflow, r.State, r.Dir)
	if in.Action == steerSendBack {
		s += fmt.Sprintf("\nTo the step: %s", in.Step)
		if note := strings.TrimSpace(in.Note); note != "" {
			if utf8.RuneCountInString(note) > maxSummaryText {
				note = string([]rune(note)[:maxSummaryText]) + " […]"
			}
			s += "\nNote for its agent:\n" + note
		}
	}
	if in.Action == steerStop {
		s += "\nIts running step's chat or script is stopped; its agent task stays."
	}
	return s
}
