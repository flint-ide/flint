package workflows

import (
	"errors"
)

// errChatNotOpen is the known limitation after a restart: an agent's chat
// (a custom ACP agent's, say) cannot be resumed, so there is nothing to read.
var errChatNotOpen = errors.New("The step's chat is not open any more (Flint was restarted): choose Retry to take the step again.")

// recheck is "Check again" for run id: it reads the end of the failed step's
// chat now and lets Run.Rechecked judge it. The chat is read without the
// engine's lock held, and the run must still be at the same start of the same
// step when the result is applied.
func (e *engine) recheck(id string) error {
	r, ok := e.get(id)
	if !ok {
		return errNoRun
	}
	if !r.Recheckable() {
		_, err := r.Rechecked("", 0, 0) // the reason
		return err
	}
	chat := r.Records[r.Current].Chat
	out := e.host.ChatOutput(chat, outputLines)
	if out == "" { // the host knows no such chat: it was open before Flint restarted
		return errChatNotOpen
	}
	p, now := e.policy(), e.now()
	// No result line, but the agent's provider announced its limit: a pause
	// by the limit, as when the turn first ended (chatOutcome).
	tail := lastLines(out, limitLines)
	word, _ := ParseResult(out)
	limit := word == "" && LimitInOutput(tail) == LimitAgent
	_, err := e.change(id, func(cur Run) (Run, error) {
		if cur.Recheckable() && (cur.Exec != r.Exec || cur.Current != r.Current || cur.Records[cur.Current].Chat != chat) {
			return cur, errors.New("the run moved on meanwhile: look at it again")
		}
		if limit && cur.Recheckable() {
			cur = cur.clone()
			cur.Records[cur.Current].Output = tailBytes(out, MaxOutput)
			return limited(cur.logged(now, cur.Current, "Checked again: the agent reached its usage limit"), LimitAgent, tail, p, now), nil
		}
		return cur.Rechecked(out, p.maxAttempts, now)
	})
	return err
}
