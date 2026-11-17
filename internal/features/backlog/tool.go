package backlog

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/mcpserver"
)

// ToolName is the read-only agent tool that shows the open backlogs.
const ToolName = "get_backlog"

// Bounds of get_backlog's answer (the MCP server cuts answers at 64 KB).
const (
	maxToolOutput = 60 << 10
	maxToolBody   = 2000 // bytes of one item's text
)

func (a *API) agentTools() []mcpserver.Tool {
	return []mcpserver.Tool{{
		Name:  ToolName,
		Title: "Read the backlog",
		Description: "Returns the backlogs open in Flint (folders of planned work items for one or more repositories) with their items: " +
			"id, title, status (doing, review, todo, done), priority, agent, repositories and text. " +
			"Use it to answer what is planned or what to work on next. Read-only: statuses change in Flint.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"status":{"type":"string","enum":["todo","doing","review","done"],"description":"Only items with this status."},` +
			`"backlog":{"type":"string","description":"Only the backlog with this name."}` +
			`},"additionalProperties":false}`),
		Call: a.callTool,
	}}
}

func (a *API) callTool(_ context.Context, _ string, args json.RawMessage) (string, error) {
	var in struct {
		Status  string `json:"status"`
		Backlog string `json:"backlog"`
	}
	if len(args) > 0 && string(args) != "null" {
		dec := json.NewDecoder(strings.NewReader(string(args)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return "", fmt.Errorf("invalid arguments: %v", err)
		}
	}
	if in.Status != "" && !slices.Contains(Statuses, in.Status) {
		return "", fmt.Errorf("status must be one of %s", strings.Join(Statuses, ", "))
	}
	return a.describe(in.Status, in.Backlog), nil
}

// describe renders the known backlogs as text for an agent, bounded.
func (a *API) describe(status, name string) string {
	var b strings.Builder
	folders := a.knownSet()
	shown := 0
	cut := 0
	for _, folder := range folders {
		l, err := Load(folder)
		if err != nil {
			if name == "" {
				fmt.Fprintf(&b, "# Backlog at %s\n\nCannot be read: %v\n\n", folder, err)
			}
			continue
		}
		if name != "" && l.Config.Name != name {
			continue
		}
		shown++
		fmt.Fprintf(&b, "# Backlog %q (%s)\n\n", l.Config.Name, folder)
		if len(l.Config.Repositories) > 0 {
			b.WriteString("Repositories:\n")
			for _, r := range l.Config.Repositories {
				fmt.Fprintf(&b, "- %s: %s\n", r.Name, r.URL)
			}
			b.WriteString("\n")
		}
		for _, it := range l.Items {
			if status != "" && it.Status != status {
				continue
			}
			entry := itemText(it)
			if b.Len()+len(entry) > maxToolOutput {
				cut++
				continue
			}
			b.WriteString(entry)
		}
		if len(l.Problems) > 0 && b.Len() < maxToolOutput-200 {
			fmt.Fprintf(&b, "%d file(s) of this backlog could not be read.\n\n", len(l.Problems))
		}
	}
	switch {
	case len(folders) == 0:
		return "No backlog is open in Flint."
	case shown == 0 && name != "":
		return fmt.Sprintf("No open backlog is named %q.", name)
	}
	if cut > 0 {
		fmt.Fprintf(&b, "(%d more item(s) not shown: the answer is limited to %d KB; ask for one status or one backlog.)\n", cut, maxToolOutput>>10)
	}
	return strings.TrimSpace(b.String())
}

func itemText(it Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s: %s\n", it.ID, it.Title)
	fmt.Fprintf(&b, "status: %s; priority: %s", it.Status, it.Priority)
	if it.Agent != "" {
		fmt.Fprintf(&b, "; agent: %s", it.Agent)
	}
	if len(it.Repos) > 0 {
		fmt.Fprintf(&b, "; repositories: %s", strings.Join(it.Repos, ", "))
	}
	b.WriteString("\n")
	if it.Body != "" {
		body := it.Body
		if len(body) > maxToolBody {
			body = body[:maxToolBody]
			for !utf8.ValidString(body) {
				body = body[:len(body)-1]
			}
			body += "\n[…text cut]"
		}
		b.WriteString("\n" + body + "\n")
	}
	b.WriteString("\n")
	return b.String()
}
