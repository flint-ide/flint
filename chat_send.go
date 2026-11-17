package main

import (
	"fmt"

	as "flint/internal/agentsession"
)

// sendToChat sends text to a chat on behalf of another chat (the orchestration
// tools). It refuses, leaving the target untouched, when the target has a
// draft the user is typing, waits for approval, or is busy: the errors are
// orchestrate.ErrDraftOpen, ErrWaitingApproval and ErrBusy. Otherwise the text
// is redacted and goes through promptChat like any prompt, shown in the target
// as a message from `from`.
func (a *App) sendToChat(id, text string, from as.Origin) error {
	if err := a.orch.CheckSendable(id); err != nil {
		return err
	}
	return a.promptChat(id, text, nil, false, &from)
}

// DraftChanged records whether the user has an unfinished message in a chat's
// input. It only flips a flag that makes Flint refuse messages other agents
// send to that chat; the id must name a chat Flint knows.
func (c *AgentChatAPI) DraftChanged(sessionID string, open bool) error {
	if _, ok := c.app.orch.Get(sessionID); !ok {
		return fmt.Errorf("unknown chat %q", sessionID)
	}
	c.app.orch.SetDraft(sessionID, open)
	return nil
}
