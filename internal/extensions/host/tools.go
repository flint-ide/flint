package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"flint/internal/mcpserver"
)

// maxToolName is MCP's limit on a tool name.
const maxToolName = 64

// Tools are the agent tools of the enabled extensions for Flint's MCP
// server, named <short>_<name> (acme.jira's jira_issue: jira_jira_issue).
// offered says whether the user lets the extension's tools be offered.
// Listing them reads only manifests: an extension starts on its first call.
func (h *Host) Tools(offered func(id string) bool) []mcpserver.Tool {
	return h.tools(func(e *ext) bool { return h.o.App.Enabled(e.id) && offered(e.id) && e.state() != StateFailed })
}

// tools lists the agent tools of the extensions want accepts.
func (h *Host) tools(want func(*ext) bool) []mcpserver.Tool {
	h.ensureDiscovered()
	var out []mcpserver.Tool
	taken := map[string]bool{}
	for _, e := range h.all() {
		if e.m == nil || len(e.m.Contributes.AgentTools) == 0 || !want(e) {
			continue
		}
		short := toolPrefix(e.id)
		for _, t := range e.m.Contributes.AgentTools {
			name := uniqueName(short+"_"+t.Name, taken)
			taken[name] = true
			title := t.Title
			if title == "" {
				title = t.Name
			}
			out = append(out, mcpserver.Tool{
				Name:        name,
				Title:       e.m.Name + ": " + title,
				Description: t.Description + " (from the " + e.m.Name + " extension)",
				InputSchema: t.InputSchema,
				Call: func(ctx context.Context, root string, args json.RawMessage) (string, error) {
					return e.callTool(ctx, t.Name, root, args)
				},
			})
		}
	}
	return out
}

// ToolNames maps the extension's tool names to the names agents see (or
// would see, while it is off).
func (h *Host) ToolNames(id string) map[string]string {
	out := map[string]string{}
	for _, t := range h.tools(func(*ext) bool { return true }) {
		if prefix := toolPrefix(id) + "_"; strings.HasPrefix(t.Name, prefix) {
			out[strings.TrimPrefix(t.Name, prefix)] = t.Name
		}
	}
	return out
}

// toolPrefix is the extension's short name: "acme.jira-cloud" → "jira_cloud".
func toolPrefix(id string) string {
	_, name, _ := strings.Cut(id, ".")
	return strings.ReplaceAll(name, "-", "_")
}

// uniqueName cuts name to MCP's limit and numbers it when taken.
func uniqueName(name string, taken map[string]bool) string {
	base := name
	if len(base) > maxToolName {
		base = base[:maxToolName]
	}
	out := base
	for i := 2; taken[out]; i++ {
		suffix := "_" + strconv.Itoa(i)
		out = base[:min(len(base), maxToolName-len(suffix))] + suffix
	}
	return out
}

// callTool runs agentTool/call for an agent working in root.
func (e *ext) callTool(ctx context.Context, name, root string, args json.RawMessage) (string, error) {
	if !e.h.o.App.Trusted(root) {
		return "", errors.New("the project folder is not trusted (Restricted Mode)")
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var res struct {
		Text string `json:"text"`
	}
	params := map[string]any{"name": name, "root": root, "arguments": args}
	if err := e.call(ctx, e.h.o.ToolTimeout, "agentTool/call", params, &res); err != nil {
		return "", fmt.Errorf("%s: %w", e.name(), err)
	}
	return res.Text, nil
}
