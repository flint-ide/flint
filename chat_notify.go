package main

import (
	"errors"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/orchestrate"
)

// Telling the parent: when an agent that a chat started waits for approval,
// fails or finishes, the chat gets one short message with the agent's last
// lines, so it need not poll (orchestration.notifyParent). The rules for which
// changes count and how notices merge are in internal/orchestrate (notice.go);
// this file is the wiring: noticing the change, and delivering through the same
// path as every other agent-sent message (promptChat), so the notice is
// redacted, passes the usage gate, and shows in the parent's transcript with an
// origin.

const (
	// noticeOrigin labels the notice in the parent's transcript.
	noticeOrigin = "Flint"
	// A notice is delivered when the parent's turn has ended, which the session
	// manager only knows a moment after the turn_end event; a delivery that
	// meets that gap tries again shortly. Bounded, and never a poll: it only
	// runs while a notice is waiting.
	noticeRetries = 10
	noticeRetryIn = 100 * time.Millisecond
)

// observeChat folds one session event into the registry and reacts to the state
// change it may have caused. It runs inside the session manager's event
// callback, which holds the manager's sequence lock, so it must never call back
// into the manager itself: deliveries run on their own goroutine.
func (a *App) observeChat(e as.Event) {
	n, prev, ok := a.orch.ObserveChange(e)
	if ok && prev != n.Live.State {
		a.chatChanged(n, prev)
	}
}

// chatChanged queues a notice for the chat's parent when the change is one the
// parent acts on, and delivers what is waiting for the chat itself when its
// turn just ended.
func (a *App) chatChanged(n orchestrate.Node, prev string) {
	turnEnded := agenttask.Busy(prev) && !agenttask.Busy(n.Live.State)
	// Whether Flint's own notice started the turn that just ended: its end
	// must not set the next notice off (orchestrate.ShouldNotify).
	noticeTurn := turnEnded && a.notices.EndTurn(n.ID)
	if n.Parent != "" && a.currentSettings().Orchestration.NotifyParent && orchestrate.ShouldNotify(prev, n.Live.State, noticeTurn) {
		a.notices.Add(n.Parent, orchestrate.Notice{
			Child: n.ID, Role: n.Role, State: n.Live.State, Output: a.orch.Output(n.ID, orchestrate.NoticeLines),
		})
		go a.flushNotices(n.Parent, 0)
	}
	if turnEnded {
		go a.flushNotices(n.ID, 0)
	}
}

// flushNotices delivers parent's waiting notices as one message if the chat can
// take it now: not working, not waiting for approval, no draft open. Otherwise
// they wait for the turn to end (chatChanged) or the draft to close
// (AgentChatAPI.DraftChanged).
func (a *App) flushNotices(parent string, attempt int) {
	if !a.notices.Pending(parent) {
		return
	}
	if _, known := a.orch.Get(parent); !known || !a.currentSettings().Orchestration.NotifyParent {
		a.notices.Take(parent)
		return
	}
	if a.orch.CheckSendable(parent) != nil {
		return
	}
	taken := a.notices.Take(parent)
	if len(taken) == 0 {
		return
	}
	origin := as.Origin{SessionID: taken[0].Child, Agent: noticeOrigin}
	a.notices.MarkTurn(parent) // before the turn can end, so the end finds the mark
	err := a.promptChat(parent, orchestrate.Compose(taken), nil, false, &origin)
	if err == nil {
		return
	}
	a.notices.EndTurn(parent) // no turn started
	a.notices.Requeue(parent, taken)
	if errors.Is(err, as.ErrBusy) && attempt < noticeRetries {
		time.AfterFunc(noticeRetryIn, func() { a.flushNotices(parent, attempt+1) })
		return
	}
	a.logf("orchestrate: notice for %s not delivered: %v", parent, err)
}
