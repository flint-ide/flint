package backlog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"flint/internal/feature"
	"flint/internal/prompts"
)

// Delegate starts an item's agent tasks: one per repository it names (in the
// window's active project, project, when it names none), run by its agent
// (else the default agent), through the same path as New Agent Task. Before
// anything is created it checks the agent can start and every repository
// has a trusted checkout inside the workspace; when one task cannot be
// created, none is kept. Then it remembers the tasks for the item and sets
// its status to doing. Flint starts the chats itself (or queues them).
//
// project is used only for an item without repositories and must be one of
// the open workspace folders.
func (a *API) Delegate(folder, id, project string) (DelegateResult, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return DelegateResult{}, err
	}
	if !prompts.ValidName(id) {
		return DelegateResult{}, errors.New("invalid item id")
	}
	c, err := LoadConfig(real)
	if err != nil {
		return DelegateResult{}, err
	}
	data, _, err := ReadItemFile(real, id)
	if err != nil {
		return DelegateResult{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	it, err := ParseItem(id, data, c)
	if err != nil {
		return DelegateResult{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	agents := a.host.Agents()
	if p := agentProblem(it.Agent, agents); p != "" {
		return DelegateResult{}, errors.New(p)
	}
	agent := it.Agent
	if agent == "" {
		agent = agents.Default
	}
	ctx, cancel := a.ctx(delegateTimeout)
	defer cancel()
	type target struct{ repo, dir string }
	var targets []target
	if len(it.Repos) == 0 {
		dir, err := a.projectFolder(project)
		if err != nil {
			return DelegateResult{}, err
		}
		targets = append(targets, target{"", dir})
	} else {
		roots := a.roots(ctx)
		checkouts := a.checkouts()
		var missing, outside, untrusted, broken []string
		for _, name := range it.Repos {
			decl, _ := c.Repository(name) // ParseItem checked the names
			v := a.resolveRepo(ctx, decl, roots, checkouts)
			switch {
			case v.Problem != "":
				broken = append(broken, name+": "+v.Problem)
			case v.Folder == "":
				missing = append(missing, name)
			case !v.InWorkspace:
				outside = append(outside, name+" ("+v.Folder+")")
			case !v.Trusted:
				untrusted = append(untrusted, name+" ("+v.Folder+")")
			default:
				targets = append(targets, target{name, v.Folder})
			}
		}
		var why []string
		if len(missing) > 0 {
			why = append(why, "no checkout of "+strings.Join(missing, ", ")+" was found: open it in the workspace or Locate… it")
		}
		why = append(why, broken...)
		if len(outside) > 0 {
			why = append(why, "add "+strings.Join(outside, ", ")+" to the workspace first")
		}
		if len(untrusted) > 0 {
			why = append(why, strings.Join(untrusted, ", ")+" is in Restricted Mode: trust the folder first")
		}
		if len(why) > 0 {
			return DelegateResult{}, errors.New("Delegate needs every repository of the item: " + strings.Join(why, "; "))
		}
	}
	reqs := make([]feature.NewAgentTask, len(targets))
	for i, t := range targets {
		reqs[i] = feature.NewAgentTask{Dir: t.dir, Description: it.TaskDescription(t.repo), Agent: agent}
		if len(reqs[i].Description) > MaxDescription {
			return DelegateResult{}, fmt.Errorf("the item's title and text with the note about its repositories are longer than %d characters: shorten the text", MaxDescription)
		}
	}
	a.mu.Lock()
	_, room := addLinks(a.links()[real][id], make([]Link, len(reqs)))
	a.mu.Unlock()
	if !room {
		return DelegateResult{}, fmt.Errorf("the item has %d agent tasks: remove some in the Agent Tasks panel first", MaxTasksPerItem)
	}
	// Not under mu: creating worktrees can take a while (checkout hooks), and
	// the panel must still load meanwhile.
	made, err := a.host.CreateAgentTasks(ctx, reqs)
	if err != nil {
		return DelegateResult{}, err
	}
	if len(made) != len(targets) {
		return DelegateResult{}, fmt.Errorf("internal error: %d agent tasks for %d repositories", len(made), len(targets))
	}
	added := make([]Link, len(made))
	tasks := map[string]feature.AgentTask{}
	for i, t := range made {
		added[i] = Link{Task: t.ID, Repo: targets[i].repo, State: t.State, PullRequest: t.PullRequest, Merged: t.Merged}
		tasks[t.ID] = t
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res := DelegateResult{Tasks: []TaskView{}}
	ls := a.links()
	if ls[real] == nil {
		ls[real] = map[string][]Link{}
	}
	next, ok := addLinks(ls[real][id], added)
	if !ok { // another Delegate filled it meanwhile: keep the tasks anyway
		next = append(ls[real][id], added...)
	}
	ls[real][id] = next
	ls.pruneBacklogs(func(f string) bool { return slices.Contains(a.knownSet(), f) })
	var warnings []string
	if err := a.host.SaveState(stateTasks, ls); err != nil {
		warnings = append(warnings, "the item's agent tasks could not be remembered: "+err.Error())
	}
	if moved, err := a.writeStatus(real, id, c, StatusDoing); err != nil {
		warnings = append(warnings, "the status could not be set to doing: "+err.Error())
	} else {
		it = moved
	}
	res.Warning = strings.Join(warnings, "; ")
	all := a.host.AgentTasks(ctx, taskIDs(map[string][]Link{id: ls[real][id]}))
	for id, t := range tasks {
		if _, ok := all[id]; !ok {
			all[id] = t
		}
	}
	res.Item = itemView(real, it, ls[real][id], all)
	res.Item.AgentProblem = agentProblem(it.Agent, agents)
	for _, tv := range res.Item.Tasks {
		if _, ok := tasks[tv.ID]; ok {
			res.Tasks = append(res.Tasks, tv)
		}
	}
	a.emit(real)
	return res, nil
}

// projectFolder checks the window's active project from the UI: it must be
// one of the open workspace folders, and trusted.
func (a *API) projectFolder(project string) (string, error) {
	if project == "" {
		return "", errors.New("the item names no repository and no project is open: open the project it is for")
	}
	for _, root := range a.host.Roots() {
		if root == project {
			if !a.host.Trusted(root) {
				return "", fmt.Errorf("%s is in Restricted Mode: trust the folder first", root)
			}
			return root, nil
		}
	}
	return "", fmt.Errorf("%s is not an open project folder", project)
}
