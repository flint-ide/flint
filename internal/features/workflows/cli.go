package workflows

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"flint/internal/agenttask"
	"flint/internal/cli"
	"flint/internal/mcptools"
)

// maxWaitCLI bounds run wait's --timeout (a week).
const maxWaitCLI = 7 * 24 * 60 * 60

// cliCommands are the feature's commands of the flint command line
// (`flint workflow …` and `flint run …`). Each calls what the bound API
// calls; a CLI start is the user's own, like Run Workflow….
func (a *API) cliCommands() []cli.Command {
	runArg := []cli.Arg{{Name: "run", Max: 64}}
	act := func(path, summary, help string, fn func(id string) error) cli.Command {
		return cli.Command{
			Path: path, Summary: summary, Help: help, Kind: cli.Acts, Args: runArg,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				id := c.Arg(0)
				if err := fn(id); err != nil {
					return cli.Result{}, err
				}
				return a.runResult(id)
			},
		}
	}
	return []cli.Command{
		{
			Path: "workflow list", Summary: "List the workflows offered in the folder", Kind: cli.Reads, Folder: true,
			Run: a.cliList,
		},
		{
			Path: "workflow show", Summary: "Show a workflow's steps", Kind: cli.Reads, Folder: true,
			Args: []cli.Arg{{Name: "name", Max: 64}}, Run: a.cliShow,
		},
		{
			Path: "workflow new", Summary: "Write a starter workflow to .flint/workflows/<name>.md", Kind: cli.Acts, Folder: true,
			Args: []cli.Arg{{Name: "name", Max: 64}},
			Help: "The folder must be trusted. An existing file is never overwritten.",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				path, err := a.NewWorkflow(c.Folder, c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: "Wrote " + path, JSON: map[string]string{"path": path}}, nil
			},
		},
		{
			Path: "workflow run", Summary: "Start a run of a workflow in the folder", Kind: cli.Acts, Folder: true,
			Args: []cli.Arg{{Name: "name", Max: 64}},
			Flags: []cli.Flag{
				{Name: "task", Value: "<text>", Usage: "what the run should deliver", Required: true, File: true, Max: agenttask.MaxPrompt},
				{Name: "agent", Value: "<agent>", Usage: "the agent for the steps that name none (default: the default agent)", Max: maxArg},
			},
			Help: "Like Run Workflow…: one agent task (a branch and a worktree) takes the steps in order.\n" +
				"The folder must be trusted and in a git repository. Follow it with flint run wait <run>.\n" +
				"The first time script steps would run without a sandbox in a checkout, Flint asks you in its own dialog.",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				v, err := a.Start(StartRequest{Dir: c.Folder, Workflow: c.Arg(0), Description: c.String("task"), Agent: c.String("agent")})
				if err != nil {
					return cli.Result{}, err
				}
				text := fmt.Sprintf("Started run %s of %s: %s\nFollow it: flint run wait %s", v.ID, v.Workflow, v.Title, v.ID)
				return cli.Result{Text: text, JSON: v}, nil
			},
		},
		{
			Path: "workflow coordinate", Summary: "Print the prompt that asks an agent to coordinate runs for a goal", Kind: cli.Reads, Folder: true,
			Flags: []cli.Flag{{Name: "goal", Value: "<text>", Usage: "what the runs should achieve", Required: true, File: true, Max: 4 * MaxGoal}},
			Help:  "Paste the prompt into an agent chat in Flint. The agent starts and steers runs only through Flint's tools, which ask you before each call; it can never approve.",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				p, err := a.CoordinatePrompt(c.Folder, c.String("goal"))
				if err != nil {
					return cli.Result{}, err
				}
				res := cli.Result{Text: p.Prompt, JSON: p}
				if !p.ToolOn {
					res.Note = "Note: agents get the workflow tools once Flint's MCP server and Settings ▸ Agent Context ▸ Actions ▸ \"Run and steer workflows\" are on."
				}
				if !p.DelegateOn && strings.Contains(p.Prompt, mcptools.ToolProjectDelegate) {
					res.Note = strings.TrimSpace(res.Note + "\nNote: agents can delegate the project's items once Settings ▸ Agent Context ▸ Actions ▸ \"Delegate project items\" is on.")
				}
				return res, nil
			},
		},
		{Path: "run list", Summary: "List the workflow runs, newest first", Kind: cli.Reads, Run: a.cliRuns},
		{
			Path: "run show", Summary: "Show a run: its steps, what it waits for, its log", Kind: cli.Reads, Args: runArg,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) { return a.runResult(c.Arg(0)) },
		},
		a.humanCommand("run approve", "Approve: pass the approval step the run waits at", "Only you can approve: agents cannot run this command.", approveDecision, a.Approve),
		{
			Path: "run send-back", Summary: "Take the steps from one step on again, with a note", Kind: cli.Acts, Args: runArg,
			Flags: []cli.Flag{
				{Name: "step", Value: "<step>", Usage: "the step's id or title", Required: true, Max: maxArg},
				{Name: "note", Value: "<text>", Usage: fmt.Sprintf("what the step's agent should change (at most %d characters)", MaxNote), File: true, Max: 4 * MaxNote},
			},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				id := c.Arg(0)
				if err := a.SendBack(id, c.String("step"), c.String("note")); err != nil {
					return cli.Result{}, err
				}
				return a.runResult(id)
			},
		},
		act("run check", "Check again: read the failed step's chat for its result", "Use it after you answered the agent in the step's chat. A result line of RESULT: DONE or PASS passes the step and the run goes on; otherwise the run stays failed.", a.Recheck),
		act("run retry", "Take the failed step again", "", a.Retry),
		a.humanCommand("run continue", "Continue anyway: pass the failed step and go on", "Only you can continue past a failed step: agents cannot run this command.", continueDecision, a.Skip),
		act("run pause", "Pause the run before its next step", "", a.Pause),
		act("run resume", "Let a paused run go on", "", a.Resume),
		act("run stop", "Stop the run and its running step; the agent task stays", "", a.Stop),
		{
			Path: "run remove", Summary: "Forget a run that is over; its agent task stays", Kind: cli.Acts, Args: runArg,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				if err := a.Remove(c.Arg(0)); err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: "Removed run " + c.Arg(0), JSON: map[string]string{"removed": c.Arg(0)}}, nil
			},
		},
		{
			Path: "run wait", Summary: "Wait until the run needs you, is paused or is over", Kind: cli.Reads, Args: runArg, Waits: true,
			Flags: []cli.Flag{{Name: "timeout", Value: "<seconds>", Usage: "give up after this long (default: no limit)", Max: 16}},
			Help: "Waits without polling. Exit status: 0 the run is done, 3 it needs you (waiting for your approval, a failed step,\n" +
				"paused by you, or paused by a limit that does not resume by itself), 1 it was stopped or removed, 4 the timeout passed.\n" +
				"A run paused by a limit that resumes by itself (workflows.resumeAfterMinutes) keeps waiting.",
			Run: a.cliWait,
		},
	}
}

func (a *API) cliList(_ context.Context, c *cli.Call) (cli.Result, error) {
	l, err := a.List(c.Folder)
	if err != nil {
		return cli.Result{}, err
	}
	rows := make([][]string, 0, len(l.Workflows))
	for _, w := range l.Workflows {
		desc := w.Description
		if w.Problem != "" {
			desc = "cannot run: " + w.Problem
		}
		rows = append(rows, []string{w.Name, w.Source, fmt.Sprint(len(w.Steps)), cli.Cut(desc, 80)})
	}
	text := cli.Table([]string{"NAME", "SOURCE", "STEPS", "DESCRIPTION"}, rows)
	for _, p := range l.Problems {
		text += "problem: " + p + "\n"
	}
	if l.Restricted && l.Hidden > 0 {
		text += fmt.Sprintf("%d workflow file(s) of this repository are not offered: the folder is in Restricted Mode.\n", l.Hidden)
	}
	return cli.Result{Text: text, JSON: l}, nil
}

func (a *API) cliShow(_ context.Context, c *cli.Call) (cli.Result, error) {
	l, err := a.List(c.Folder)
	if err != nil {
		return cli.Result{}, err
	}
	name := c.Arg(0)
	for _, w := range l.Workflows {
		if w.Name != name {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s (%s)\n", w.Name, w.Source)
		if w.Description != "" {
			b.WriteString(w.Description + "\n")
		}
		if w.Path != "" {
			b.WriteString(w.Path + "\n")
		}
		if w.Problem != "" {
			b.WriteString("cannot run: " + w.Problem + "\n")
		}
		b.WriteString("\n" + stepTable(w.Steps, false))
		return cli.Result{Text: b.String(), JSON: w}, nil
	}
	return cli.Result{}, fmt.Errorf("there is no workflow named %q here", name)
}

// stepTable lists steps; withState adds what happened to them in a run.
func stepTable(steps []StepView, withState bool) string {
	header := []string{"#", "STEP", "KIND", "BY", "ON FAIL"}
	if withState {
		header = append(header, "STATE")
	}
	rows := make([][]string, len(steps))
	for i, s := range steps {
		by := s.Agent
		if s.Kind == string(KindScript) {
			by = s.Run
		}
		if s.TakenBy != "" {
			by = s.TakenBy + " (fallback)"
		}
		r := []string{fmt.Sprint(i + 1), s.Title, s.Kind, by, s.OnFail}
		if withState {
			st := stepStateWord(s.State)
			if s.Attempts > 1 {
				st += fmt.Sprintf(" (attempt %d)", s.Attempts)
			}
			if s.Skipped {
				st += " (continued anyway)"
			}
			r = append(r, st)
		}
		rows[i] = r
	}
	return cli.Table(header, rows)
}

func (a *API) cliRuns(context.Context, *cli.Call) (cli.Result, error) {
	runs := a.Runs()
	rows := make([][]string, len(runs))
	for i, v := range runs {
		rows[i] = []string{v.ID, stateText(v), v.Workflow, currentStep(v), cli.Cut(v.Title, 60)}
	}
	text := cli.Table([]string{"RUN", "STATE", "WORKFLOW", "STEP", "TITLE"}, rows)
	if len(runs) == 0 {
		text = "No workflow runs.\n"
	}
	return cli.Result{Text: text, JSON: runs}, nil
}

// runStateWords are the words of the run view for a run's states.
var runStateWords = map[string]string{
	StateRunning: "Running", StateWaiting: "Waiting for you", StateFailed: "Failed",
	StatePaused: "Paused", StateDone: "Done", StateStopped: "Stopped",
}

// stepStateWord is the word for a step record's state.
func stepStateWord(state string) string {
	switch state {
	case StepPending:
		return "Pending"
	case StepRunning:
		return "Running"
	case StepDone:
		return "Done"
	case StepFailed:
		return "Failed"
	}
	return state
}

// stateText is the run's state in the UI's words, with what it waits for.
func stateText(v RunView) string {
	s := cmp.Or(runStateWords[v.State], v.State)
	switch {
	case v.Queued:
		s = "Queued"
	case v.Hold != nil:
		s = "Paused by a limit"
	case v.Paused && v.State == StateRunning:
		s += " (pausing)"
	}
	if v.NeedsUser && v.State != StateWaiting {
		s += ", needs you"
	}
	return s
}

func currentStep(v RunView) string {
	if v.Current >= 0 && v.Current < len(v.Steps) {
		return v.Steps[v.Current].Title
	}
	return ""
}

// viewOf is the run id as the run view shows it.
func (a *API) viewOf(id string) (RunView, error) {
	r, err := a.run(id)
	if err != nil {
		return RunView{}, err
	}
	ctx, cancel := a.ctx(requestTimeout)
	defer cancel()
	t := a.host.AgentTasks(ctx, []string{r.Task})
	return runView(r, t[r.Task].State), nil
}

// runResult shows run id after a command.
func (a *API) runResult(id string) (cli.Result, error) {
	v, err := a.viewOf(id)
	if err != nil {
		return cli.Result{}, err
	}
	return cli.Result{Text: runCLIText(v), JSON: v}, nil
}

// maxLogLines is how much of a run's log run show prints.
const maxLogLines = 10

func runCLIText(v RunView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s of %s: %s\n", v.ID, v.Workflow, v.Title)
	fmt.Fprintf(&b, "State:   %s\n", stateText(v))
	if v.Error != "" {
		fmt.Fprintf(&b, "Error:   %s\n", v.Error)
		if slices.Contains(v.Actions, ActionRecheck) {
			fmt.Fprintf(&b, "         Answer the agent in the step's chat in Flint, then: flint run check %s (or run retry, run continue)\n", v.ID)
		}
	}
	if v.Hold != nil {
		fmt.Fprintf(&b, "Limit:   %s\n", v.Hold.Reason)
	}
	fmt.Fprintf(&b, "Task:    %s (%s)\n", v.Task, v.TaskState)
	fmt.Fprintf(&b, "Folder:  %s\n", v.Dir)
	if len(v.Actions) > 0 {
		fmt.Fprintf(&b, "You can: %s\n", strings.Join(actionWords(v.Actions), ", "))
	}
	b.WriteString("\n" + stepTable(v.Steps, true))
	if len(v.Log) > 0 {
		b.WriteString("\nLog:\n")
		log := v.Log[max(len(v.Log)-maxLogLines, 0):]
		for _, e := range log {
			at := time.Unix(e.At, 0).Format("15:04:05")
			step := ""
			if e.Step != "" {
				step = e.Step + ": "
			}
			fmt.Fprintf(&b, "  %s  %s%s\n", at, step, strings.Join(strings.Fields(e.Text), " "))
		}
	}
	return b.String()
}

// actionWords names the actions as commands.
func actionWords(actions []string) []string {
	out := make([]string, len(actions))
	for i, act := range actions {
		switch act {
		case ActionSendBack:
			act = "send-back"
		case ActionRecheck:
			act = "check"
		case ActionSkip:
			act = "continue"
		}
		out[i] = act
	}
	return out
}

// waitResult is run wait's JSON.
type waitResult struct {
	TimedOut bool    `json:"timedOut"`
	Run      RunView `json:"run"`
}

// waitDone is when run wait stops waiting: the run needs the user, is
// paused (by the user, or by a limit that does not resume by itself) or is
// over. A run paused by a limit that resumes by itself is not: it keeps going
// without the user.
func waitDone(r Run) bool {
	selfResuming := r.State == StatePaused && r.Hold != nil && r.Hold.Until > 0
	return settled(r) && !selfResuming
}

// cliWait blocks like wait_for_workflow_run (engine.waitRun, settled) until
// the client goes away, Flint quits or --timeout passes.
func (a *API) cliWait(ctx context.Context, c *cli.Call) (cli.Result, error) {
	secs, err := c.Int("timeout", 0, 1, maxWaitCLI)
	if err != nil {
		return cli.Result{}, err
	}
	id := c.Arg(0)
	if _, err := a.run(id); err != nil {
		return cli.Result{}, err
	}
	wctx, cancel := context.WithCancel(ctx)
	if secs > 0 {
		wctx, cancel = context.WithTimeout(ctx, time.Duration(secs)*time.Second)
	}
	defer cancel()
	if base := a.engine.baseCtx(); base != nil {
		defer context.AfterFunc(base, cancel)()
	}
	_, err = a.engine.waitRun(wctx, id, waitDone)
	timedOut := false
	switch {
	case errors.Is(err, errNoRun):
		return cli.Result{}, errors.New("the run was removed")
	case err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		timedOut = true
	case err != nil && ctx.Err() == nil:
		return cli.Result{}, errors.New("Flint is quitting")
	case err != nil:
		return cli.Result{}, err
	}
	v, err := a.viewOf(id)
	if err != nil {
		return cli.Result{}, err
	}
	res := cli.Result{Text: runCLIText(v), JSON: waitResult{TimedOut: timedOut, Run: v}}
	switch {
	case timedOut:
		res.Exit = cli.ExitTimeout
	case v.State == StateDone:
	case v.State == StateStopped:
		res.Exit = cli.ExitFailed
	default:
		res.Exit = cli.ExitNeedsUser
	}
	return res, nil
}
