package claude

import (
	"encoding/json"

	"flint/internal/agentsession"
)

// translator holds the per-session state needed to turn the CLI's stream
// into agentsession events. Only the reader goroutine touches it.
type translator struct {
	// streamed records assistant message ids whose text/thinking already
	// arrived as stream_event deltas, so the complete "assistant" copy of
	// those blocks is not emitted twice.
	streamed map[string]bool
	curMsg   string
	tools    map[string]*toolState // by tool_use id, until its result
	tasks    taskList
	lastCost float64 // cumulative total_cost_usd of the previous result
}

type toolState struct {
	name   string
	input  json.RawMessage
	hidden bool // planning tools, shown as KindPlan
	call   agentsession.ToolCall
}

func newTranslator() translator {
	return translator{streamed: map[string]bool{}, tools: map[string]*toolState{}}
}

// handleLine dispatches one stdout line.
func (s *session) handleLine(line []byte) {
	var m inbound
	if err := json.Unmarshal(line, &m); err != nil {
		return // not JSON (stray output); ignore
	}
	switch m.Type {
	case msgStreamEvent:
		s.onStreamEvent(&m)
	case msgAssistant:
		s.onAssistant(&m)
	case msgUser:
		s.onUser(&m)
	case msgResult:
		s.onResult(line)
	case msgControlReq:
		s.onControlRequest(&m)
	case msgControlResp:
		s.onControlResponse(&m)
	case msgControlCancel:
		s.onControlCancel(m.RequestID)
	case msgSystem:
		if m.Subtype == subtypeInit && m.Model != "" {
			s.mu.Lock()
			s.model = m.Model
			s.mu.Unlock()
		}
	}
}

func (s *session) onStreamEvent(m *inbound) {
	if m.fromSubagent() {
		return
	}
	var ev streamEvent
	if json.Unmarshal(m.Event, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			s.tr.curMsg = ev.Message.ID
		}
	case "content_block_delta":
		if ev.Delta == nil {
			return
		}
		msgID := firstNonEmpty(m.APIMessageID, s.tr.curMsg)
		switch ev.Delta.Type {
		case "text_delta":
			if ev.Delta.Text != "" {
				s.tr.streamed[msgID] = true
				s.emit(agentsession.Event{Kind: agentsession.KindMessageChunk, Text: ev.Delta.Text})
			}
		case "thinking_delta":
			if ev.Delta.Thinking != "" {
				s.tr.streamed[msgID] = true
				s.emit(agentsession.Event{Kind: agentsession.KindThoughtChunk, Text: ev.Delta.Thinking})
			}
		}
	}
}

func (s *session) onAssistant(m *inbound) {
	if m.fromSubagent() {
		return
	}
	var msg apiMessage
	if json.Unmarshal(m.Message, &msg) != nil {
		return
	}
	streamed := s.tr.streamed[msg.ID]
	for _, b := range msg.blocks() {
		switch b.Type {
		case "text":
			if !streamed && b.Text != "" {
				s.emit(agentsession.Event{Kind: agentsession.KindMessageChunk, Text: b.Text})
			}
		case "thinking":
			if !streamed && b.Thinking != "" {
				s.emit(agentsession.Event{Kind: agentsession.KindThoughtChunk, Text: b.Thinking})
			}
		case "tool_use":
			s.onToolUse(b)
		}
	}
}

func (s *session) onToolUse(b contentBlock) {
	if b.ID == "" || s.tr.tools[b.ID] != nil {
		return
	}
	st := &toolState{name: b.Name, input: b.Input, hidden: isPlanTool(b.Name)}
	s.tr.tools[b.ID] = st
	if b.Name == toolTodoWrite {
		if plan, ok := todoPlan(b.Input); ok {
			s.emit(agentsession.Event{Kind: agentsession.KindPlan, Plan: plan})
		}
	}
	if st.hidden {
		return
	}
	st.call = flintTool(describeTool(b.ID, b.Name, b.Input, s.cwd), b.Name, s.cwd, s.flintMCP)
	call := st.call
	s.emit(agentsession.Event{Kind: agentsession.KindToolCall, ToolCall: &call})
}

func (s *session) onUser(m *inbound) {
	if m.fromSubagent() {
		return
	}
	var msg apiMessage
	if json.Unmarshal(m.Message, &msg) != nil {
		return
	}
	var results []contentBlock
	for _, b := range msg.blocks() {
		if b.Type == "tool_result" && b.ToolUseID != "" {
			results = append(results, b)
		}
	}
	for _, b := range results {
		var structured json.RawMessage
		if len(results) == 1 {
			structured = m.ToolUseResult
		}
		s.onToolResult(b, structured)
	}
}

func (s *session) onToolResult(b contentBlock, structured json.RawMessage) {
	st := s.tr.tools[b.ToolUseID]
	delete(s.tr.tools, b.ToolUseID)
	if st != nil && st.hidden {
		s.onPlanToolResult(st, b, structured)
		return
	}
	status := agentsession.StatusCompleted
	if b.IsError {
		status = agentsession.StatusFailed
	}
	upd := &agentsession.ToolCall{
		ID:     b.ToolUseID,
		Status: status,
		Output: truncate(toolResultText(b.Content), maxOutputBytes),
	}
	if st != nil && st.name == toolWrite && !b.IsError && len(st.call.Diffs) == 1 {
		if orig, ok := writeOriginal(structured); ok {
			d := st.call.Diffs[0]
			d.OldText = orig
			upd.Diffs = []agentsession.Diff{d}
		}
	}
	s.emit(agentsession.Event{Kind: agentsession.KindToolCallUpdate, ToolCall: upd})
}

func (s *session) onPlanToolResult(st *toolState, b contentBlock, structured json.RawMessage) {
	if b.IsError {
		return
	}
	changed := false
	switch st.name {
	case toolTaskCreate:
		changed = s.tr.tasks.created(st.input, structured)
	case toolTaskUpdate:
		changed = s.tr.tasks.updated(st.input)
	}
	if changed {
		s.emit(agentsession.Event{Kind: agentsession.KindPlan, Plan: s.tr.tasks.plan()})
	}
}

func (s *session) onResult(line []byte) {
	var r resultMsg
	if json.Unmarshal(line, &r) != nil {
		return
	}
	usage := s.tr.usage(&r)
	s.tr.streamed = map[string]bool{}

	s.mu.Lock()
	t := s.turn
	cancelled := t != nil && t.cancelled
	perms := s.perms
	s.perms = map[string]*pendingPerm{}
	s.mu.Unlock()

	s.settleAll(perms)
	if t == nil {
		return // a late result after the turn was abandoned
	}
	if usage != nil {
		s.emit(agentsession.Event{Kind: agentsession.KindUsage, Usage: usage})
	}
	stop, errText := mapResult(&r, cancelled)
	if errText != "" {
		s.emit(agentsession.Event{Kind: agentsession.KindError, Text: errText})
	}
	s.endTurn(t, stop, nil)
}

// mapResult derives the stop reason (and error text, if the turn failed).
func mapResult(r *resultMsg, cancelled bool) (stop, errText string) {
	if cancelled {
		return agentsession.StopCancelled, ""
	}
	if r.Subtype == subtypeSuccess && !r.IsError {
		reason := ""
		if r.StopReason != nil {
			reason = *r.StopReason
		}
		switch reason {
		case "max_tokens":
			return agentsession.StopMaxTokens, ""
		case "refusal":
			return agentsession.StopRefusal, ""
		}
		return agentsession.StopEndTurn, ""
	}
	return agentsession.StopError, r.errorText()
}

// usage computes the turn's consumption. total_cost_usd is cumulative for
// the process; model_usage_since_last_result (when present) is per turn.
func (tr *translator) usage(r *resultMsg) *agentsession.Usage {
	cost := 0.0
	if r.ModelUsageSinceLastResult != nil {
		for _, m := range r.ModelUsageSinceLastResult {
			cost += m.CostUSD
		}
	} else if r.TotalCostUSD >= tr.lastCost {
		cost = r.TotalCostUSD - tr.lastCost
	}
	if r.TotalCostUSD > 0 {
		tr.lastCost = r.TotalCostUSD
	}
	if r.Usage == nil && cost == 0 {
		return nil
	}
	u := &agentsession.Usage{CostUSD: cost}
	if r.Usage != nil {
		u.InputTokens = r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens
		u.OutputTokens = r.Usage.OutputTokens
		u.ContextTokens = u.InputTokens
		if n := len(r.Usage.Iterations); n > 0 {
			it := r.Usage.Iterations[n-1]
			u.ContextTokens = it.InputTokens + it.CacheCreationInputTokens + it.CacheReadInputTokens
		}
	}
	u.ContextWindow = contextWindow(r)
	return u
}

// contextWindow is the largest window among the models used (the main model
// dominates; sub-agent models are usually smaller or equal).
func contextWindow(r *resultMsg) int {
	win := 0
	for _, m := range []map[string]modelUsage{r.ModelUsage, r.ModelUsageSinceLastResult} {
		for _, mu := range m {
			win = max(win, mu.ContextWindow)
		}
	}
	return win
}

func (s *session) onControlRequest(m *inbound) {
	var req controlRequestBody
	if json.Unmarshal(m.Request, &req) != nil || m.RequestID == "" {
		return
	}
	if req.Subtype != subtypeCanUseTool {
		// hook callbacks, MCP routing, ...: not offered by this host.
		_ = s.write(outControlResponse{Type: msgControlResp, Response: outControlResult{
			Subtype: subtypeError, RequestID: m.RequestID, Error: "unsupported control request: " + req.Subtype,
		}})
		return
	}
	p := &pendingPerm{toolUseID: req.ToolUseID, input: req.Input, suggestions: req.PermissionSuggestion}
	s.mu.Lock()
	cancelled := s.turn != nil && s.turn.cancelled
	if !cancelled {
		s.perms[m.RequestID] = p
	}
	s.mu.Unlock()
	if cancelled {
		// The turn is being interrupted; refuse instead of asking the user.
		_ = s.write(outControlResponse{Type: msgControlResp, Response: outControlResult{
			Subtype: subtypeSuccess, RequestID: m.RequestID,
			Response: permissionDeny{Behavior: behaviorDeny, Message: rejectMessage},
		}})
		return
	}
	call := flintTool(describeTool(req.ToolUseID, req.ToolName, req.Input, s.cwd), req.ToolName, s.cwd, s.flintMCP)
	if st := s.tr.tools[req.ToolUseID]; st != nil && !st.hidden {
		call = st.call
	}
	s.emit(agentsession.Event{Kind: agentsession.KindPermission, Permission: &agentsession.PermissionRequest{
		ID:       m.RequestID,
		ToolCall: call,
		Options:  permissionOptions(len(req.PermissionSuggestion) > 0),
	}})
}

// permissionOptions are the answers offered for a can_use_tool request.
// "Always allow" needs a permission update suggested by the CLI.
func permissionOptions(canPersist bool) []agentsession.PermissionOption {
	opts := []agentsession.PermissionOption{{ID: agentsession.AllowOnce, Name: "Allow once", Kind: agentsession.AllowOnce}}
	if canPersist {
		opts = append(opts, agentsession.PermissionOption{ID: agentsession.AllowAlways, Name: "Always allow", Kind: agentsession.AllowAlways})
	}
	return append(opts, agentsession.PermissionOption{ID: agentsession.RejectOnce, Name: "Reject", Kind: agentsession.RejectOnce})
}

func (s *session) onControlResponse(m *inbound) {
	var r controlResponseBody
	if json.Unmarshal(m.Response, &r) != nil {
		return
	}
	s.mu.Lock()
	ch := s.ctlWait[r.RequestID]
	delete(s.ctlWait, r.RequestID)
	s.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

// onControlCancel handles the CLI withdrawing a can_use_tool request (e.g.
// after an interrupt).
func (s *session) onControlCancel(requestID string) {
	s.mu.Lock()
	_, ok := s.perms[requestID]
	delete(s.perms, requestID)
	s.mu.Unlock()
	if ok {
		s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: requestID})
	}
}
