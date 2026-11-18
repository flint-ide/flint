package backlog

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/feature"
	"flint/internal/prompts"
)

// Delegate starts an item's agent tasks: one per repository it names (in the
// window's active project, project, when it names none), run by its agent
// (else the default agent), through the same path as New Agent Task. An
// item with together gets one joint group instead (Host.CreateJointAgentTasks:
// one agent, one chat, across all of its repositories), after the same
// checks; an agent that cannot work in several folders is refused by Flint
// with its reason, and nothing is created. Before
// anything is created it checks the agent can start and every repository
// has a trusted checkout inside the workspace; when one task cannot be
// created, none is kept. An item of a backlog whose folder is not trusted is
// sent only after the user confirmed it in a native dialog (Host.Confirm),
// which the UI can't skip. Then it remembers the tasks for the item and sets
// its status to doing. Flint starts the chats itself (or queues them).
//
// project is used only for an item without repositories and must be one of
// the open workspace folders.
//
// An item that waits for items that are not done (its after key) is not
// started: the result says NotReady and what it waits for, and the UI offers
// StartWhenReady. An item whose after key has a problem is refused.
func (a *API) Delegate(folder, id, project string) (DelegateResult, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return DelegateResult{}, err
	}
	if !prompts.ValidName(id) {
		return DelegateResult{}, errors.New("invalid item id")
	}
	return a.delegate(real, id, project, nil)
}

// errStarting: the item is being delegated already.
var errStarting = errors.New("the item is being started already: wait a moment")

// autoStart is a Delegate that Flint makes by itself for a waiting start.
type autoStart struct {
	// waitedFor are the items it waited for (they are done now).
	waitedFor []string
	// still reports, under mu, whether the start is still wanted (the
	// user may cancel it while the confirmation is open).
	still func() bool
}

// delegate is Delegate for a known backlog folder and a valid id, by a click
// (auto nil) or by the starter for a waiting start (auto). Only one delegate
// of an item runs at a time; another one meanwhile fails with errStarting.
func (a *API) delegate(real, id, project string, auto *autoStart) (DelegateResult, error) {
	key := real + "\x00" + id
	a.mu.Lock()
	if a.starting[key] || auto != nil && !auto.still() {
		a.mu.Unlock()
		return DelegateResult{}, errStarting
	}
	a.starting[key] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.starting, key)
		a.mu.Unlock()
	}()
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
	var deps Deps
	if len(it.After) > 0 {
		l, err := Load(real)
		if err != nil {
			return DelegateResult{}, err
		}
		deps = Dependencies(append(slices.DeleteFunc(l.Items, func(x Item) bool { return x.ID == id }), it))[id]
		if deps.Problem != "" {
			return DelegateResult{}, fmt.Errorf("%s can't start: %s: fix the item's after line", itemPath(id), deps.Problem)
		}
		if len(deps.WaitingFor) > 0 {
			v := itemView(real, it, a.linksOf(real, id), nil)
			a.mu.Lock()
			setDeps(&v, deps, a.waiting()[real])
			a.mu.Unlock()
			return DelegateResult{Item: v, Tasks: []TaskView{}, NotReady: true, WaitingFor: deps.WaitingFor}, nil
		}
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
			if d := reqs[i].Description; d == it.Description() {
				return DelegateResult{}, fmt.Errorf("the item's title and text are longer than %d bytes: shorten the text", MaxDescription)
			}
			return DelegateResult{}, fmt.Errorf("the item's title and text with the note about its repositories are longer than %d bytes: shorten the text", MaxDescription)
		}
	}
	joint := it.Together && len(reqs) > 1 // ParseItem: together only with repos
	if !a.host.Trusted(real) {
		if auto == nil && !a.host.Confirm("Delegate from an untrusted backlog?", untrustedMessage(c.Name, real, agent, reqs, joint), "Delegate") {
			return DelegateResult{}, errors.New("Delegate was cancelled: the backlog's folder is in Restricted Mode")
		}
		// No click comes with this question: it says which item starts and why now.
		if auto != nil && !a.host.Confirm("Start a waiting item of an untrusted backlog?", autoStartMessage(it, auto.waitedFor)+untrustedMessage(c.Name, real, agent, reqs, joint), "Start") {
			return DelegateResult{}, errors.New("the start was declined in Flint's confirmation (the backlog's folder is in Restricted Mode): Delegate it by hand, or cancel the start")
		}
	}
	if auto != nil {
		a.mu.Lock()
		still := auto.still()
		a.mu.Unlock()
		if !still {
			return DelegateResult{}, errStarting // cancelled while the question was open
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
	var made []feature.AgentTask
	if joint {
		made, err = a.host.CreateJointAgentTasks(ctx, reqs, agent)
	} else {
		made, err = a.host.CreateAgentTasks(ctx, reqs)
	}
	if dh := (*feature.DetachedHeadError)(nil); errors.As(err, &dh) {
		return DelegateResult{}, fmt.Errorf("%s is on a detached HEAD: check out a branch there, then Delegate again", dh.Dir)
	}
	if errors.Is(err, feature.ErrJointEngine) {
		return DelegateResult{}, fmt.Errorf("%w (the item says together: true; without that line each repository gets its own agent)", err)
	}
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
	// Started: a waiting start of the item is done with.
	if w := a.waiting(); w[real] != nil {
		if _, ok := w[real][id]; ok {
			delete(w[real], id)
			if len(w[real]) == 0 {
				delete(w, real)
			}
			_ = a.host.SaveState(stateWaiting, w)
		}
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
	setDeps(&res.Item, deps, nil)
	res.WaitingFor = []string{}
	for _, tv := range res.Item.Tasks {
		if _, ok := tasks[tv.ID]; ok {
			res.Tasks = append(res.Tasks, tv)
		}
	}
	a.emit(real)
	return res, nil
}

// maxConfirmText bounds the description shown in the confirmation of a
// Delegate from an untrusted backlog (characters).
const maxConfirmText = 1000

// untrustedMessage is the question Delegate asks before it sends the text of
// an item from a backlog whose folder is not trusted: the item may come from
// someone else's repository, and its text becomes an agent's instructions.
// It names the agent, the folders the tasks work in and the description
// (the first task's, cut at maxConfirmText characters and saying so).
func untrustedMessage(name, folder, agent string, reqs []feature.NewAgentTask, joint bool) string {
	dirs := make([]string, len(reqs))
	for i, r := range reqs {
		dirs[i] = r.Dir
	}
	desc := reqs[0].Description
	if n := utf8.RuneCountInString(desc); n > maxConfirmText {
		desc = string([]rune(desc)[:maxConfirmText]) + fmt.Sprintf("\n[… cut here: the description has %d characters]", n)
	}
	note := ""
	switch {
	case joint:
		note = "\nOne agent works in all of these folders together, in one chat (the item says together: true); " +
			"Flint adds to its first message which folder is which repository."
	case len(reqs) > 1:
		note = "\nEach task also gets a note naming the item's repositories."
	}
	return fmt.Sprintf("The backlog %q is in a folder you have not trusted (Restricted Mode):\n%s\n\n"+
		"Its item becomes the instructions of an agent. Read it before you go on.\n\n"+
		"Agent: %s\nWorks in: %s%s\n\nDescription:\n%s",
		name, folder, agent, strings.Join(dirs, ", "), note, desc)
}

// autoStartMessage opens the question of a start nobody clicked just now: it
// names the item and why it starts now.
// An item with together says that one agent works across its repositories.
func autoStartMessage(it Item, waitedFor []string) string {
	together := ""
	if it.Together {
		together = fmt.Sprintf(" It runs as one agent across the repositories %s.", strings.Join(it.Repos, ", "))
	}
	return fmt.Sprintf("Flint is starting the backlog item %q (%s) by itself: you asked to start it when it is ready, "+
		"and the items it waited for (%s) are done now.%s\n\n", it.Title, it.ID, strings.Join(waitedFor, ", "), together)
}

// linksOf are the links of item id of folder.
func (a *API) linksOf(folder, id string) []Link {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.links()[folder][id]
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
