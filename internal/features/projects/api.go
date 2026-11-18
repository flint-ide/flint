package projects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"flint/internal/feature"
	"flint/internal/prompts"
)

// EventChanged is emitted after Flint wrote to a project (a status, a new
// item, an automatic status change) or the list of projects changed; its
// data is the project folder ("" for the list).
const EventChanged = "projects:changed"

// gitTimeout bounds the git calls of one request (remotes, task states).
const gitTimeout = 30 * time.Second

// delegateTimeout bounds creating an item's agent tasks (worktrees, hooks),
// counted from after Delegate's confirmation (a variable for tests).
var delegateTimeout = 5 * time.Minute

// API is the Projects panel's service. Every method takes a project folder
// that Flint knows (opened or created here, or a workspace folder with
// flint-project.json) and validated item ids; paths are taken from the UI
// only to open, create or locate, and must be existing folders.
type API struct {
	host feature.Host
	// mu serializes reading and writing the state keys and the item files.
	mu sync.Mutex
	// kick asks the watcher to look at the items' tasks (TasksChanged).
	kick chan struct{}
	// startKick asks the starter to look at the waiting starts.
	startKick chan struct{}
	// starting are the items being delegated now (folder NUL id), under
	// mu: one start per item at a time, whoever asks (a click, the starter).
	starting map[string]bool
	// added counts the items each chat added with add_project_item, under mu.
	added map[string]int
	// beforeCompare runs in writeStatus once the new file is written, just
	// before the old one is compared (tests: an editor saving meanwhile).
	beforeCompare func()

	clones cloner
}

func newAPI(h feature.Host) *API {
	return &API{host: h, kick: make(chan struct{}, 1), startKick: make(chan struct{}, 1), starting: map[string]bool{}, added: map[string]int{}}
}

// ProjectInfo is a project in the picker.
type ProjectInfo struct {
	// Path is the project folder; the other methods take it back.
	Path string `json:"path"`
	Name string `json:"name"`
	// Workspace: listed because it is a workspace folder (not remembered,
	// so Close does not apply).
	Workspace bool `json:"workspace"`
	// Trusted: the project's folder is trusted. An untrusted project is left
	// out of get_project, and Delegate asks first (in Go).
	Trusted bool `json:"trusted"`
	// Error says why the project can't be read now ("" when it can).
	Error string `json:"error"`
	// OldFormat: the project is marked by OldConfigFile (oldformat.go) and
	// read-only until RenameOldConfig.
	OldFormat bool `json:"oldFormat"`
}

// Project is a project with its items, as the panel and the board show it.
type Project struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Workspace bool   `json:"workspace"`
	// Trusted: as ProjectInfo.Trusted.
	Trusted bool `json:"trusted"`
	// OldFormat: as ProjectInfo.OldFormat.
	OldFormat bool `json:"oldFormat"`
	// Repositories are flint-project.json's, with the checkout of each.
	Repositories []RepositoryView `json:"repositories"`
	// Context is the project's context file, PROJECT.md (Open, or Add
	// Project Context… when it does not exist); a problem with it is also
	// listed in Problems.
	Context ContextView `json:"context"`
	// Items in the panel's order: doing, review, todo, done, then priority
	// (high first), then title.
	Items    []ItemView `json:"items"`
	Problems []Problem  `json:"problems"`
	// DefaultAgent runs items without an agent ("" when no agent can start).
	DefaultAgent string `json:"defaultAgent"`
	// Clone is the clone running for this project (null when none).
	Clone *CloneView `json:"clone"`
}

// RepositoryView is a repository of the project and its checkout here.
type RepositoryView struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Folder is the checkout Delegate uses: a workspace folder whose
	// repository has a remote with this URL, else the folder chosen with
	// Locate…; "" when there is none (offer Locate…).
	Folder string `json:"folder"`
	// Located: Folder was chosen with Locate….
	Located bool `json:"located"`
	// InWorkspace: Folder is inside an open workspace folder. A located
	// folder outside it must be attached to the workspace before Delegate.
	InWorkspace bool `json:"inWorkspace"`
	// Trusted: Folder is trusted (Delegate is refused in Restricted Mode).
	Trusted bool `json:"trusted"`
	// Problem says why the located folder can't be used ("" when it can).
	Problem string `json:"problem"`
}

// ItemView is an item with its agent tasks.
type ItemView struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Priority string   `json:"priority"`
	Agent    string   `json:"agent"`
	Repos    []string `json:"repos"`
	// Together: Delegate starts one agent that works in all of Repos (one
	// joint group of agent tasks, one chat) instead of one per repository.
	Together bool   `json:"together"`
	Body     string `json:"body"`
	// Path is the item's file, to open in the editor.
	Path string `json:"path"`
	// AgentProblem says why Delegate can't start the item's agent now.
	AgentProblem string `json:"agentProblem"`
	// Failed: one of its agent tasks failed (the item stays in doing).
	Failed bool       `json:"failed"`
	Tasks  []TaskView `json:"tasks"`
	// After are the items that must be done first (ids of this project), as
	// the file says; WaitingFor those of them that are not done.
	After      []string `json:"after"`
	WaitingFor []string `json:"waitingFor"`
	// Ready: every item of After is done and DependencyProblem is "".
	// Delegate starts only a ready item.
	Ready bool `json:"ready"`
	// DependencyProblem says what is wrong with After (an unknown id, the
	// item itself, a cycle); such an item is never ready.
	DependencyProblem string `json:"dependencyProblem"`
	// Waiting: the user asked to start the item when it is ready (null when
	// not). Flint then delegates it by itself.
	Waiting *WaitingView `json:"waiting"`
}

// WaitingView is a waiting start of an item.
type WaitingView struct {
	// Since is when the user asked (RFC 3339).
	Since string `json:"since"`
	// Reason says why the last automatic start failed ("" when none was
	// tried); the item keeps waiting, and the start is tried again once
	// something it depends on changed.
	Reason string `json:"reason"`
}

// TaskView is one agent task of an item.
type TaskView struct {
	ID string `json:"id"`
	// Repo is the repository's name ("" for the window's active folder).
	Repo   string `json:"repo"`
	Title  string `json:"title"`
	Agent  string `json:"agent"`
	Branch string `json:"branch"`
	// Worktree is the task's folder (open it as the Agent Tasks panel does).
	Worktree string `json:"worktree"`
	// State: queued, idle, working, waiting, finished or failed; for a
	// removed task, the last state seen.
	State       string `json:"state"`
	Error       string `json:"error"`
	PullRequest string `json:"pullRequest"`
	Merged      bool   `json:"merged"`
	// Stale: the worktree folder is gone. Gone: the task was removed.
	Stale bool `json:"stale"`
	Gone  bool `json:"gone"`
	// Joint is the id of the task's joint group ("" for a task with a chat
	// of its own, and for a removed task); Lead marks the group's task whose
	// worktree its one chat runs in (open that one for the chat).
	Joint string `json:"joint"`
	Lead  bool   `json:"lead"`
}

// DelegateResult is what Delegate started.
type DelegateResult struct {
	Item ItemView `json:"item"`
	// Tasks are the new agent tasks. Flint starts their chats itself (or
	// queues them); the UI adopts each chat from "chat:opened".
	Tasks []TaskView `json:"tasks"`
	// Warning: the tasks started, but something after that failed (the
	// status could not be written).
	Warning string `json:"warning"`
	// NotReady: nothing was started because the item waits for items that
	// are not done (WaitingFor). The UI offers StartWhenReady.
	NotReady   bool     `json:"notReady"`
	WaitingFor []string `json:"waitingFor"`
}

func (a *API) ctx(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx := a.host.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, timeout)
}

func (a *API) emit(folder string) { a.host.Emit(EventChanged, folder) }

// ---- known projects

func (a *API) remembered() []string {
	var list []string
	if err := a.host.LoadState(stateProjects, &list); err != nil {
		return nil
	}
	return list
}

// workspaceProjects are the workspace folders with flint-project.json (or,
// in the old format, OldConfigFile) at their top (resolved).
func (a *API) workspaceProjects() []string {
	var out []string
	for _, root := range a.host.Roots() {
		real, err := Resolve(root)
		if err != nil || slices.Contains(out, real) {
			continue
		}
		if hasConfigFile(real) {
			out = append(out, real)
		}
	}
	return out
}

// known resolves a folder from the UI and checks that it is a project Flint
// knows; workspace tells whether it is listed as a workspace folder.
func (a *API) known(folder string) (real string, workspace bool, err error) {
	if folder == "" || !filepath.IsAbs(folder) {
		return "", false, errors.New("choose a project first")
	}
	real, err = Resolve(folder)
	if err == nil {
		if slices.Contains(a.workspaceProjects(), real) {
			return real, true, nil
		}
		if slices.Contains(a.remembered(), real) {
			return real, false, nil
		}
	} else if slices.Contains(a.remembered(), filepath.Clean(folder)) {
		return "", false, fmt.Errorf("the project folder %s is gone: close it", folder)
	}
	return "", false, fmt.Errorf("%s is not an open project: open it with Open Project… first", folder)
}

// knownSet are all known projects (resolved).
func (a *API) knownSet() []string {
	out := a.workspaceProjects()
	for _, f := range a.remembered() {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// Projects lists the projects Flint knows: the remembered ones, most recent
// first, then the workspace folders that hold flint-project.json.
func (a *API) Projects() []ProjectInfo {
	out := []ProjectInfo{}
	ws := a.workspaceProjects()
	remembered := a.remembered()
	add := func(folder string, workspace bool) {
		info := ProjectInfo{Path: folder, Name: filepath.Base(folder), Workspace: workspace, Trusted: a.host.Trusted(folder)}
		if c, err := LoadConfig(folder); err != nil {
			info.Error = err.Error()
		} else {
			info.Name, info.OldFormat = c.Name, c.OldFormat
		}
		out = append(out, info)
	}
	for _, f := range remembered {
		add(f, false)
	}
	for _, f := range ws {
		if !slices.Contains(remembered, f) {
			add(f, true)
		}
	}
	return out
}

// requireDir checks a folder from the UI: absolute, existing, a directory.
// It returns it resolved.
func requireDir(folder string) (string, error) {
	if folder == "" || !filepath.IsAbs(folder) {
		return "", errors.New("choose a folder")
	}
	info, err := os.Stat(folder)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", folder, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", folder)
	}
	return Resolve(folder)
}

// Open remembers a folder that holds flint-project.json (Open Project…; the
// UI asks for the folder with WorkspaceAPI.PickFolder).
func (a *API) Open(folder string) (ProjectInfo, error) {
	real, err := requireDir(folder)
	if err != nil {
		return ProjectInfo{}, err
	}
	if _, err := os.Lstat(filepath.Join(real, configFile(real))); errors.Is(err, fs.ErrNotExist) {
		return ProjectInfo{}, fmt.Errorf("%s is not a project: it has no %s (use New Project… to make one)", folder, ConfigFile)
	}
	c, err := LoadConfig(real)
	if err != nil {
		return ProjectInfo{}, err
	}
	if err := a.rememberFolder(real); err != nil {
		return ProjectInfo{}, err
	}
	return ProjectInfo{Path: real, Name: c.Name, Workspace: slices.Contains(a.workspaceProjects(), real), Trusted: a.host.Trusted(real), OldFormat: c.OldFormat}, nil
}

// Create makes folder a project (flint-project.json named name, default the
// folder's name, and an empty items folder) and remembers it. It never
// overwrites a file.
func (a *API) Create(folder, name string) (ProjectInfo, error) {
	real, err := requireDir(folder)
	if err != nil {
		return ProjectInfo{}, err
	}
	data, err := NewConfig(name)
	if err != nil {
		return ProjectInfo{}, err
	}
	if configFile(real) == OldConfigFile {
		return ProjectInfo{}, fmt.Errorf("%s is a project in the old format (%s): open it with Open Project… and rename the file there", folder, OldConfigFile)
	}
	f, err := os.OpenFile(filepath.Join(real, ConfigFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return ProjectInfo{}, fmt.Errorf("%s is already a project: open it with Open Project…", folder)
	}
	if err != nil {
		return ProjectInfo{}, err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return ProjectInfo{}, err
	}
	if err := os.Mkdir(filepath.Join(real, ItemsDir), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return ProjectInfo{}, err
	}
	c, err := LoadConfig(real)
	if err != nil {
		return ProjectInfo{}, err
	}
	if err := a.rememberFolder(real); err != nil {
		return ProjectInfo{}, err
	}
	return ProjectInfo{Path: real, Name: c.Name, Workspace: slices.Contains(a.workspaceProjects(), real), Trusted: a.host.Trusted(real)}, nil
}

func (a *API) rememberFolder(real string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.host.SaveState(stateProjects, remember(a.remembered(), real)); err != nil {
		return err
	}
	a.emit("")
	return nil
}

// Close forgets a remembered project and the links of its items to agent
// tasks (the tasks stay). A workspace folder's project is listed while the
// folder is open and can't be closed.
func (a *API) Close(folder string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := a.remembered()
	key := filepath.Clean(folder)
	if real, err := Resolve(folder); err == nil {
		key = real
	}
	if !slices.Contains(list, key) {
		if slices.Contains(a.workspaceProjects(), key) {
			return errors.New("this project is listed because it is a workspace folder: remove the folder from the workspace instead")
		}
		return fmt.Errorf("%s is not an open project", folder)
	}
	if err := a.host.SaveState(stateProjects, forget(list, key)); err != nil {
		return err
	}
	if !slices.Contains(a.workspaceProjects(), key) {
		ls := a.links()
		if _, ok := ls[key]; ok {
			delete(ls, key)
			if err := a.host.SaveState(stateTasks, ls); err != nil {
				return err
			}
		}
		w := a.waiting()
		if _, ok := w[key]; ok {
			delete(w, key)
			if err := a.host.SaveState(stateWaiting, w); err != nil {
				return err
			}
		}
		a.clones.cancel(key)
	}
	a.emit("")
	return nil
}

// ---- reading

// Load reads a project with its items, problems, repositories and the
// items' agent tasks. Items whose tasks moved on change status first (see
// NextStatus).
func (a *API) Load(folder string) (Project, error) {
	real, workspace, err := a.known(folder)
	if err != nil {
		return Project{}, err
	}
	ctx, cancel := a.ctx(gitTimeout)
	defer cancel()
	a.mu.Lock()
	defer a.mu.Unlock()
	var tasks map[string]feature.AgentTask
	if c, err := LoadConfig(real); err == nil && !c.OldFormat {
		var moved bool
		if moved, tasks = a.reconcile(ctx, real, c); moved {
			a.emit(real)
			a.kickStarter()
		}
	}
	l, err := Load(real)
	if err != nil {
		return Project{}, err
	}
	agents := a.host.Agents()
	b := Project{
		Path: real, Name: l.Config.Name, Workspace: workspace, Trusted: a.host.Trusted(real), OldFormat: l.Config.OldFormat,
		Repositories: a.resolveRepos(ctx, l.Config), Context: contextView(real, l.Context), Items: []ItemView{}, Problems: l.Problems,
		DefaultAgent: agents.Default, Clone: a.clones.view(real),
	}
	itemLinks := a.links()[real]
	if tasks == nil {
		tasks = a.host.AgentTasks(ctx, taskIDs(itemLinks))
	}
	deps := Dependencies(l.Items)
	waiting := a.waiting()[real]
	for _, it := range l.Items {
		v := itemView(real, it, itemLinks[it.ID], tasks)
		v.AgentProblem = agentProblem(it.Agent, agents)
		if it.Agent != "" && v.AgentProblem != "" {
			b.Problems = append(b.Problems, Problem{File: itemPath(it.ID), Reason: v.AgentProblem})
		}
		setDeps(&v, deps[it.ID], waiting)
		if v.DependencyProblem != "" {
			b.Problems = append(b.Problems, Problem{File: itemPath(it.ID), Reason: v.DependencyProblem})
		}
		b.Items = append(b.Items, v)
	}
	if len(waiting) > 0 {
		// A dependency may have become done in the editor: the starter looks
		// (it starts nothing that it tried before unless something changed).
		a.kickStarter()
	}
	return b, nil
}

// setDeps fills in what v waits for and its waiting start.
func setDeps(v *ItemView, d Deps, waiting map[string]Waiting) {
	v.WaitingFor = nonNil(d.WaitingFor)
	v.DependencyProblem = d.Problem
	v.Ready = d.Ready()
	if w, ok := waiting[v.ID]; ok {
		v.Waiting = &WaitingView{Since: w.Since.UTC().Format(time.RFC3339), Reason: w.Reason}
	}
}

// viewDeps fills in v's dependencies from the project as it is on disk now.
// Callers hold mu.
func (a *API) viewDeps(folder string, v *ItemView) {
	l, err := Load(folder)
	if err != nil {
		return
	}
	setDeps(v, Dependencies(l.Items)[v.ID], a.waiting()[folder])
}

// agentProblem says why agent (or the default agent, for "") can't start.
func agentProblem(agent string, agents feature.Agents) string {
	switch {
	case agent == "" && agents.Default == "":
		return "no agent can start: install Claude Code or Codex, or add an agent in Settings ▸ Agents"
	case agent != "" && !slices.Contains(agents.Available, agent):
		return fmt.Sprintf("the agent %q is not available here: define or install it, or change the item's agent", agent)
	}
	return ""
}

func itemView(folder string, it Item, ls []Link, tasks map[string]feature.AgentTask) ItemView {
	v := ItemView{
		ID: it.ID, Title: it.Title, Status: it.Status, Priority: it.Priority, Agent: it.Agent,
		Repos: nonNil(it.Repos), Together: it.Together, Body: it.Body, Path: ItemFile(folder, it.ID), Tasks: []TaskView{},
		Failed: Failed(ls), After: nonNil(it.After), WaitingFor: []string{}, Ready: true,
	}
	for _, l := range ls {
		tv := TaskView{ID: l.Task, Repo: l.Repo, State: l.State, PullRequest: l.PullRequest, Merged: l.Merged, Gone: l.Gone}
		if t, ok := tasks[l.Task]; ok && !l.Gone {
			tv.Title, tv.Agent, tv.Branch, tv.Worktree = t.Title, t.Agent, t.Branch, t.Worktree
			tv.State, tv.Error, tv.Merged, tv.Stale = t.State, t.Error, t.Merged, t.Stale
			tv.Joint, tv.Lead = t.Joint, t.Lead
			if t.PullRequest != "" {
				tv.PullRequest = t.PullRequest
			}
		}
		v.Tasks = append(v.Tasks, tv)
	}
	return v
}

// ---- repositories

// rootInfo is a workspace folder and its repository's remotes.
type rootInfo struct {
	path, real string
	keys       []string // RepoKey of each remote
}

func (a *API) roots(ctx context.Context) []rootInfo {
	var out []rootInfo
	for _, root := range a.host.Roots() {
		ri := rootInfo{path: root}
		ri.real, _ = Resolve(root)
		if repo, err := a.host.Repository(ctx, root); err == nil && !repo.TaskWorktree {
			for _, u := range repo.Remotes {
				if k, ok := RepoKey(u); ok {
					ri.keys = append(ri.keys, k)
				}
			}
		}
		out = append(out, ri)
	}
	return out
}

// inWorkspace returns folder written under the open workspace folder that
// holds it (as the app compares paths), or "" when none does.
func inWorkspace(folder string, roots []rootInfo) string {
	real, err := Resolve(folder)
	if err != nil {
		return ""
	}
	for _, r := range roots {
		if r.real != "" && inside(real, r.real) {
			rel, _ := filepath.Rel(r.real, real)
			return filepath.Join(r.path, rel)
		}
	}
	return ""
}

func (a *API) checkouts() []Checkout {
	var list []Checkout
	_ = a.host.LoadState(stateCheckouts, &list)
	return list
}

func (a *API) resolveRepos(ctx context.Context, c Config) []RepositoryView {
	roots := a.roots(ctx)
	checkouts := a.checkouts()
	out := []RepositoryView{}
	for _, r := range c.Repositories {
		out = append(out, a.resolveRepo(ctx, r, roots, checkouts))
	}
	return out
}

// resolveRepo finds the checkout of r: a trusted workspace folder whose
// repository has a remote with r's URL, else the folder chosen with Locate…
// when it is still a checkout of r and trusted, else a workspace folder in
// Restricted Mode with that remote, else the located folder as it is. Any
// folder can claim any URL, so one in Restricted Mode never hides a trusted
// checkout (and the UI never offers to trust it in that one's place).
func (a *API) resolveRepo(ctx context.Context, r RepositoryDecl, roots []rootInfo, checkouts []Checkout) RepositoryView {
	v := RepositoryView{Name: r.Name, URL: r.URL}
	key, _ := RepoKey(r.URL)
	untrusted := ""
	for _, root := range roots {
		if !slices.Contains(root.keys, key) {
			continue
		}
		if a.host.Trusted(root.path) {
			v.Folder, v.InWorkspace, v.Trusted = root.path, true, true
			return v
		}
		if untrusted == "" {
			untrusted = root.path
		}
	}
	located := v
	if folder := checkoutOf(checkouts, key); folder != "" {
		located.Folder, located.Located = folder, true
		if err := a.checkCheckout(ctx, folder, key); err != nil {
			located.Problem = err.Error()
		} else {
			if p := inWorkspace(folder, roots); p != "" {
				located.Folder, located.InWorkspace = p, true
			}
			located.Trusted = a.host.Trusted(located.Folder)
		}
	}
	if located.Trusted || untrusted == "" {
		return located
	}
	v.Folder, v.InWorkspace = untrusted, true
	return v
}

// checkCheckout checks that folder is a checkout of the repository key.
func (a *API) checkCheckout(ctx context.Context, folder, key string) error {
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return fmt.Errorf("the folder %s is gone: Locate… the checkout again", folder)
	}
	repo, err := a.host.Repository(ctx, folder)
	if err != nil {
		return fmt.Errorf("%s is not a git checkout", folder)
	}
	if repo.TaskWorktree {
		return fmt.Errorf("%s is an agent task's worktree: choose the repository's own checkout", folder)
	}
	for _, u := range repo.Remotes {
		if k, ok := RepoKey(u); ok && k == key {
			return nil
		}
	}
	return fmt.Errorf("%s has no remote with this repository's URL", folder)
}

// Locate remembers dir as the checkout of the project's repository repo
// (Locate…; the UI asks for the folder with WorkspaceAPI.PickFolder). dir
// must be a checkout with a remote that has the repository's URL. The choice
// is kept in Flint's state, never in the project. A folder outside the
// workspace is attached to it by the UI before Delegate.
func (a *API) Locate(folder, repo, dir string) (RepositoryView, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return RepositoryView{}, err
	}
	c, err := LoadConfig(real)
	if err != nil {
		return RepositoryView{}, err
	}
	decl, ok := c.Repository(repo)
	if !ok {
		return RepositoryView{}, fmt.Errorf("the project has no repository %q", repo)
	}
	if _, err := requireDir(dir); err != nil {
		return RepositoryView{}, err
	}
	dir = filepath.Clean(dir)
	key, _ := RepoKey(decl.URL)
	ctx, cancel := a.ctx(gitTimeout)
	defer cancel()
	if err := a.checkCheckout(ctx, dir, key); err != nil {
		return RepositoryView{}, err
	}
	a.mu.Lock()
	err = a.host.SaveState(stateCheckouts, setCheckout(a.checkouts(), key, dir))
	a.mu.Unlock()
	if err != nil {
		return RepositoryView{}, err
	}
	a.emit(real)
	return a.resolveRepo(ctx, decl, a.roots(ctx), a.checkouts()), nil
}

// ---- writing items

// NewItem writes items/<id>.md from a template with title (the id is derived
// from it, made unique) and returns the item; the UI opens its file.
func (a *API) NewItem(folder, title string) (ItemView, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return ItemView{}, err
	}
	c, err := LoadConfig(real)
	if err != nil {
		return ItemView{}, err
	}
	if err := writable(c); err != nil {
		return ItemView{}, err
	}
	data, err := NewItemFile(title)
	if err != nil {
		return ItemView{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	it, err := a.writeNewItem(real, c, ItemID(title), func(string) ([]byte, error) { return data, nil })
	if err != nil {
		return ItemView{}, err
	}
	a.emit(real)
	return itemView(real, it, nil, nil), nil
}

// writeNewItem writes a new item file: its id is base, or base-2, base-3 …
// when taken (a file is never overwritten, and an id with a waiting start
// is not used), and file makes its content for
// that id (an error refuses the id, and stops). The project must have room
// (MaxItems). The written file must parse. Callers hold mu.
func (a *API) writeNewItem(real string, c Config, base string, file func(id string) ([]byte, error)) (Item, error) {
	if err := writable(c); err != nil {
		return Item{}, err
	}
	dir := filepath.Join(real, ItemsDir)
	if prompts.CountDir(dir, real, MaxItems) >= MaxItems {
		return Item{}, fmt.Errorf("the project has %d items, the most Flint reads: remove some first", MaxItems)
	}
	for n := 1; n <= 100; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		if _, err := os.Lstat(ItemFile(real, id)); err == nil {
			continue
		}
		if _, ok := a.waiting()[real][id]; ok {
			// A waiting start of an item whose file is gone: a new item
			// never takes its id (the start would not run it, but it would
			// show the old start's state).
			continue
		}
		data, err := file(id)
		if err != nil {
			return Item{}, err
		}
		it, err := ParseItem(id, data, c)
		if err != nil {
			return Item{}, err
		}
		_, err = prompts.WriteNew(dir, real, id+".md", string(data))
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return Item{}, err
		}
		return it, nil
	}
	return Item{}, fmt.Errorf("items named %s already exist: choose another title", base)
}

// SetStatus sets an item's status (the user's choice: any status to any
// other), rewriting only its status line.
func (a *API) SetStatus(folder, id, status string) (ItemView, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return ItemView{}, err
	}
	if !prompts.ValidName(id) {
		return ItemView{}, errors.New("invalid item id")
	}
	if !slices.Contains(Statuses, status) {
		return ItemView{}, fmt.Errorf("unknown status %q", status)
	}
	c, err := LoadConfig(real)
	if err != nil {
		return ItemView{}, err
	}
	if err := writable(c); err != nil {
		return ItemView{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	it, err := a.writeStatus(real, id, c, status)
	if err != nil {
		return ItemView{}, err
	}
	a.emit(real)
	a.kickStarter() // the last item another one waits for may be done now
	ctx, cancel := a.ctx(gitTimeout)
	defer cancel()
	ls := a.links()[real][id]
	v := itemView(real, it, ls, a.host.AgentTasks(ctx, taskIDs(map[string][]Link{id: ls})))
	a.viewDeps(real, &v)
	return v, nil
}

// writeStatus reads item id, checks it, and writes it back with status. The
// new file is written next to it first, and the file must still be what was
// read just before it is replaced, which leaves the smallest window for an
// editor's save to be lost. Callers hold mu.
func (a *API) writeStatus(folder, id string, c Config, status string) (Item, error) {
	if err := writable(c); err != nil {
		return Item{}, err
	}
	data, real, err := ReadItemFile(folder, id)
	if err != nil {
		return Item{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	it, err := ParseItem(id, data, c)
	if err != nil {
		return Item{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	if it.Status == status {
		return it, nil
	}
	out, err := SetStatus(data, status)
	if err != nil {
		return Item{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return Item{}, err
	}
	changed := func() bool {
		if a.beforeCompare != nil {
			a.beforeCompare()
		}
		now, err := readRegular(real)
		return err != nil || !bytes.Equal(now, data)
	}
	if err := replaceFile(real, out, info.Mode().Perm(), changed); err != nil {
		if errors.Is(err, errChanged) {
			return Item{}, fmt.Errorf("%s changed while Flint was writing it: try again", itemPath(id))
		}
		return Item{}, err
	}
	a.rehashWaiting(folder, id, data, out)
	it.Status = status
	return it, nil
}

// errChanged: replaceFile found the file changed.
var errChanged = errors.New("the file changed")

// replaceFile writes data to a temporary file next to path, then replaces
// path with it unless changed reports, at the last moment, that path is not
// what the caller read any more (errChanged).
func replaceFile(path string, data []byte, perm fs.FileMode, changed func() bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".flint-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after the rename
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(perm)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if changed() {
		return errChanged
	}
	return os.Rename(name, path)
}

// ---- links and automatic status changes

func (a *API) links() links {
	ls := links{}
	_ = a.host.LoadState(stateTasks, &ls)
	if ls == nil {
		ls = links{}
	}
	return ls
}

// reconcile refreshes the links of folder's items from their agent tasks and
// moves an item (NextStatus) only when something about its tasks changed
// since the last look: an item the user moved stays where they put it until
// its tasks change. The links of an item whose file can't be read now (gone
// for a moment during a branch switch or a rename, or not parsable) are left
// as they are and looked at when it is back; they go only with Close or
// pruning. A move that can't be written keeps the links as they were, so
// the next look tries again. It reports whether a status changed, and the
// tasks it read (nil when the project has none). Callers hold mu.
func (a *API) reconcile(ctx context.Context, folder string, c Config) (moved bool, tasks map[string]feature.AgentTask) {
	ls := a.links()
	items := ls[folder]
	if len(items) == 0 {
		return false, nil
	}
	tasks = a.host.AgentTasks(ctx, taskIDs(items))
	dirty := false
	for id, list := range items {
		if !prompts.ValidName(id) {
			delete(items, id)
			dirty = true
			continue
		}
		next, changed := Refresh(list, tasks)
		if !changed {
			continue
		}
		data, _, err := ReadItemFile(folder, id)
		if err != nil {
			continue
		}
		it, err := ParseItem(id, data, c)
		if err != nil {
			continue
		}
		if status := NextStatus(it.Status, next); status != it.Status {
			if _, err := a.writeStatus(folder, id, c, status); err != nil {
				continue
			}
			moved = true
		}
		items[id], dirty = next, true
	}
	if dirty {
		if len(items) == 0 {
			delete(ls, folder)
		}
		_ = a.host.SaveState(stateTasks, ls)
	}
	return moved, tasks
}

// reconcileAll reconciles every known project that has links, and tells the
// UI about the ones whose items moved.
func (a *API) reconcileAll(ctx context.Context) {
	known := a.knownSet()
	a.mu.Lock()
	var moved []string
	for folder := range a.links() {
		if !slices.Contains(known, folder) {
			continue
		}
		c, err := LoadConfig(folder)
		if err != nil || c.OldFormat {
			continue // an old-format project's statuses move once it is renamed
		}
		if m, _ := a.reconcile(ctx, folder, c); m {
			moved = append(moved, folder)
		}
	}
	a.mu.Unlock()
	for _, f := range moved {
		a.emit(f)
	}
}

// tasksChanged is the TasksChanged hook: it only wakes the watcher.
func (a *API) tasksChanged() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// watch reconciles when agent tasks changed, one pass at a time, until ctx
// ends. Nothing polls: it waits for TasksChanged.
func (a *API) watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.kick:
			rctx, cancel := context.WithTimeout(ctx, gitTimeout)
			a.reconcileAll(rctx)
			cancel()
			a.kickStarter() // an item another one waits for may be done now
		}
	}
}

// itemPath is an item's file relative to the project: items/<id>.md.
func itemPath(id string) string { return ItemsDir + "/" + id + ".md" }
