package workflows

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

// outputLines is how many of a chat's last lines a step reads for its result.
const outputLines = 60

// The reasons a step fails with that the user reads in the run view.
const (
	reasonBlocked   = "The agent could not finish this step."
	reasonNoResult  = "The agent ended its turn without a result line."
	reasonReview    = "the review found problems"
	reasonNoVerdict = "The reviewer gave no verdict."
	reasonCanceled  = "the turn was cancelled"
	reasonClosed    = "the step's chat was closed"
	reasonRemoved   = "its agent task was removed"
	reasonNoChat    = "Flint does not know which chat took this step: read the task's chat, then choose Continue anyway or Retry"
)

// outcome is how a step start ended: passed, stopped by a limit (limit is
// its kind), or failed with a reason (and, with rework, may send the work
// back to the step's on-fail step).
type outcome struct {
	pass   bool
	limit  string
	reason string
	output string
	rework bool
	// hint is a line for the user after a failure (sandboxHint), kept out
	// of the feedback an agent gets.
	hint string
}

func passed(output string) outcome { return outcome{pass: true, output: output} }

func failed(reason, output string, rework bool) outcome {
	return outcome{reason: reason, output: output, rework: rework}
}

// failure is a failure with reason, or a limit when text says so.
func failure(reason, text, output string) outcome {
	if kind := LimitKind(text); kind != "" {
		return outcome{limit: kind, reason: reason, output: output}
	}
	return failed(reason, output, false)
}

// apply is the transition the outcome makes.
func (o outcome) apply(r Run, p policy, now int64) Run {
	switch {
	case o.pass:
		return r.Passed(o.output, now)
	case o.limit != "":
		return limited(r, o.limit, o.reason, p, now)
	}
	cur := r.Current
	r = r.Failed(o.reason, o.output, o.rework, p.maxAttempts, now)
	switch {
	case o.hint == "":
	case r.State == StateFailed && r.Error == o.reason:
		r.Error += "\n" + o.hint
	default: // sent back automatically: the agent's feedback stays the reason alone
		r = r.logged(now, cur, o.hint)
	}
	return r
}

// chatOutcome reads how an agent or review step's chat ended: the state of
// its turn and, for a turn that finished, its result line. A failed turn
// whose error or last lines name a limit is a limit; so is a finished turn
// without a result line whose last lines name the agent's limit (an agent
// may print it as ordinary text and end the turn).
func chatOutcome(kind Kind, res feature.ChatResult, out string) outcome {
	tail := lastLines(out, limitLines)
	switch res.State {
	case agenttask.Failed:
		return failure(cmp.Or(res.Error, "the agent failed"), res.Error+"\n"+tail, out)
	case agenttask.Idle:
		return failed(reasonCanceled, out, false)
	case agenttask.Finished:
	default:
		return failed(fmt.Sprintf("the agent's turn ended as %q", res.State), out, false)
	}
	word, _ := ParseResult(out)
	if word == "" && LimitInOutput(tail) == LimitAgent {
		return outcome{limit: LimitAgent, reason: tail, output: out}
	}
	// The one rule the first evaluation and "Check again" share.
	switch v := Decide(kind, word); {
	case v.Pass:
		return passed(out)
	default:
		return failed(v.Reason, out, v.Rework)
	}
}

// scriptOutcome reads how a script step's run configuration ended. A run
// the user stopped in its tab waits for the user instead of sending the
// work back: the user is already at it.
func scriptOutcome(res feature.RunResult) outcome {
	switch {
	case res.Stopped:
		return failed("it was stopped", res.Output, false)
	case res.Error != "":
		return failed(res.Error, res.Output, true)
	case res.Code != 0:
		return failed(fmt.Sprintf("it exited with code %d", res.Code), res.Output, true)
	}
	return passed(res.Output)
}

// sandboxHintText is the line after the failure of a sandboxed script: the
// first thing a new user meets is a tool that writes a cache or needs the
// network.
const sandboxHintText = "This step ran in a sandbox: it can write only in the run's folder and temp folders, and has no network. " +
	"If the command needs a cache folder or the network, add the folder or allow the network in Settings ▸ Workflows."

// sandboxHint is the line to show after a script step that ran sandboxed
// and exited with a non-zero code; "" otherwise (not sandboxed, passed,
// stopped by the user, or failed to start).
func sandboxHint(sandboxed bool, res feature.RunResult) string {
	if !sandboxed || res.Stopped || res.Error != "" || res.Code == 0 {
		return ""
	}
	return sandboxHintText
}

// takeChat takes an agent or review step: a new chat in the task's worktree
// gets the step's prompt, and the end of its turn is the step's end.
func (e *engine) takeChat(ctx context.Context, r Run) {
	id, exec, kind := r.ID, r.Exec, r.Steps[r.Current].Kind
	// A reviewer reads the implementer's worktree: it starts without that
	// worktree's project settings and instruction files, and asks for every
	// edit and command (feature.StepChatOptions).
	chat, err := e.host.StartStepChat(ctx, r.Task, r.StepAgent(), StepPrompt(r, true), feature.StepChatOptions{Reviewer: kind == KindReview})
	e.addChat(id, chat)
	if err != nil {
		if ctx.Err() != nil { // Flint quits, or the run was stopped
			e.dropChat(id, exec, chat)
			return
		}
		o := failure(err.Error(), err.Error(), "")
		_ = e.complete(id, exec, func(r Run, p policy, now int64) Run {
			if chat != "" {
				r = r.WithChat(chat)
			}
			return o.apply(r, p, now)
		})
		return
	}
	if e.complete(id, exec, func(r Run, _ policy, _ int64) Run { return r.WithChat(chat) }) != nil {
		e.dropChat(id, exec, chat)
		return
	}
	e.awaitChat(ctx, id, exec, chat, kind)
}

// awaitChat waits for the end of the step's chat turn and applies its
// outcome; a turn that ended without a result line is first asked to finish
// (nudge), and the next turn is waited for.
func (e *engine) awaitChat(ctx context.Context, id string, exec int, chat string, kind Kind) {
	complete := func(fn func(Run, policy, int64) Run) error { return e.complete(id, exec, fn) }
	for {
		res, err := e.host.WaitChat(ctx, chat)
		if err != nil {
			if ctx.Err() == nil {
				_ = e.complete(id, exec, failed(reasonClosed, "", false).apply)
			}
			return // Flint quits: the step is interrupted at the next start
		}
		o := chatOutcome(kind, res, e.host.ChatOutput(chat, outputLines))
		if r, ok := e.get(id); ok && r.Exec == exec && e.nudge(ctx, r, chat, o, complete) {
			continue
		}
		_ = e.complete(id, exec, o.apply)
		return
	}
}

// dropChat stops a chat that was opened for a step start the run no longer
// waits for (it was stopped meanwhile), so no agent works for nothing.
func (e *engine) dropChat(id string, exec int, chat string) {
	if chat != "" && !e.current(id, exec) {
		_ = e.host.StopChat(chat)
	}
}

// takeScript takes a script step: the run configuration runs in the task's
// worktree (read from the checkout, never the worktree), in the sandbox when
// workflows.sandbox is on, and its exit code is the step's end.
func (e *engine) takeScript(ctx context.Context, r Run) {
	id, exec := r.ID, r.Exec
	opts := scriptOptions(Section.Get(e.host.Settings()), userHome())
	res, err := e.host.RunConfiguration(ctx, r.Task, r.Steps[r.Current].Run, opts)
	if len(res.Changed) > 0 && e.current(id, exec) {
		// The script planted what a later step would run (an agent's
		// settings or hooks, its instructions, the .git file): nothing
		// more runs in this worktree, not even a rework.
		_ = e.stop(id, changedReason(res.Changed))
		return
	}
	if ctx.Err() != nil {
		return // stopped (Stop decides) or Flint quits (interrupted at the next start)
	}
	o := scriptOutcome(res)
	o.hint = sandboxHint(opts.Sandbox != nil, res)
	if err != nil {

		o = failed(err.Error(), res.Output, false)
	}
	_ = e.complete(id, exec, func(r Run, p policy, now int64) Run {
		if res.ID != "" {
			r = r.WithRunTab(res.ID)
		}
		return o.apply(r, p, now)
	})
}

// maxChangedNamed bounds the paths changedReason names.
const maxChangedNamed = 5

// changedReason is why a run stopped after its script changed what later
// steps run or read.
func changedReason(paths []string) string {
	named := paths
	if len(named) > maxChangedNamed {
		named = append(slices.Clone(named[:maxChangedNamed]), fmt.Sprintf("and %d more", len(paths)-maxChangedNamed))
	}
	return fmt.Sprintf("the script changed %s, which later steps would run: the run stopped", strings.Join(named, ", "))
}

// scriptOptions are how a script step runs under the settings s: in the
// sandbox, with its network and the user's folders (~ expanded with home),
// when workflows.sandbox is on.
func scriptOptions(s Settings, home string) feature.RunOptions {
	if s.Sandbox != SandboxOn {
		return feature.RunOptions{}
	}
	writable := make([]string, len(s.SandboxWritable))
	for i, dir := range s.SandboxWritable {
		writable[i] = expandHome(dir, home)
	}
	return feature.RunOptions{Sandbox: &feature.Sandbox{Network: s.SandboxNetwork, Writable: writable}}
}
