package workflows

import (
	"context"
	"errors"
	"fmt"

	"flint/internal/cli"
)

// The command line's human decisions: run approve and run continue pass a
// step that only the user may pass. They are cli.Command.Human: whoever
// runs them, Flint asks in its own native dialog first (the web view
// cannot answer it), naming the run, the workflow, the step and the process
// that asked, and decides only after a yes for the same run at the same
// step (Question.Key).

// decision is what a human decision checks and asks.
type decision struct {
	title, ok string
	// state is the run's state the decision needs; refuse says why not.
	state, refuse string
}

var (
	approveDecision  = decision{title: "Approve this step?", ok: "Approve", state: StateWaiting, refuse: "the run is not waiting for approval"}
	continueDecision = decision{title: "Continue past the failed step?", ok: "Continue Anyway", state: StateFailed, refuse: "the run has not failed"}
)

// humanCommand is a human decision on the run named by its argument: d's
// question, then fn.
func (a *API) humanCommand(path, summary, help string, d decision, fn func(id string) error) cli.Command {
	return cli.Command{
		Path: path, Summary: summary, Help: help, Kind: cli.Acts, Args: []cli.Arg{{Name: "run", Max: 64}},
		Human: true,
		Ask:   func(_ context.Context, c *cli.Call) (cli.Question, error) { return a.question(c.Arg(0), d) },
		Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
			id := c.Arg(0)
			if err := fn(id); err != nil {
				return cli.Result{}, err
			}
			return a.runResult(id)
		},
	}
}

// question describes deciding d on run id now, or says why it cannot be
// decided.
func (a *API) question(id string, d decision) (cli.Question, error) {
	r, err := a.run(id)
	if err != nil {
		return cli.Question{}, err
	}
	if r.State != d.state || !r.valid() || d.state == StateWaiting && r.Steps[r.Current].Kind != KindGate {
		return cli.Question{}, errors.New(d.refuse)
	}
	step := r.Steps[r.Current]
	msg := fmt.Sprintf("Run: %s\nWorkflow: %s\nStep: %s", cut(r.Title, maxRunTitle), r.Workflow, step.Title)
	if d.state == StateFailed && r.Error != "" {
		msg += "\nIt failed: " + cut(r.Error, 300)
	}
	return cli.Question{
		Title: d.title, Message: msg, OK: d.ok,
		Key: fmt.Sprintf("%s/%s/%d/%d/%d", r.ID, r.State, r.Current, r.Exec, r.Records[r.Current].Attempts),
	}, nil
}
