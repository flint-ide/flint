package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/feature"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
	"flint/internal/prompts"
)

// DelegateToolName is the acting agent tool that delegates a ready item:
// the coordinator above a project's repositories hands its items to their
// agents or workflows, through the same function as the Delegate click
// (delegate), each call with the user's yes to that one call.
//
// What it can never do: delegate an item that waits for others (nor ask to
// start it when it is ready), delegate from a project whose folder is not
// trusted, delegate from inside handed-out work (an agent task's chat or
// worktree, a workflow step: no recursion), reach a project its folder is
// not in, or delegate more than maxDelegationsPerChat items per chat.
const DelegateToolName = mcptools.ToolProjectDelegate

// Bounds of delegate_project_item.
const (
	// maxDelegationsPerChat bounds the items one chat delegates;
	// maxDelegateCounters the chats counted (then the one with the fewest
	// delegations below the limit is forgotten: roomForDelegator).
	maxDelegationsPerChat = 10
	maxDelegateCounters   = 1000
	// maxChatInLog bounds the chat's id in "delegated by" (bytes).
	maxChatInLog = 80
)

// errHandedWork refuses a chat that works on handed-out work.
var errHandedWork = errors.New("an agent that was handed work cannot hand out work: " + DelegateToolName +
	" is for a chat in a workspace folder, not for an agent task's chat or worktree or a workflow step")

// delegateTool is delegate_project_item, offered while the MCP server and
// Settings ▸ Agent Context ▸ Actions ▸ "Delegate project items"
// (mcpServer.actions.delegate) are on. It acts: Flint asks the user before
// each call, showing what will start where.
func (a *API) delegateTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:  DelegateToolName,
		Title: "Delegate a project item",
		Description: "Delegates one READY item of a Flint project this folder is in (the project's folder is this folder or inside it, or the project lists this folder's repository), " +
			"exactly as the user's Delegate click does: Flint makes the item's agent tasks (a branch and a worktree in every repository it names) and its agent, or the workflow it names, takes each; the item's status becomes doing. " +
			"Read the project with " + ToolName + " first: an item that waits for other items (not ready) is refused, and so is a project whose folder is in Restricted Mode. " +
			"The user is asked before each call. Follow a workflow's runs with " + mcptools.ToolWorkflowWait + ": an approval step waits for the user, and you cannot approve it. " +
			fmt.Sprintf("One chat delegates at most %d items.", maxDelegationsPerChat),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"project":{"type":"string","maxLength":4096,"description":"The project: its folder or its name, as get_project shows them. Default: the one project this folder is in that has the item."},` +
			`"item":{"type":"string","maxLength":200,"description":"The item's id, as get_project shows it."}` +
			`},"required":["item"],"additionalProperties":false}`),
		Acts:    true,
		Plan:    a.planDelegate,
		Timeout: delegateTimeout + gitTimeout,
	}
}

type delegateArgs struct {
	Project string `json:"project"`
	Item    string `json:"item"`
}

// decodeDelegate decodes the arguments strictly: unknown ones are refused.
func decodeDelegate(raw json.RawMessage) (delegateArgs, error) {
	var in delegateArgs
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return delegateArgs{}, fmt.Errorf("invalid arguments: %v", err)
	}
	if dec.More() {
		return delegateArgs{}, errors.New("invalid arguments: more than one object")
	}
	switch {
	case in.Item == "":
		return delegateArgs{}, errors.New("item is required: the item's id, as get_project shows it")
	case !prompts.ValidName(in.Item):
		return delegateArgs{}, errors.New("invalid item id: use the id get_project shows")
	case len(in.Project) > 4096:
		return delegateArgs{}, errors.New("invalid arguments: project is a folder or a name")
	}
	return in, nil
}

// delegator is the chat that delegates: its id (mcpserver.CallerFrom), its
// folder (the token's workspace folder) and where that folder is.
type delegator struct {
	chat, root string
	place      place
}

// counter is what the per-chat count of delegations is kept by.
func (d delegator) counter() string {
	if d.chat != "" {
		return d.chat
	}
	return "folder:" + d.root
}

// by is who delegates, for the tasks' origin and the runs' log.
func (d delegator) by() string {
	chat := d.chat
	if chat == "" {
		chat = "unknown"
	}
	return fmt.Sprintf("an agent in %s (chat %s)", d.root, cutBytes(chat, maxChatInLog))
}

// place is a workspace folder as projects see it: the folder (resolved)
// and the RepoKeys of the remotes of its checkout.
type place struct {
	dir  string
	keys []string
}

// placeOf describes dir; worktree is true for an agent task's worktree.
func (a *API) placeOf(ctx context.Context, dir string) (p place, worktree bool) {
	p.dir = filepath.Clean(dir)
	if real, err := Resolve(dir); err == nil {
		p.dir = real
	}
	repo, err := a.host.Repository(ctx, dir)
	if err != nil {
		return p, false
	}
	for _, u := range repo.Remotes {
		if k, ok := RepoKey(u); ok && !slices.Contains(p.keys, k) {
			p.keys = append(p.keys, k)
		}
	}
	return p, repo.TaskWorktree
}

// projectsIn are the known projects whose folder is trusted that p is "in":
// the project's folder is p's folder or inside it, or p's checkout has a
// remote that is one of the project's repositories (the projects get_project
// puts first for a chat there).
func (a *API) projectsIn(p place) []string {
	var out []string
	for _, f := range a.knownSet() {
		if !a.host.Trusted(f) {
			continue
		}
		if p.dir != "" && inside(f, p.dir) {
			out = append(out, f)
			continue
		}
		if len(p.keys) == 0 {
			continue
		}
		c, err := LoadConfig(f)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(c.Repositories, func(r RepositoryDecl) bool {
			k, ok := RepoKey(r.URL)
			return ok && slices.Contains(p.keys, k)
		}) {
			out = append(out, f)
		}
	}
	return out
}

// delegator checks who calls: a trusted workspace folder, a chat that is
// no agent task's (an item's agent or a workflow step) and does not work in
// a task's worktree, and that has room for one more delegation.
func (a *API) delegator(ctx context.Context, root string) (delegator, error) {
	if root == "" || !filepath.IsAbs(root) {
		return delegator{}, errors.New("Flint cannot tell which folder this chat works in")
	}
	if !a.host.Trusted(root) {
		return delegator{}, errors.New("this chat's folder is in Restricted Mode in Flint: agents cannot delegate from there")
	}
	d := delegator{chat: mcpserver.CallerFrom(ctx), root: filepath.Clean(root)}
	if d.chat != "" && a.host.TaskOrigin(d.chat) != "" {
		return delegator{}, errHandedWork
	}
	p, worktree := a.placeOf(ctx, root)
	if worktree {
		return delegator{}, errHandedWork
	}
	d.place = p
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.delegated[d.counter()] >= maxDelegationsPerChat {
		return delegator{}, fmt.Errorf("this chat delegated %d items, the most one chat may: the user can Delegate more in Flint", maxDelegationsPerChat)
	}
	return d, nil
}

// delegateProject finds the project of the call among those the caller is
// in: the one named (by folder or name, trusted, as add_project_item finds
// it), else the one that has the item.
func (a *API) delegateProject(d delegator, in delegateArgs) (string, error) {
	mine := a.projectsIn(d.place)
	if ref := strings.TrimSpace(in.Project); ref != "" {
		if real, err := Resolve(ref); err == nil && filepath.IsAbs(ref) && slices.Contains(a.knownSet(), real) && !a.host.Trusted(real) {
			return "", errAgentUntrusted
		}
		real, _, err := a.toolProject(ref)
		if err != nil {
			return "", err
		}
		if !slices.Contains(mine, real) {
			return "", fmt.Errorf("this chat's folder is not in the project %q: an agent delegates only items of a project whose folder is its folder or inside it, or that lists its repository", in.Project)
		}
		return real, nil
	}
	var has []string
	for _, f := range mine {
		if _, _, err := ReadItemFile(f, in.Item); err == nil {
			has = append(has, f)
		}
	}
	switch len(has) {
	case 0:
		return "", fmt.Errorf("no project this chat's folder is in has an item %q: call %s for the projects and their items", in.Item, ToolName)
	case 1:
		return has[0], nil
	}
	return "", fmt.Errorf("several projects this chat's folder is in have an item %q: pass project (%s)", in.Item, strings.Join(has, ", "))
}

// errNotReady refuses an item that waits for others.
func errNotReady(id string, waitingFor []string) error {
	return fmt.Errorf("the item %s is not ready: it waits for %s, which must be done first. An agent cannot delegate it now nor ask Flint to start it when it is ready: leave it, or tell the user",
		id, strings.Join(waitingFor, ", "))
}

// planDelegate checks a delegate_project_item call with every check of
// Delegate, and of the run of the workflow the item names, and says what
// the user approves.
func (a *API) planDelegate(ctx context.Context, root string, raw json.RawMessage) (mcpserver.Planned, error) {
	in, err := decodeDelegate(raw)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	d, err := a.delegator(ctx, root)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	real, err := a.delegateProject(d, in)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	del, notReady, err := a.prepare(real, in.Item, d.root, nil, d.by())
	if err != nil {
		return mcpserver.Planned{}, err
	}
	if notReady != nil {
		return mcpserver.Planned{}, errNotReady(in.Item, notReady.WaitingFor)
	}
	if !a.hasRoom(real, in.Item, len(del.reqs)) {
		return mcpserver.Planned{}, fmt.Errorf("the item has %d agent tasks: the user can remove some in the Agent Tasks panel", MaxTasksPerItem)
	}
	// The workflow is checked as its start will check it, before the user
	// is asked: it exists for each checkout, its agents are available, and
	// script steps without a sandbox are refused (never asked) until the
	// user ran such a workflow in that checkout.
	for _, r := range del.reqs {
		if r.Workflow == "" {
			continue
		}
		if err := a.host.CheckTaskWorkflow(ctx, r); err != nil {
			return mcpserver.Planned{}, fmt.Errorf("the item's workflow %q cannot run: %w", r.Workflow, err)
		}
	}
	return mcpserver.Planned{
		Summary: delegateSummary(real, d.root, del),
		Run:     func(ctx context.Context) (string, error) { return a.agentDelegate(ctx, root, in, real) },
	}, nil
}

// hasRoom reports whether item id of folder can link n more agent tasks.
func (a *API) hasRoom(folder, id string, n int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, room := addLinks(a.links()[folder][id], make([]Link, n))
	return room
}

// agentDelegate delegates the item after the user's yes, through delegate
// (every check again). The chat's delegation is counted before, so two
// calls at once cannot pass the limit, and taken back when nothing started.
func (a *API) agentDelegate(ctx context.Context, root string, in delegateArgs, planned string) (string, error) {
	d, err := a.delegator(ctx, root)
	if err != nil {
		return "", err
	}
	real, err := a.delegateProject(d, in)
	if err != nil {
		return "", err
	}
	if real != planned {
		return "", errors.New("the project changed while the user was asked: try again")
	}
	key := d.counter()
	a.mu.Lock()
	if a.delegated[key] >= maxDelegationsPerChat {
		a.mu.Unlock()
		return "", fmt.Errorf("this chat delegated %d items, the most one chat may", maxDelegationsPerChat)
	}
	if _, ok := a.delegated[key]; !ok && !roomForDelegator(a.delegated) {
		a.mu.Unlock()
		return "", fmt.Errorf("%d chats delegated items since Flint started: the user can Delegate more in Flint", maxDelegateCounters)
	}
	a.delegated[key]++
	a.mu.Unlock()
	res, err := a.delegate(real, in.Item, d.root, nil, d.by())
	if err == nil && res.NotReady {
		err = errNotReady(in.Item, res.WaitingFor)
	}
	if err != nil {
		a.mu.Lock()
		if a.delegated[key] > 0 {
			a.delegated[key]--
		}
		a.mu.Unlock()
		return "", err
	}
	return delegatedText(in.Item, res), nil
}

// roomForDelegator makes room for one more chat in counts (at most
// maxDelegateCounters): it forgets the chat with the fewest delegations,
// never one at maxDelegationsPerChat, so a chat that used its delegations
// cannot get them back by making the map overflow. False when every counted
// chat is at the limit. Callers hold a.mu.
func roomForDelegator(counts map[string]int) bool {
	if len(counts) < maxDelegateCounters {
		return true
	}
	victim, least := "", maxDelegationsPerChat
	for k, n := range counts {
		if n < least {
			victim, least = k, n
		}
	}
	if victim == "" {
		return false
	}
	delete(counts, victim)
	return true
}

// delegatedText is the answer to the agent: the tasks and runs it can
// follow.
func delegatedText(id string, res DelegateResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Delegated the item %s (%s): its status is %s.\n", id, res.Item.Title, res.Item.Status)
	var runs []string
	for _, t := range res.Tasks {
		where := t.Repo
		if where == "" {
			where = "the active folder"
		}
		fmt.Fprintf(&b, "- agent task %s in %s (%s)", t.ID, where, t.State)
		if t.Run != "" {
			fmt.Fprintf(&b, ": workflow run %s of %q (%s)", t.Run, t.Workflow, t.RunState)
			runs = append(runs, t.Run)
		}
		b.WriteString("\n")
	}
	if len(runs) > 0 {
		fmt.Fprintf(&b, "Follow the runs with %s {\"run\": \"<id>\"}. When one waits at an approval step, tell the user: only they can approve it.\n", mcptools.ToolWorkflowWait)
	}
	if res.Warning != "" {
		b.WriteString("Warning: " + res.Warning + "\n")
	}
	return strings.TrimSpace(b.String())
}

// delegateSummary is what the user approves: the project, the item's id
// and title, its agent or workflow, each repository with the folder the
// task will be made in, whether one agent works in all of them, and the
// beginning of the item's text.
func delegateSummary(folder, root string, d delegation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Delegate the item %s (%q) of the project %q (%s), as your Delegate click would.\n", d.it.ID, d.it.Title, d.c.Name, folder)
	if d.it.Workflow != "" {
		fmt.Fprintf(&b, "Workflow: %s (a run takes each agent task; steps that name no agent: %s). Approval steps wait for you; the agent cannot approve them.\n", d.it.Workflow, d.agent)
	} else {
		fmt.Fprintf(&b, "Agent: %s\n", d.agent)
	}
	if len(d.it.Repos) == 0 {
		fmt.Fprintf(&b, "Folder: a new agent task (branch and worktree) from %s (the item names no repository: this chat's folder)\n", root)
	} else {
		b.WriteString("Repositories (a new agent task, branch and worktree, from each checkout):\n")
		for _, t := range d.targets {
			fmt.Fprintf(&b, "- %s: %s\n", t.repo, t.dir)
		}
		switch {
		case d.joint:
			b.WriteString("Together: one agent works in all of them, in one chat.\n")
		case len(d.targets) > 1:
			b.WriteString("Together: no, each repository gets its own agent.\n")
		}
	}
	b.WriteString("The item's status becomes doing.\n")
	text := strings.TrimSpace(d.it.Body)
	if text == "" {
		b.WriteString("\n(The item has no text.)")
		return b.String()
	}
	if utf8.RuneCountInString(text) > maxSummaryText {
		text = string([]rune(text)[:maxSummaryText]) + " […]"
	}
	b.WriteString("\nText:\n" + text)
	return b.String()
}

// folderProjects is the feature.Instance.FolderProjects hook: the projects
// whose folder is trusted that the workspace folder dir is in (projectsIn),
// and the agent tasks linked to their items, so the Workflows feature lets
// the coordinator there reach those tasks' runs. Nothing for an agent
// task's worktree: an agent that was handed work does not coordinate.
func (a *API) folderProjects(ctx context.Context, dir string) feature.FolderProjects {
	var out feature.FolderProjects
	if dir == "" || !filepath.IsAbs(dir) {
		return out
	}
	p, worktree := a.placeOf(ctx, dir)
	if worktree {
		return out
	}
	mine := a.projectsIn(p)
	if len(mine) == 0 {
		return out
	}
	a.mu.Lock()
	ls := a.links()
	a.mu.Unlock()
	for _, f := range mine {
		name := filepath.Base(f)
		if c, err := LoadConfig(f); err == nil {
			name = c.Name
		}
		out.Names = append(out.Names, name)
		out.Tasks = append(out.Tasks, taskIDs(ls[f])...)
	}
	return out
}
