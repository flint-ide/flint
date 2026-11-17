package orchestrate

import (
	"errors"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
)

// Why a message to a chat is refused. Each is distinct so a tool can tell the
// calling agent what to do: wait and retry, or give up.
var (
	// ErrDraftOpen: the user has an unfinished message in the chat's input.
	// Flint refuses instead of queueing so a message never lands under the
	// user's hands.
	ErrDraftOpen = errors.New("orchestrate: the user has a draft open in that chat; wait and retry")
	// ErrWaitingApproval: the chat waits for the user to approve something.
	ErrWaitingApproval = errors.New("orchestrate: that chat is waiting for the user's approval")
	// ErrBusy: the chat is working on a turn (the same error the session
	// manager returns, so both ways of finding out match).
	ErrBusy = as.ErrBusy
)

// CheckSendable reports why a message must not be sent to the chat now, or
// nil. The order says what the user must act on first: an approval, then a
// draft, then plain work in progress. The check and the send are not atomic;
// the session manager still refuses a prompt while a turn runs.
func (r *Registry) CheckSendable(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.nodes[id]
	if e == nil {
		return ErrUnknown
	}
	switch {
	case e.node.Live.State == agenttask.Waiting:
		return ErrWaitingApproval
	case e.node.Draft:
		return ErrDraftOpen
	case agenttask.Busy(e.node.Live.State):
		return ErrBusy
	}
	return nil
}
