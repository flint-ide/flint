package agenttask

import (
	as "flint/internal/agentsession"
	"flint/internal/redact"
)

// Task states shown in the Tasks panel.
const (
	Idle     = "idle"     // no turn running (none started yet, cancelled or the chat closed)
	Working  = "working"  // the agent is on a turn
	Waiting  = "waiting"  // the agent waits for the user to approve something
	Finished = "finished" // the last turn ended normally: ready for review
	Failed   = "failed"   // the last turn ended with an error
	// Queued is shown for a task that waits for a free slot (queue.go); it
	// is never a chat's state.
	Queued = "queued"
	// Interrupted is remembered while a turn runs and shown when no chat of
	// the task is open: the turn was cut off (Flint quit or crashed, the
	// chat closed) before it ended. Never a chat's state.
	Interrupted = "interrupted"
)

// Busy reports a state that holds a slot of the queue: a turn is running.
func Busy(state string) bool { return state == Working || state == Waiting }

// maxError bounds the error text kept for a failed task.
const maxError = 300

// Live is one chat's state, derived from its session events.
type Live struct {
	State string `json:"state"`
	// Error is the last error the agent reported (redacted, shortened).
	Error string `json:"error,omitempty"`
	// Pending counts permission requests still waiting for an answer.
	Pending int `json:"-"`
	// Turn is true between a prompt and its turn_end.
	Turn bool `json:"-"`
}

// Prompted is the state right after the user sent a message.
func Prompted(Live) Live { return Live{State: Working, Turn: true} }

// Next folds one session event into l. Work outside a turn (the history a
// resumed chat replays) changes nothing; a non-fatal error mid-turn is
// overtaken by the work that follows it.
func Next(l Live, e as.Event) Live {
	switch e.Kind {
	case as.KindPermission:
		l.Pending++
		l.State = Waiting
	case as.KindPermissionSettled:
		l.Pending = max(0, l.Pending-1)
		if l.Pending > 0 {
			l.State = Waiting
		} else if l.Turn {
			l.State = Working
		}
	case as.KindTurnEnd:
		l.Turn, l.Pending = false, 0
		switch e.StopReason {
		case as.StopEndTurn:
			l.State, l.Error = Finished, ""
		case as.StopCancelled:
			l.State, l.Error = Idle, ""
		default:
			l.State = Failed
			if l.Error == "" {
				l.Error = "the agent stopped: " + e.StopReason
			}
		}
	case as.KindError:
		l.State, l.Error = Failed, shorten(redact.Secrets(e.Text))
	case as.KindMessageChunk, as.KindThoughtChunk, as.KindToolCall, as.KindToolCallUpdate, as.KindPlan:
		if !l.Turn {
			break
		}
		l.Error = ""
		if l.Pending > 0 {
			l.State = Waiting
		} else {
			l.State = Working
		}
	}
	return l
}

func shorten(s string) string {
	if r := []rune(s); len(r) > maxError {
		return string(r[:maxError]) + "…"
	}
	return s
}

// urgency orders states for a task with several chats: what needs the user
// most is shown.
var urgency = map[string]int{Waiting: 5, Working: 4, Failed: 3, Finished: 2, Idle: 1}

// Combine is a task's state from its chats; with none, the state remembered
// from before (finished, failed or interrupted), else idle.
func Combine(chats []Live, remembered string) Live {
	best := Live{State: Idle}
	if remembered == Finished || remembered == Failed || remembered == Interrupted {
		best.State = remembered
	}
	if len(chats) == 0 {
		return best
	}
	best = chats[0]
	for _, l := range chats[1:] {
		if urgency[l.State] > urgency[best.State] {
			best = l
		}
	}
	return Live{State: best.State, Error: best.Error}
}

// Settled is the state worth remembering across a restart. A running turn
// does not survive one, so it is remembered as interrupted from its start:
// a crash or a kill leaves no chance to save it later, and the turn's end
// replaces it.
func Settled(state string) string {
	switch state {
	case Finished, Failed:
		return state
	case Working, Waiting:
		return Interrupted
	}
	return ""
}
