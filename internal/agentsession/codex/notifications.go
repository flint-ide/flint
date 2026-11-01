package codex

import (
	"encoding/json"
	"strings"

	"flint/internal/agentsession"
)

// handleNotification translates one app-server notification. It runs on the
// dispatcher goroutine, in wire order.
func (s *session) handleNotification(method string, params json.RawMessage) {
	var env envelope
	if !decode(params, &env) {
		return
	}
	if id := s.threadID(); env.ThreadID != "" && id != "" && env.ThreadID != id {
		return // a sub-agent thread
	}
	switch method {
	case notifTurnStarted:
		var n turnNotification
		if decode(params, &n) {
			s.onTurnStarted(n.Turn.ID)
		}
	case notifTurnCompleted:
		var n turnNotification
		if decode(params, &n) {
			s.onTurnCompleted(n.Turn)
		}
	case notifItemStarted, notifItemCompleted:
		var n itemNotification
		if decode(params, &n) {
			s.onItem(n.Item, method == notifItemCompleted)
		}
	default:
		s.handleStreamNotification(method, params)
	}
}

// handleStreamNotification handles deltas and turn-level updates.
func (s *session) handleStreamNotification(method string, params json.RawMessage) {
	switch method {
	case notifAgentMessageDelta, notifPlanDelta:
		var n deltaNotification
		if decode(params, &n) {
			s.messageChunk(n.ItemID, n.Delta)
		}
	case notifReasoningSummary, notifReasoningText:
		var n deltaNotification
		if decode(params, &n) {
			s.thoughtChunk(n.ItemID, n.Delta)
		}
	case notifReasoningPart:
		var n deltaNotification
		if decode(params, &n) && n.SummaryIndex > 0 {
			s.thoughtChunk(n.ItemID, "\n\n")
		}
	case notifCommandOutput:
		var n deltaNotification
		if decode(params, &n) {
			s.commandOutput(n.ItemID, n.Delta)
		}
	case notifPatchUpdated:
		var n patchUpdatedNotification
		if decode(params, &n) {
			s.patchUpdated(n.ItemID, n.Changes)
		}
	case notifPlanUpdated:
		var n planUpdatedNotification
		if decode(params, &n) {
			s.emit(agentsession.Event{Kind: agentsession.KindPlan, Plan: planEntries(n)})
		}
	default:
		s.handleStatusNotification(method, params)
	}
}

// handleStatusNotification handles usage, errors and resolved requests.
func (s *session) handleStatusNotification(method string, params json.RawMessage) {
	switch method {
	case notifTokenUsage:
		var n tokenUsageNotification
		if decode(params, &n) {
			s.onTokenUsage(n.TokenUsage.Total, n.TokenUsage.Last, n.TokenUsage.ModelContextWindow)
		}
	case notifError:
		var n errorNotification
		if decode(params, &n) && n.Error.Message != "" {
			s.onError(n.Error.Message, n.WillRetry)
		}
	case notifRequestResolved:
		var n requestResolvedNotification
		if decode(params, &n) && len(n.RequestID) > 0 {
			s.settle(requestKey(n.RequestID))
		}
	}
}

func (s *session) onTurnStarted(id string) {
	s.mu.Lock()
	t := s.turn
	s.mu.Unlock()
	if t != nil && id != "" {
		s.setTurnID(t, id)
	}
}

func (s *session) onTurnCompleted(ti turnInfo) {
	s.mu.Lock()
	t := s.turn
	mismatch := t != nil && t.id != "" && ti.ID != "" && t.id != ti.ID
	s.mu.Unlock()
	if t == nil || mismatch {
		return
	}
	switch ti.Status {
	case "interrupted":
		s.finishTurn(t, agentsession.StopCancelled, nil, "")
	case "failed":
		msg := "turn failed"
		if ti.Error != nil && ti.Error.Message != "" {
			msg = ti.Error.Message
		}
		s.finishTurn(t, agentsession.StopError, &TurnError{Message: msg}, msg)
	default:
		s.finishTurn(t, agentsession.StopEndTurn, nil, "")
	}
}

// onItem handles item/started and item/completed.
func (s *session) onItem(it threadItem, completed bool) {
	switch {
	case it.Type == "agentMessage" || it.Type == "plan":
		if completed && !s.wasStreamed(it.ID) {
			s.messageChunk(it.ID, it.Text)
		}
	case it.Type == "reasoning":
		if completed && !s.wasStreamed(it.ID) {
			text := strings.Join(it.Summary, "\n\n")
			if text == "" {
				text = strings.Join(it.Content, "\n\n")
			}
			s.thoughtChunk(it.ID, text)
		}
	case toolItemTypes[it.Type] && completed:
		s.toolCompleted(it)
	case toolItemTypes[it.Type]:
		s.toolStarted(it)
	}
}

// state returns the item's state, creating it. Caller holds s.mu.
func (s *session) state(id, itemType string) *itemState {
	st := s.items[id]
	if st == nil {
		st = &itemState{itemType: itemType}
		s.items[id] = st
	}
	return st
}

func (s *session) wasStreamed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.items[id]
	return st != nil && st.streamed
}

// messageChunk emits assistant text, separating consecutive messages.
func (s *session) messageChunk(itemID, text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	s.state(itemID, "agentMessage").streamed = true
	if t := s.turn; t != nil {
		if t.lastTextItem != "" && t.lastTextItem != itemID {
			text = "\n\n" + text
		}
		t.lastTextItem = itemID
	}
	s.mu.Unlock()
	s.emit(agentsession.Event{Kind: agentsession.KindMessageChunk, Text: text})
}

func (s *session) thoughtChunk(itemID, text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	s.state(itemID, "reasoning").streamed = true
	s.mu.Unlock()
	s.emit(agentsession.Event{Kind: agentsession.KindThoughtChunk, Text: text})
}

func (s *session) toolStarted(it threadItem) {
	tc := toolCallFor(s.cwd, it)
	s.mu.Lock()
	st := s.state(it.ID, it.Type)
	st.call, st.announce = tc, true
	s.mu.Unlock()
	s.emit(agentsession.Event{Kind: agentsession.KindToolCall, ToolCall: &tc})
}

func (s *session) toolCompleted(it threadItem) {
	tc := toolCallFor(s.cwd, it)
	if tc.Status == agentsession.StatusInProgress {
		tc.Status = agentsession.StatusCompleted
	}
	s.mu.Lock()
	st := s.items[it.ID]
	delete(s.items, it.ID)
	s.mu.Unlock()
	if st == nil || !st.announce {
		s.emit(agentsession.Event{Kind: agentsession.KindToolCall, ToolCall: &tc})
		return
	}
	if tc.Output == "" && st.output.Len() > 0 {
		tc.Output = truncateOutput(st.output.String())
	}
	if len(st.call.Diffs) > 0 {
		// Reconstructed before the patch landed; most reliable.
		tc.Diffs, tc.Output = st.call.Diffs, st.call.Output
	}
	s.emit(agentsession.Event{Kind: agentsession.KindToolCallUpdate, ToolCall: &tc})
}

func (s *session) commandOutput(itemID, delta string) {
	s.mu.Lock()
	st := s.items[itemID]
	if st == nil || !st.announce {
		s.mu.Unlock()
		return
	}
	st.streamed = true
	out := st.appendOutput(delta)
	s.mu.Unlock()
	s.emit(agentsession.Event{Kind: agentsession.KindToolCallUpdate, ToolCall: &agentsession.ToolCall{ID: itemID, Output: out}})
}

func (s *session) patchUpdated(itemID string, changes []fileUpdateChange) {
	var tc agentsession.ToolCall
	fillFileChange(&tc, s.cwd, changes, false)
	tc.ID, tc.Kind = itemID, ""
	s.mu.Lock()
	st := s.items[itemID]
	if st == nil || !st.announce {
		s.mu.Unlock()
		return
	}
	st.call.Diffs, st.call.Output, st.call.Paths, st.call.Title = tc.Diffs, tc.Output, tc.Paths, tc.Title
	s.mu.Unlock()
	s.emit(agentsession.Event{Kind: agentsession.KindToolCallUpdate, ToolCall: &tc})
}

// onTokenUsage takes the thread's total and the last model call. A turn
// whose starting total is unknown (the first turn after a resume, where the
// total includes the history) starts from the total before that call.
func (s *session) onTokenUsage(total, last tokenBreakdown, ctxWin int) {
	s.mu.Lock()
	s.usage, s.usageKnown = total, true
	s.ctxLast, s.ctxWin = last.InputTokens, ctxWin
	if t := s.turn; t != nil {
		if !t.baseKnown {
			t.usageBase, t.baseKnown = total.minus(last), true
		}
		t.usageSeen = true
	}
	s.mu.Unlock()
}

func (s *session) onError(msg string, willRetry bool) {
	text := msg
	if willRetry {
		text += " (retrying…)"
	} else {
		s.mu.Lock()
		if s.turn != nil {
			s.turn.lastError = msg
		}
		s.mu.Unlock()
	}
	s.emit(agentsession.Event{Kind: agentsession.KindError, Text: text})
}

// settle withdraws a request the server resolved on its own.
func (s *session) settle(key string) {
	s.mu.Lock()
	p := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if p != nil {
		s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: p.key})
	}
}

func planEntries(n planUpdatedNotification) []agentsession.PlanEntry {
	out := make([]agentsession.PlanEntry, 0, len(n.Plan))
	for _, step := range n.Plan {
		status := agentsession.StatusPending
		switch step.Status {
		case "inProgress":
			status = agentsession.StatusInProgress
		case "completed":
			status = agentsession.StatusCompleted
		}
		out = append(out, agentsession.PlanEntry{Content: step.Step, Status: status})
	}
	return out
}
