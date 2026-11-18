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

// agentTools are get_backlog and, while the user has switched it on
// (mcpServer.actions.backlog), add_backlog_item.
func (a *API) agentTools() []mcpserver.Tool {
	tools := a.readTools()
	if a.host.Settings().MCPServer.Actions.Backlog {
		tools = append(tools, a.addTool())
	}
	return tools
}

func (a *API) readTools() []mcpserver.Tool {
	return []mcpserver.Tool{{
		Name:  ToolName,
		Title: "Read the backlog",
		Description: "Returns the backlogs open in Flint (folders of planned work items for one or more repositories) with their items: " +
			"id, title, status (doing, review, todo, done), priority, agent, repositories, the items it waits for (after) and whether it is ready, and text. " +
			"Use it to answer what is planned or what to work on next. Read-only: statuses change in Flint. " +
			"Backlogs whose folder is in Restricted Mode in Flint are left out.",
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

// noteRoom is kept free of describe's budget for its closing notes, so they
// are never cut by the MCP server's own limit.
const noteRoom = 512

// describe renders the known backlogs as text for an agent, bounded:
// everything it writes (headers, repositories, items, notes) counts against
// maxToolOutput. Backlogs whose folder is not trusted are left out (their
// text could steer the agent), and it says how many.
func (a *API) describe(status, name string) string {
	var b strings.Builder
	folders := a.knownSet()
	budget := maxToolOutput - noteRoom
	fits := func(s string) bool { return b.Len()+len(s) <= budget }
	shown, cut, untrusted := 0, 0, 0
	for _, folder := range folders {
		if !a.host.Trusted(folder) {
			untrusted++
			continue
		}
		l, err := Load(folder)
		if err != nil {
			if s := fmt.Sprintf("# Backlog at %s\n\nCannot be read: %v\n\n", folder, err); name == "" && fits(s) {
				b.WriteString(s)
			}
			continue
		}
		if name != "" && l.Config.Name != name {
			continue
		}
		shown++
		var items []Item
		for _, it := range l.Items {
			if status == "" || it.Status == status {
				items = append(items, it)
			}
		}
		if head := backlogHeader(l.Config, folder); fits(head) {
			b.WriteString(head)
		} else {
			cut += len(items)
			continue
		}
		deps := Dependencies(l.Items)
		for _, it := range items {
			if entry := itemText(it, deps[it.ID]); fits(entry) {
				b.WriteString(entry)
			} else {
				cut++
			}
		}
		if s := fmt.Sprintf("%d file(s) of this backlog could not be read.\n\n", len(l.Problems)); len(l.Problems) > 0 && fits(s) {
			b.WriteString(s)
		}
	}
	switch {
	case len(folders) == 0:
		return "No backlog is open in Flint."
	case shown == 0 && name != "":
		fmt.Fprintf(&b, "No open backlog is named %q.\n", name)
	}
	if cut > 0 {
		fmt.Fprintf(&b, "(%d more item(s) not shown: the answer is limited to %d KB; ask for one status or one backlog.)\n", cut, maxToolOutput>>10)
	}
	if untrusted > 0 {
		fmt.Fprintf(&b, "(%d backlog(s) left out: their folders are in Restricted Mode in Flint. The user can trust a backlog's folder in Flint to let agents read it.)\n", untrusted)
	}
	return strings.TrimSpace(b.String())
}

// backlogHeader is a backlog's heading and its repositories.
func backlogHeader(c Config, folder string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Backlog %q (%s)\n\n", c.Name, folder)
	if len(c.Repositories) > 0 {
		b.WriteString("Repositories:\n")
		for _, r := range c.Repositories {
			fmt.Fprintf(&b, "- %s: %s\n", r.Name, r.URL)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func itemText(it Item, d Deps) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s: %s\n", it.ID, it.Title)
	fmt.Fprintf(&b, "status: %s; priority: %s", it.Status, it.Priority)
	if it.Agent != "" {
		fmt.Fprintf(&b, "; agent: %s", it.Agent)
	}
	if len(it.Repos) > 0 {
		fmt.Fprintf(&b, "; repositories: %s", strings.Join(it.Repos, ", "))
	}
	if len(it.After) > 0 {
		fmt.Fprintf(&b, "; after: %s", strings.Join(it.After, ", "))
		switch {
		case d.Problem != "":
			fmt.Fprintf(&b, " (never ready: %s)", d.Problem)
		case len(d.WaitingFor) > 0:
			fmt.Fprintf(&b, " (not ready: waits for %s)", strings.Join(d.WaitingFor, ", "))
		default:
			b.WriteString(" (ready)")
		}
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
