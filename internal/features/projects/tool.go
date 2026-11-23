package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/mcpserver"
)

// ToolName is the read-only agent tool that shows the open projects.
const ToolName = "get_project"

// Bounds of get_project's answer (the MCP server cuts answers at 64 KB).
const (
	maxToolOutput = 60 << 10
	maxToolBody   = 2000 // bytes of one item's text
)

// agentTools are get_project and, while the user has switched them on,
// add_project_item (mcpServer.actions.projects) and delegate_project_item
// (mcpServer.actions.delegate, with the server on).
func (a *API) agentTools() []mcpserver.Tool {
	tools := a.readTools()
	mcp := a.host.Settings().MCPServer
	if mcp.Actions.Projects {
		tools = append(tools, a.addTool())
	}
	if mcp.Enabled && mcp.Actions.Delegate {
		tools = append(tools, a.delegateTool())
	}
	return tools
}

func (a *API) readTools() []mcpserver.Tool {
	return []mcpserver.Tool{{
		Name:  ToolName,
		Title: "Read the Flint projects",
		Description: "Returns the Flint projects the user has open (a Flint project is a folder of planned work items, its backlog, for one or more repositories; not the folder this chat works in) with their items: " +
			"id, title, status (doing, review, todo, done), priority, agent, repositories (and whether one agent works in all of them together), the workflow that takes it and the state of its runs, the items it waits for (after) and whether it is ready, and text. " +
			"Use it to answer what is planned or what to work on next. " +
			"Called from an agent working on a project's item, it returns that project first and in full, marks that item, and names the other projects (ask for one by name). " +
			"Read-only: statuses change in Flint. " +
			"Projects whose folder is in Restricted Mode in Flint are left out.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"status":{"type":"string","enum":["todo","doing","review","done"],"description":"Only items with this status."},` +
			`"project":{"type":"string","description":"Only the project with this name."}` +
			`},"additionalProperties":false}`),
		Call: a.callTool,
	}}
}

func (a *API) callTool(ctx context.Context, root string, args json.RawMessage) (string, error) {
	var in struct {
		Status  string `json:"status"`
		Project string `json:"project"`
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
	return a.describeFor(a.toolCaller(ctx, root), in.Status, in.Project), nil
}

// caller is who asks get_project: the item an agent task of the calling chat
// works on (from the task's origin), or else the folder the chat works in.
type caller struct {
	// Project and Item: the chat is an agent task of this item ("" when not).
	Project, Item string
	// Repos are the RepoKeys of the remotes of the chat's folder.
	Repos []string
}

// toolCaller finds out who calls: the chat comes from the MCP server
// (mcpserver.CallerFrom, the chat's token), its task's origin from the Host,
// and root is the chat's folder (also from its token).
func (a *API) toolCaller(ctx context.Context, root string) caller {
	var c caller
	if o, ok := parseOrigin(a.host.TaskOrigin(mcpserver.CallerFrom(ctx))); ok {
		c.Project, c.Item = o.Project, o.Item
	}
	if root != "" {
		if repo, err := a.host.Repository(ctx, root); err == nil {
			for _, u := range repo.Remotes {
				if k, ok := RepoKey(u); ok {
					c.Repos = append(c.Repos, k)
				}
			}
		}
	}
	return c
}

// noteRoom is kept free of describe's budget for its closing notes, so they
// are never cut by the MCP server's own limit.
const noteRoom = 512

// describe is get_project's answer for a caller that is no project's task.
func (a *API) describe(status, name string) string { return a.describeFor(caller{}, status, name) }

// describeFor renders the known projects as text for an agent, bounded:
// everything it writes (headers, repositories, items, notes) counts against
// maxToolOutput. Projects whose folder is not trusted are left out (their
// text could steer the agent), and it says how many.
//
// For the agent of a project's item (who.Project), without a name it is
// that project first and in full, the caller's item marked, and the other
// projects by name only; asking for a project by name returns that one.
// For another chat, the projects with a repository whose URL is a remote of
// the chat's folder come first.
func (a *API) describeFor(who caller, status, name string) string {
	var b strings.Builder
	folders := a.knownSet()
	trusted := func(f string) bool { return a.host.Trusted(f) }
	focus := ""
	if who.Project != "" && slices.Contains(folders, who.Project) && trusted(who.Project) {
		focus = who.Project
	}
	// The order: the caller's project, then the projects of the chat's
	// repository, then the others (each group in the known order).
	rank := func(f string) int {
		if f == focus {
			return 0
		}
		if len(who.Repos) > 0 {
			if c, err := LoadConfig(f); err == nil {
				for _, r := range c.Repositories {
					if k, ok := RepoKey(r.URL); ok && slices.Contains(who.Repos, k) {
						return 1
					}
				}
			}
		}
		return 2
	}
	slices.SortStableFunc(folders, func(x, y string) int { return rank(x) - rank(y) })
	budget := maxToolOutput - noteRoom
	fits := func(s string) bool { return b.Len()+len(s) <= budget }
	shown, cut, untrusted := 0, 0, 0
	var others []string // by name only
	for _, folder := range folders {
		if !trusted(folder) {
			untrusted++
			continue
		}
		l, err := Load(folder)
		if err != nil {
			if s := fmt.Sprintf("# Project at %s\n\nCannot be read: %v\n\n", folder, err); name == "" && focus == "" && fits(s) {
				b.WriteString(s)
			}
			continue
		}
		if name != "" && l.Config.Name != name {
			continue
		}
		if name == "" && focus != "" && folder != focus {
			others = append(others, l.Config.Name)
			continue
		}
		shown++
		if l.Config.OldFormat {
			s := projectHeader(l.Config, folder) + fmt.Sprintf("This project is in the old format (%s): Flint shows it read-only, and its items are not listed here until the user renames the file to %s in the Projects panel.\n\n", OldConfigFile, ConfigFile)
			if fits(s) {
				b.WriteString(s)
			}
			continue
		}
		var items []Item
		for _, it := range l.Items {
			if status == "" || it.Status == status || folder == focus && it.ID == who.Item {
				items = append(items, it)
			}
		}
		head := projectHeader(l.Config, folder)
		mine := ""
		if folder == focus {
			if i := slices.IndexFunc(l.Items, func(it Item) bool { return it.ID == who.Item }); i >= 0 {
				mine = who.Item
				head += fmt.Sprintf("You are working on the item %s: %s (marked below).\n\n", mine, l.Items[i].Title)
				// The caller's item first.
				slices.SortStableFunc(items, func(x, y Item) int { return boolRank(x.ID != mine) - boolRank(y.ID != mine) })
			}
		}
		if fits(head) {
			b.WriteString(head)
		} else {
			cut += len(items)
			continue
		}
		deps := Dependencies(l.Items)
		runs := a.itemRuns(folder)
		for _, it := range items {
			entry := itemText(it, deps[it.ID], runs[it.ID])
			if it.ID == mine {
				entry = strings.Replace(entry, "\n", " (you are working on this item)\n", 1)
			}
			if fits(entry) {
				b.WriteString(entry)
			} else {
				cut++
			}
		}
		if s := fmt.Sprintf("%d file(s) of this project could not be read.\n\n", len(l.Problems)); len(l.Problems) > 0 && fits(s) {
			b.WriteString(s)
		}
	}
	switch {
	case len(folders) == 0:
		return "The user has no Flint project open."
	case shown == 0 && name != "":
		fmt.Fprintf(&b, "No open project is named %q.\n", name)
	}
	if len(others) > 0 {
		list := strings.Join(quoteAll(others), ", ")
		if s := fmt.Sprintf("Other open projects (call %s with project: <name> to read one): %s\n", ToolName, list); fits(s) {
			b.WriteString(s)
		} else {
			fmt.Fprintf(&b, "(%d other open project(s) not listed: call %s with a project's name to read it.)\n", len(others), ToolName)
		}
	}
	if cut > 0 {
		fmt.Fprintf(&b, "(%d more item(s) not shown: the answer is limited to %d KB; ask for one status or one project.)\n", cut, maxToolOutput>>10)
	}
	if untrusted > 0 {
		fmt.Fprintf(&b, "(%d project(s) left out: their folders are in Restricted Mode in Flint. The user can trust a project's folder in Flint to let agents read it.)\n", untrusted)
	}
	return strings.TrimSpace(b.String())
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func quoteAll(v []string) []string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// projectHeader is a project's heading and its repositories.
func projectHeader(c Config, folder string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Project %q (%s)\n\n", c.Name, folder)
	if len(c.Repositories) > 0 {
		b.WriteString("Repositories:\n")
		for _, r := range c.Repositories {
			fmt.Fprintf(&b, "- %s: %s\n", r.Name, r.URL)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// itemText is one item for get_project; runs are the lines of its tasks'
// workflow runs (itemRuns).
func itemText(it Item, d Deps, runs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s: %s\n", it.ID, it.Title)
	fmt.Fprintf(&b, "status: %s; priority: %s", it.Status, it.Priority)
	if it.Agent != "" {
		fmt.Fprintf(&b, "; agent: %s", it.Agent)
	}
	if len(it.Repos) > 0 {
		fmt.Fprintf(&b, "; repositories: %s", strings.Join(it.Repos, ", "))
		if it.Together {
			b.WriteString(" (together: one agent works in all of them)")
		}
	}
	if it.Workflow != "" {
		fmt.Fprintf(&b, "; workflow: %s (a workflow run takes each of its tasks)", it.Workflow)
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
	if len(runs) > 0 {
		b.WriteString("runs: " + strings.Join(runs, "; ") + "\n")
	}
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

// maxRunsPerItem bounds the runs get_project shows per item: the newest.
const maxRunsPerItem = 5

// itemRuns are, per item of folder, the workflow runs that take its agent
// tasks, as "bug-fix · waiting for the user's approval (run <id>, api)":
// the state as it is now while the task exists, else as last seen. A
// coordinator sees where each item's runs stand without another call.
func (a *API) itemRuns(folder string) map[string][]string {
	a.mu.Lock()
	items := a.links()[folder]
	a.mu.Unlock()
	var ids []string
	for _, ls := range items {
		for _, l := range ls {
			if l.Workflow != "" && !l.Gone {
				ids = append(ids, l.Task)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := a.ctx(gitTimeout)
	tasks := a.host.AgentTasks(ctx, ids)
	cancel()
	out := map[string][]string{}
	for id, ls := range items {
		var lines []string
		for _, l := range ls {
			if l.Workflow == "" {
				continue
			}
			wf, run, state := l.Workflow, l.RunID, l.Run
			if t, ok := tasks[l.Task]; ok && !l.Gone && t.Run.Workflow != "" {
				wf, run, state = t.Run.Workflow, t.Run.ID, t.Run.State
			}
			where := l.Repo
			if where == "" {
				where = "the active folder"
			}
			if l.Gone {
				where += ", task removed"
			}
			lines = append(lines, fmt.Sprintf("%s · %s (run %s, %s)", wf, runStateText(state), run, where))
		}
		if n := len(lines); n > maxRunsPerItem {
			lines = append([]string{fmt.Sprintf("%d older run(s) not shown", n-maxRunsPerItem)}, lines[n-maxRunsPerItem:]...)
		}
		if len(lines) > 0 {
			out[id] = lines
		}
	}
	return out
}

// runStateText says a run's state for an agent; a run that waits for the
// user is said plainly, so the agent tells the user instead of acting.
func runStateText(state string) string {
	switch state {
	case runWaiting:
		return "waiting for the user's approval"
	case runFailed:
		return "failed, waits for the user"
	case "":
		return "state unknown"
	}
	return state
}
