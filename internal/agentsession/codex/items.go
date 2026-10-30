package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"flint/internal/agentsession"
)

// maxOutputBytes bounds the tool output kept per tool call (the tail wins).
const maxOutputBytes = 16 << 10

const truncatedPrefix = "…(output truncated)\n"

// toolItemTypes are ThreadItem types rendered as tool calls.
var toolItemTypes = map[string]bool{
	"commandExecution": true, "fileChange": true, "mcpToolCall": true,
	"dynamicToolCall": true, "webSearch": true, "imageView": true,
	"collabAgentToolCall": true,
}

// shellWrapper matches the login-shell wrapper Codex puts around commands.
var shellWrapper = regexp.MustCompile(`^(?:\S*/)?(?:ba|z)?sh -l?c (.+)$`)

// displayCommand unwraps `/bin/zsh -lc 'cmd'` into `cmd` for display.
func displayCommand(cmd string) string {
	m := shellWrapper.FindStringSubmatch(strings.TrimSpace(cmd))
	if m == nil {
		return cmd
	}
	if inner, ok := unquoteShell(m[1]); ok {
		return inner
	}
	return m[1]
}

// unquoteShell decodes a single-quoted shell word, including the '\”
// idiom for embedded quotes.
func unquoteShell(s string) (string, bool) {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return "", false
	}
	body := strings.ReplaceAll(s[1:len(s)-1], `'\''`, "\x00")
	if strings.Contains(body, "'") {
		return "", false
	}
	return strings.ReplaceAll(body, "\x00", "'"), true
}

// truncateOutput keeps the last maxOutputBytes of s on a rune boundary.
func truncateOutput(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	cut := len(s) - maxOutputBytes
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return truncatedPrefix + s[cut:]
}

// itemState tracks one ThreadItem across its notifications.
type itemState struct {
	itemType string
	call     agentsession.ToolCall
	output   strings.Builder // streamed command output (bounded)
	streamed bool            // text/output deltas were received
	announce bool            // a KindToolCall was emitted
}

// appendOutput adds a delta, keeping the builder bounded.
func (st *itemState) appendOutput(delta string) string {
	st.output.WriteString(delta)
	if st.output.Len() > 2*maxOutputBytes {
		tail := truncateOutput(st.output.String())
		st.output.Reset()
		st.output.WriteString(tail)
	}
	return truncateOutput(st.output.String())
}

// toolCallFor maps a tool-like ThreadItem to the agent-neutral ToolCall.
func toolCallFor(cwd string, it threadItem) agentsession.ToolCall {
	tc := agentsession.ToolCall{ID: it.ID, Status: toolStatus(it)}
	switch it.Type {
	case "commandExecution":
		cmd := displayCommand(it.Command)
		tc.Kind, tc.Title, tc.Input = agentsession.ToolExecute, "Run: "+cmd, cmd
		tc.Paths = commandPaths(cwd, it.CommandActions)
		if it.AggregatedOutput != nil {
			tc.Output = truncateOutput(*it.AggregatedOutput)
		}
	case "fileChange":
		fillFileChange(&tc, cwd, it.Changes, it.Status == "completed")
	case "mcpToolCall", "dynamicToolCall":
		fillToolInvocation(&tc, it)
	case "webSearch":
		tc.Kind, tc.Title, tc.Input = agentsession.ToolFetch, "Search: "+it.Query, it.Query
	case "imageView":
		p := absPath(cwd, it.Path)
		tc.Kind, tc.Title, tc.Paths = agentsession.ToolRead, "View image "+filepath.Base(p), []string{p}
	default:
		tc.Kind, tc.Title = agentsession.ToolOther, "Agent: "+it.Tool
	}
	return tc
}

// toolStatus maps the item status; command exit codes refine "completed".
func toolStatus(it threadItem) string {
	switch it.Status {
	case "completed":
		if it.ExitCode != nil && *it.ExitCode != 0 {
			return agentsession.StatusFailed
		}
		if it.Success != nil && !*it.Success {
			return agentsession.StatusFailed
		}
		return agentsession.StatusCompleted
	case "failed", "declined":
		return agentsession.StatusFailed
	default: // "inProgress", or no status at all (webSearch, imageView)
		return agentsession.StatusInProgress
	}
}

func commandPaths(cwd string, actions []commandAction) []string {
	var out []string
	for _, a := range actions {
		if a.Type == "read" && a.Path != "" {
			out = append(out, absPath(cwd, a.Path))
		}
	}
	return out
}

// fillFileChange sets the edit title, paths and diffs. Changes whose old/new
// text cannot be reconstructed are rendered as unified diffs in Output.
func fillFileChange(tc *agentsession.ToolCall, cwd string, changes []fileUpdateChange, applied bool) {
	tc.Kind = agentsession.ToolEdit
	allDeleted := len(changes) > 0
	var fallback []string
	for _, ch := range changes {
		tc.Paths = append(tc.Paths, absPath(cwd, ch.Path))
		allDeleted = allDeleted && ch.Kind.Type == "delete"
		if d, ok := buildDiff(cwd, ch, applied); ok {
			tc.Diffs = append(tc.Diffs, d)
		} else {
			fallback = append(fallback, unifiedText(cwd, ch))
		}
	}
	if allDeleted {
		tc.Kind = agentsession.ToolDelete
	}
	tc.Output = truncateOutput(strings.Join(fallback, "\n"))
	tc.Title = editTitle(cwd, changes)
}

func editTitle(cwd string, changes []fileUpdateChange) string {
	switch len(changes) {
	case 0:
		return "Edit files"
	case 1:
		ch := changes[0]
		name := relPath(cwd, absPath(cwd, ch.Path))
		switch {
		case ch.Kind.Type == "add":
			return "Create " + name
		case ch.Kind.Type == "delete":
			return "Delete " + name
		case ch.Kind.MovePath != "":
			return "Move " + name + " → " + relPath(cwd, absPath(cwd, ch.Kind.MovePath))
		}
		return "Edit " + name
	default:
		return fmt.Sprintf("Edit %d files", len(changes))
	}
}

func relPath(cwd, p string) string {
	if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// fillToolInvocation handles MCP and dynamic tool calls.
func fillToolInvocation(tc *agentsession.ToolCall, it threadItem) {
	tc.Kind = agentsession.ToolOther
	tc.Title = it.Tool
	if it.Server != "" {
		tc.Title = it.Server + "." + it.Tool
	}
	tc.Input = prettyJSON(it.Arguments)
	var texts []string
	if it.Result != nil {
		texts = textContent(it.Result.Content)
	}
	texts = append(texts, textContent(it.ContentItems)...)
	if it.ItemError != nil && it.ItemError.Message != "" {
		texts = append(texts, it.ItemError.Message)
	}
	tc.Output = truncateOutput(strings.Join(texts, "\n"))
}

// textContent extracts the text of MCP content blocks / dynamic tool items.
func textContent(items []json.RawMessage) []string {
	var out []string
	for _, raw := range items {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &block) == nil && block.Text != "" {
			out = append(out, block.Text)
		}
	}
	return out
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") != nil {
		return string(raw)
	}
	return buf.String()
}
