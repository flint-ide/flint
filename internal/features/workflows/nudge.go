package workflows

import (
	"context"
	"fmt"
)

// Nudging. An agent or review step whose turn ends without a result line has
// usually stopped early: often it says it waits for work it left running in
// the background (a test run, a watcher, a monitor), which cannot report
// back to a turn that is over, or for which nothing runs at all. Before such
// a step waits for the user, Flint asks the same chat to finish, at most
// workflows.nudges times per attempt; each ask is a line in the run's log.

// nudgeText is Flint's message to a chat whose turn ended without a result
// line; kind is the step's (agent or review).
func nudgeText(kind Kind) string {
	verdict := `"RESULT: DONE" when the step is complete, or "RESULT: BLOCKED" after saying why you cannot finish it`
	if kind == KindReview {
		verdict = `"RESULT: PASS" when nothing must change, or "RESULT: FAIL" after listing what must change`
	}
	return "Your turn ended without a result line, so this workflow step cannot go on.\n" +
		"If you said you are waiting for something running in the background (tests, a build, a watcher or monitor), " +
		"check that it really still runs: a finished turn is not woken up by it. " +
		"Do what is left now with blocking commands in the foreground, then end your answer with one line, exactly: " +
		verdict + "."
}

// unanswered reports whether o is a finished turn that only lacks its result
// line (an agent's, or a reviewer's verdict).
func (o outcome) unanswered() bool {
	return !o.pass && o.limit == "" && (o.reason == reasonNoResult || o.reason == reasonNoVerdict)
}

// nudge asks chat, which took r's current step, to finish when o is
// unanswered and the attempt has asks left (workflows.nudges), and records
// the ask through complete. It reports whether it asked: the caller then
// waits for the chat's next turn instead of applying o.
func (e *engine) nudge(ctx context.Context, r Run, chat string, o outcome, complete func(func(Run, policy, int64) Run) error) bool {
	max := Section.Get(e.host.Settings()).Nudges
	if !o.unanswered() || chat == "" || !r.valid() || r.Records[r.Current].Nudges >= max {
		return false
	}
	if err := e.host.PromptStepChat(ctx, chat, nudgeText(r.Steps[r.Current].Kind)); err != nil {
		_ = complete(func(r Run, _ policy, now int64) Run {
			return r.logged(now, r.Current, fmt.Sprintf("Could not ask %s to finish: %v", r.title(r.Current), err))
		})
		return false
	}
	_ = complete(func(r Run, _ policy, now int64) Run { return r.Nudged(max, now) })
	return true
}
