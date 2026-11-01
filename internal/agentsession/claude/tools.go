package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/agentsession"
)

const (
	// maxOutputBytes bounds ToolCall.Output and ToolCall.Input.
	maxOutputBytes = 16 << 10
	// maxDiffFileBytes bounds how much of an existing file is read to show
	// the "before" side of a Write.
	maxDiffFileBytes = 2 << 20
	// maxTitleRunes bounds command lines shown in titles.
	maxTitleRunes = 100
)

// Planning tools are rendered as KindPlan instead of tool calls.
const (
	toolTodoWrite  = "TodoWrite"
	toolTaskCreate = "TaskCreate"
	toolTaskUpdate = "TaskUpdate"
	toolTaskList   = "TaskList"
	toolTaskGet    = "TaskGet"
	toolWrite      = "Write"
)

func isPlanTool(name string) bool {
	switch name {
	case toolTodoWrite, toolTaskCreate, toolTaskUpdate, toolTaskList, toolTaskGet:
		return true
	}
	return false
}

// toolInput is the union of the input fields of Claude Code's built-in tools.
type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Path         string `json:"path"`
	Pattern      string `json:"pattern"`
	Glob         string `json:"glob"`
	OldString    string `json:"old_string"`
	NewString    string `json:"new_string"`
	Content      string `json:"content"`
	NewSource    string `json:"new_source"`
	Edits        []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
	Command      string `json:"command"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	Query        string `json:"query"`
	SubagentType string `json:"subagent_type"`
	Plan         string `json:"plan"`
	Skill        string `json:"skill"`
}

// describeTool turns a tool_use (or a can_use_tool request) into a ToolCall
// with a kind, a human-readable title, absolute paths, input and diffs.
func describeTool(id, name string, rawInput json.RawMessage, cwd string) agentsession.ToolCall {
	var in toolInput
	_ = json.Unmarshal(rawInput, &in)
	tc := agentsession.ToolCall{
		ID:     id,
		Kind:   agentsession.ToolOther,
		Status: agentsession.StatusPending,
		Title:  name,
		Input:  prettyInput(rawInput),
	}
	abs := func(p string) string { return absPath(p, cwd) }
	rel := func(p string) string { return displayPath(p, cwd) }

	switch name {
	case "Read":
		tc.Kind = agentsession.ToolRead
		tc.Title = "Read " + rel(in.FilePath)
		tc.Paths = nonEmpty(abs(in.FilePath))
	case "Glob":
		tc.Kind = agentsession.ToolSearch
		tc.Title = "Find " + in.Pattern + inDir(in.Path, cwd)
		tc.Paths = nonEmpty(abs(in.Path))
	case "Grep":
		tc.Kind = agentsession.ToolSearch
		tc.Title = "Search " + quote(in.Pattern) + inDir(in.Path, cwd)
		tc.Paths = nonEmpty(abs(in.Path))
	case "LS":
		tc.Kind = agentsession.ToolSearch
		tc.Title = "List " + rel(in.Path)
		tc.Paths = nonEmpty(abs(in.Path))
	case "Edit":
		p := abs(in.FilePath)
		tc.Kind = agentsession.ToolEdit
		tc.Title = "Edit " + rel(in.FilePath)
		tc.Paths = nonEmpty(p)
		tc.Diffs = []agentsession.Diff{{Path: p, OldText: in.OldString, NewText: in.NewString}}
	case "MultiEdit":
		p := abs(in.FilePath)
		tc.Kind = agentsession.ToolEdit
		tc.Title = "Edit " + rel(in.FilePath)
		tc.Paths = nonEmpty(p)
		for _, e := range in.Edits {
			tc.Diffs = append(tc.Diffs, agentsession.Diff{Path: p, OldText: e.OldString, NewText: e.NewString})
		}
	case toolWrite:
		p := abs(in.FilePath)
		tc.Kind = agentsession.ToolEdit
		tc.Title = "Write " + rel(in.FilePath)
		tc.Paths = nonEmpty(p)
		tc.Diffs = []agentsession.Diff{{Path: p, OldText: readSmallFile(p), NewText: in.Content}}
		tc.Input = truncate(tc.Input, maxOutputBytes)
	case "NotebookEdit":
		p := abs(in.NotebookPath)
		tc.Kind = agentsession.ToolEdit
		tc.Title = "Edit " + rel(in.NotebookPath)
		tc.Paths = nonEmpty(p)
		tc.Diffs = []agentsession.Diff{{Path: p, NewText: in.NewSource}}
	case "Bash":
		tc.Kind = agentsession.ToolExecute
		tc.Title = "Run: " + clip(firstLine(in.Command), maxTitleRunes)
		tc.Input = in.Command
	case "BashOutput", "KillShell", "KillBash", "Monitor":
		tc.Kind = agentsession.ToolExecute
	case "WebFetch":
		tc.Kind = agentsession.ToolFetch
		tc.Title = "Fetch " + in.URL
	case "WebSearch":
		tc.Kind = agentsession.ToolFetch
		tc.Title = "Search the web: " + in.Query
	case "Task", "Agent":
		tc.Title = "Agent: " + firstNonEmpty(in.Description, in.SubagentType, name)
	case "ExitPlanMode":
		tc.Kind = agentsession.ToolThink
		tc.Title = "Plan ready for review"
		if in.Plan != "" {
			tc.Input = in.Plan
		}
	case "ToolSearch":
		tc.Title = "Load tools: " + in.Query
	case "Skill":
		tc.Title = "Skill: " + firstNonEmpty(in.Skill, name)
	default:
		if server, tool, ok := splitMCPName(name); ok {
			tc.Title = server + ": " + tool
		}
	}
	return tc
}

// flintTool reclassifies a call of Flint's own MCP server in a session that
// was given it (flintMCP): the tools that only show the project's own state
// (agentsession.MCPReadTools) become reads inside cwd, so the reads policy
// decides about them; terminal output and cluster status keep asking. In a
// session without Flint's server, a server named "flint" is someone else's
// and nothing changes.
func flintTool(tc agentsession.ToolCall, name, cwd string, flintMCP bool) agentsession.ToolCall {
	server, tool, ok := splitMCPName(name)
	if !flintMCP || !ok || server != agentsession.MCPServerName {
		return tc
	}
	tc.Title = "Flint: " + tool
	if slices.Contains(agentsession.MCPReadTools, tool) {
		tc.Kind, tc.Paths = agentsession.ToolRead, nonEmpty(cwd)
	}
	return tc
}

// splitMCPName splits "mcp__server__tool".
func splitMCPName(name string) (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(name, "mcp__")
	if !found {
		return "", "", false
	}
	server, tool, ok = strings.Cut(rest, "__")
	return server, tool, ok && server != "" && tool != ""
}

// absPath makes p absolute relative to cwd ("" stays "").
func absPath(p, cwd string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}

// displayPath shows p relative to cwd when it lies inside it.
func displayPath(p, cwd string) string {
	if p == "" {
		return "."
	}
	a := absPath(p, cwd)
	if r, err := filepath.Rel(cwd, a); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(r)
	}
	return a
}

func inDir(p, cwd string) string {
	if p == "" {
		return ""
	}
	return " in " + displayPath(p, cwd)
}

func nonEmpty(p string) []string {
	if p == "" {
		return nil
	}
	return []string{p}
}

func quote(s string) string { return `"` + s + `"` }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// clip shortens s to n runes.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// truncate bounds s to about n bytes on a UTF-8 boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… (truncated)"
}

// prettyInput indents a tool input for the details view.
func prettyInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return truncate(string(raw), maxOutputBytes)
	}
	return truncate(buf.String(), maxOutputBytes)
}

// readSmallFile returns the file's content, or "" if it does not exist, is
// not a regular file or is too large to diff.
func readSmallFile(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDiffFileBytes {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, maxDiffFileBytes))
	if err != nil {
		return ""
	}
	return string(data)
}

// toolResultText flattens tool_result content (a string or an array of
// text / tool_reference / image blocks).
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "tool_reference":
			parts = append(parts, "Loaded tool "+b.ToolName)
		case "image":
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, "\n")
}

// writeOriginal extracts the authoritative "before" text of a Write from its
// tool_use_result ({"type":"create"|"update","originalFile":...}).
func writeOriginal(raw json.RawMessage) (string, bool) {
	var r map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &r) != nil {
		return "", false
	}
	orig, ok := r["originalFile"]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(orig, &s) == nil {
		return s, true
	}
	return "", true // null: a new file
}

// Plans.

type todoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// todoPlan maps TodoWrite's input to a plan.
func todoPlan(raw json.RawMessage) ([]agentsession.PlanEntry, bool) {
	var in struct {
		Todos []todoItem `json:"todos"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Todos == nil {
		return nil, false
	}
	plan := make([]agentsession.PlanEntry, 0, len(in.Todos))
	for _, t := range in.Todos {
		plan = append(plan, agentsession.PlanEntry{Content: t.Content, Status: planStatus(t.Status), Priority: t.Priority})
	}
	return plan, true
}

func planStatus(s string) string {
	switch s {
	case agentsession.StatusInProgress, agentsession.StatusCompleted:
		return s
	}
	return agentsession.StatusPending
}

// taskItem is one entry of the TaskCreate/TaskUpdate task list.
type taskItem struct {
	id, subject, status string
}

// taskList mirrors the CLI's task list (TaskCreate / TaskUpdate tools) so it
// can be shown as a plan.
type taskList struct {
	items []taskItem
}

// created applies a successful TaskCreate (tool_use_result {"task":{"id","subject"}}).
func (l *taskList) created(input, result json.RawMessage) bool {
	var res struct {
		Task struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"task"`
	}
	var in struct {
		Subject string `json:"subject"`
	}
	_ = json.Unmarshal(input, &in)
	if json.Unmarshal(result, &res) != nil || res.Task.ID == "" {
		return false
	}
	subject := firstNonEmpty(res.Task.Subject, in.Subject)
	l.items = append(l.items, taskItem{id: res.Task.ID, subject: subject, status: agentsession.StatusPending})
	return true
}

// updated applies a successful TaskUpdate input ({"taskId","status","subject"}).
func (l *taskList) updated(input json.RawMessage) bool {
	var in struct {
		TaskID  string `json:"taskId"`
		Status  string `json:"status"`
		Subject string `json:"subject"`
	}
	if json.Unmarshal(input, &in) != nil || in.TaskID == "" {
		return false
	}
	for i, it := range l.items {
		if it.id != in.TaskID {
			continue
		}
		if in.Status == "deleted" {
			l.items = append(l.items[:i:i], l.items[i+1:]...)
			return true
		}
		if in.Status != "" {
			it.status = planStatus(in.Status)
		}
		if in.Subject != "" {
			it.subject = in.Subject
		}
		l.items[i] = it
		return true
	}
	return false
}

func (l *taskList) plan() []agentsession.PlanEntry {
	plan := make([]agentsession.PlanEntry, 0, len(l.items))
	for _, it := range l.items {
		plan = append(plan, agentsession.PlanEntry{Content: it.subject, Status: it.status})
	}
	return plan
}
