package projects

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/feature"
	"flint/internal/prompts"
)

// Delegate starts an item's agent tasks: one per repository it names (in the
// window's active folder, root, when it names none), run by its agent
// (else the default agent), through the same path as New Agent Task. An
// item with together gets one joint group instead (Host.CreateJointAgentTasks:
// one agent, one chat, across all of its repositories), after the same
// checks; an agent that cannot work in several folders is refused by Flint
// with its reason, and nothing is created. Before
// anything is created it checks the agent can start and every repository
// has a trusted checkout inside the workspace; when one task cannot be
// created, none is kept. An item of a project whose folder is not trusted is
// sent only after the user confirmed it in a native dialog (Host.Confirm),
// which the UI can't skip. Then it remembers the tasks for the item and sets
// its status to doing. Flint starts the chats itself (or queues them).
//
// root is used only for an item without repositories and must be one of
// the open workspace folders.
//
// An item that waits for items that are not done (its after key) is not
// started: the result says NotReady and what it waits for, and the UI offers
// StartWhenReady. An item whose after key has a problem is refused.
func (a *API) Delegate(folder, id, root string) (DelegateResult, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return DelegateResult{}, err
	}
	if !prompts.ValidName(id) {
		return DelegateResult{}, errors.New("invalid item id")
	}
	return a.delegate(real, id, root, nil, "")
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
	// hash is the item's file when the user asked (Waiting.Hash): a file
	// that is not that any more is not started (errItemChanged).
	hash string
}

// errItemChanged refuses a waiting start whose item's file changed since the
// user asked (its text, agent or repositories may say something else now, or
// another item took its id).
var errItemChanged = errors.New("the item changed since you asked: Delegate it again or cancel the start")

// delegate is Delegate for a known project folder and a valid id, by a click
// (auto nil, by ""), by the starter for a waiting start (auto), or by an
// agent's delegate_project_item call the user approved (by: "an agent in
// <folder> (chat <id>)"; delegatetool.go). Only one delegate of an item runs
// at a time; another one meanwhile fails with errStarting.
func (a *API) delegate(real, id, root string, auto *autoStart, by string) (DelegateResult, error) {
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
	d, notReady, err := a.prepare(real, id, root, auto, by)
	if err != nil {
		return DelegateResult{}, err
	}
	if notReady != nil {
		return *notReady, nil
	}
	c, it, deps, agents, agent, targets, reqs, joint, pc := d.c, d.it, d.deps, d.agents, d.agent, d.targets, d.reqs, d.joint, d.pc
	if !d.trusted {
		if auto == nil && !a.host.Confirm("Delegate from an untrusted project?", untrustedMessage(c.Name, real, agent, reqs, joint, pc), "Delegate") {
			return DelegateResult{}, errors.New("Delegate was cancelled: the project's folder is in Restricted Mode")
		}
		// No click comes with this question: it says which item starts and why now.
		// It stays open until answered: the notification and the badge say
		// it waits.
		if auto != nil && !a.host.Confirm("Start a waiting item of an untrusted project?", autoStartMessage(it, auto.waitedFor)+untrustedMessage(c.Name, real, agent, reqs, joint, pc), "Start") {
			return DelegateResult{}, errors.New("the start was declined in Flint's confirmation (the project's folder is in Restricted Mode): Delegate it by hand, or cancel the start")
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
	if !a.hasRoom(real, id, len(reqs)) {
		return DelegateResult{}, fmt.Errorf("the item has %d agent tasks: remove some in the Agent Tasks panel first", MaxTasksPerItem)
	}
	// Not under mu: creating worktrees can take a while (checkout hooks), and
	// the panel must still load meanwhile. The time for it counts from here,
	// not from before the confirmation.
	ctx, cancel := a.ctx(delegateTimeout)
	defer cancel()
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
	if by != "" {
		// Agent tasks keep no log of their own: Flint's log and the tasks'
		// origin (origin.By) say who handed the item out; a run's log says
		// it too (feature.NewAgentTask.DelegatedBy).
		slog.Info("projects: item delegated by "+by+", approved by the user", "project", real, "item", id, "tasks", len(made))
	}
	added := make([]Link, len(made))
	tasks := map[string]feature.AgentTask{}
	for i, t := range made {
		added[i] = Link{Task: t.ID, Repo: targets[i].repo, State: t.State, PullRequest: t.PullRequest, Merged: t.Merged,
			Workflow: t.Run.Workflow, RunID: t.Run.ID, Run: t.Run.State}
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
	ls.pruneProjects(func(f string) bool { return slices.Contains(a.knownSet(), f) })
	var warnings []string
	if pc.Problem != "" {
		warnings = append(warnings, fmt.Sprintf("the agents got no project context from %s: %s", ContextFile, pc.Problem))
	}
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

// target is where one of an item's agent tasks is made: the repository's
// name ("" for the active folder) and its checkout.
type target struct{ repo, dir string }

// delegation is an item that passed every check of Delegate that changes
// nothing and asks nothing (prepare), with the requests it would make.
type delegation struct {
	c       Config
	it      Item
	deps    Deps
	agents  feature.Agents
	agent   string
	targets []target
	reqs    []feature.NewAgentTask
	joint   bool
	pc      ProjectContext
	trusted bool
}

// errAgentUntrusted refuses an agent's delegation from a project whose
// folder is not trusted: only the user's Delegate, after its confirmation,
// sends such an item.
var errAgentUntrusted = errors.New("the project's folder is in Restricted Mode in Flint: an agent cannot delegate its items; the user can Delegate them in the Projects panel, or trust the folder")

// prepare makes Delegate's checks that change nothing and ask nothing: the
// project can be written, the item's file is what a waiting start asked for
// (auto), it is ready (else notReady says what it waits for), its agent is
// available, every repository resolves to a trusted checkout in the
// workspace (root for an item without repositories), and the descriptions
// fit. For an agent's call (by) a project whose folder is not trusted is
// refused outright, and the requests say who delegated them.
func (a *API) prepare(real, id, root string, auto *autoStart, by string) (delegation, *DelegateResult, error) {
	c, err := LoadConfig(real)
	if err != nil {
		return delegation{}, nil, err
	}
	if err := writable(c); err != nil {
		return delegation{}, nil, err
	}
	trusted := a.host.Trusted(real)
	if by != "" && !trusted {
		return delegation{}, nil, errAgentUntrusted
	}
	data, _, err := ReadItemFile(real, id)
	if auto != nil && (err != nil || itemHash(data) != auto.hash) {
		// Only what the user asked for starts by itself; this data is what
		// is sent below.
		return delegation{}, nil, errItemChanged
	}
	if err != nil {
		return delegation{}, nil, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	it, err := ParseItem(id, data, c)
	if err != nil {
		return delegation{}, nil, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	var deps Deps
	if len(it.After) > 0 {
		l, err := Load(real)
		if err != nil {
			return delegation{}, nil, err
		}
		deps = Dependencies(append(slices.DeleteFunc(l.Items, func(x Item) bool { return x.ID == id }), it))[id]
		if deps.Problem != "" {
			return delegation{}, nil, fmt.Errorf("%s can't start: %s: fix the item's after line", itemPath(id), deps.Problem)
		}
		if len(deps.WaitingFor) > 0 {
			v := itemView(real, it, a.linksOf(real, id), nil)
			a.mu.Lock()
			setDeps(&v, deps, a.waiting()[real])
			a.mu.Unlock()
			return delegation{}, &DelegateResult{Item: v, Tasks: []TaskView{}, NotReady: true, WaitingFor: deps.WaitingFor}, nil
		}
	}
	agents := a.host.Agents()
	if p := agentProblem(it.Agent, agents); p != "" {
		return delegation{}, nil, errors.New(p)
	}
	agent := it.Agent
	if agent == "" {
		agent = agents.Default
	}
	targets, err := a.targets(c, it, root)
	if err != nil {
		return delegation{}, nil, err
	}
	// The context file as the user may be shown it now: an untrusted
	// project's is sent only while it is still this text (origin.Confirmed).
	pc := ReadContext(real)
	reqs := make([]feature.NewAgentTask, len(targets))
	for i, t := range targets {
		o := origin{Project: real, Item: id, Repo: t.repo, By: by}
		if !trusted {
			// What the confirmation shows is all its chat may get
			// (restrictedContext).
			shown, _ := shownContext(pc.Text)
			o.Confirmed, o.Title, o.Repos = contextHash(shown), it.Title, it.Repos
		}
		reqs[i] = feature.NewAgentTask{Dir: t.dir, Description: it.TaskDescription(t.repo), Agent: agent, Origin: o.String(), Workflow: it.Workflow, DelegatedBy: by}
		if len(reqs[i].Description) > MaxDescription {
			if d := reqs[i].Description; d == it.Description() {
				return delegation{}, nil, fmt.Errorf("the item's title and text are longer than %d bytes: shorten the text", MaxDescription)
			}
			return delegation{}, nil, fmt.Errorf("the item's title and text with the note about its repositories are longer than %d bytes: shorten the text", MaxDescription)
		}
	}
	joint := it.Together && len(reqs) > 1 // ParseItem: together only with repos, never with workflow
	return delegation{c: c, it: it, deps: deps, agents: agents, agent: agent, targets: targets, reqs: reqs, joint: joint, pc: pc, trusted: trusted}, nil, nil
}

// targets resolves where an item's agent tasks are made: each repository
// it names to a checkout that is in the workspace and trusted (saying in
// one message everything that is missing), or root, an open, trusted
// workspace folder, for an item that names none.
func (a *API) targets(c Config, it Item, root string) ([]target, error) {
	if len(it.Repos) == 0 {
		dir, err := a.activeFolder(root)
		if err != nil {
			return nil, err
		}
		return []target{{"", dir}}, nil
	}
	// The checks get their own context: the one that bounds creating the
	// tasks starts after the confirmation, which may stay open for a long
	// while (a waiting start that asks while the user is away).
	cctx, ccancel := a.ctx(delegateTimeout)
	defer ccancel()
	roots := a.roots(cctx)
	checkouts := a.checkouts()
	var targets []target
	var missing, outside, untrusted, broken []string
	for _, name := range it.Repos {
		decl, _ := c.Repository(name) // ParseItem checked the names
		v := a.resolveRepo(cctx, decl, roots, checkouts)
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
		return nil, errors.New("Delegate needs every repository of the item: " + strings.Join(why, "; "))
	}
	return targets, nil
}

// maxConfirmText bounds the description shown in the confirmation of a
// Delegate from an untrusted project (characters).
const maxConfirmText = 1000

// untrustedMessage is the question Delegate asks before it sends the text of
// an item from a project whose folder is not trusted: the item may come from
// someone else's repository, and its text becomes an agent's instructions.
// It names the agent, the folders the tasks work in and the description
// (the first task's, cut at maxConfirmText characters and saying so), and
// says that the project's context file is sent too, with its beginning
// (confirmContext).
func untrustedMessage(name, folder, agent string, reqs []feature.NewAgentTask, joint bool, pc ProjectContext) string {
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
	if w := reqs[0].Workflow; w != "" {
		note += fmt.Sprintf("\nThe item runs the workflow %q: its steps' agents take each task step by step. "+
			"The workflow is read from Flint's built-in workflows, your own, or the trusted repository's .flint/workflows, never from the project's folder; "+
			"its first step's agent starts the task.", w)
	}
	read := "Its item becomes the instructions of an agent. Read it before you go on."
	if pc.Text != "" {
		read = "Its item and its context file become the instructions of an agent. Read them before you go on."
	}
	return fmt.Sprintf("The project %q is in a folder you have not trusted (Restricted Mode):\n%s\n\n"+
		"%s\n\n"+
		"Agent: %s\nWorks in: %s%s\n\nDescription:\n%s%s",
		name, folder, read, agent, strings.Join(dirs, ", "), note, desc, confirmContext(pc))
}

// autoStartMessage opens the question of a start nobody clicked just now: it
// names the item and why it starts now.
// An item with together says that one agent works across its repositories.
func autoStartMessage(it Item, waitedFor []string) string {
	together := ""
	if it.Together {
		together = fmt.Sprintf(" It runs as one agent across the repositories %s.", strings.Join(it.Repos, ", "))
	}
	if it.Workflow != "" {
		together = fmt.Sprintf(" It runs the workflow %q.", it.Workflow)
	}
	return fmt.Sprintf("Flint is starting the item %q (%s) by itself: you asked to start it when it is ready, "+
		"and the items it waited for (%s) are done now.%s\n\n", it.Title, it.ID, strings.Join(waitedFor, ", "), together)
}

// linksOf are the links of item id of folder.
func (a *API) linksOf(folder, id string) []Link {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.links()[folder][id]
}

// activeFolder checks the window's active folder from the UI: it must be
// one of the open workspace folders, and trusted.
func (a *API) activeFolder(active string) (string, error) {
	if active == "" {
		return "", errors.New("the item names no repository and no folder is open: open the folder it is for")
	}
	for _, root := range a.host.Roots() {
		if root == active {
			if !a.host.Trusted(root) {
				return "", fmt.Errorf("%s is in Restricted Mode: trust the folder first", root)
			}
			return root, nil
		}
	}
	return "", fmt.Errorf("%s is not a folder open in the window", active)
}
