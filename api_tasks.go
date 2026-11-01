package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/git"
	"flint/internal/github"
	"flint/internal/redact"
)

// taskChangesLimit bounds the files listed for one task (the counts cover all).
const taskChangesLimit = 500

var errPRDeclined = errors.New("pull request not created: cancelled")

// prCreator opens pull requests (github.Client; a fake in tests).
type prCreator interface {
	CreatePullRequest(ctx context.Context, dir string, p github.NewPullRequest) (string, error)
}

// TaskAPI runs agent tasks: a description handed to an agent that works on
// its own branch in its own worktree, listed in the Tasks panel. Everything
// the web view sends is checked here: tasks are named by id only, and new
// ones start from a folder of the open workspace.
type TaskAPI struct {
	app *App
	git *GitAPI
	pr  prCreator
}

// NewTask is what the New Agent Task form sends.
type NewTask struct {
	Dir         string `json:"dir"` // a folder of the workspace inside the repository
	Description string `json:"description"`
	Agent       string `json:"agent"`
	Base        string `json:"base"` // local branch to start from; "" = Dir's current branch
}

// TaskView is a task as the panel shows it.
type TaskView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Prompt      string `json:"prompt"`
	Agent       string `json:"agent"`
	Branch      string `json:"branch"`
	Base        string `json:"base"`
	Worktree    string `json:"worktree"`
	Checkout    string `json:"checkout"`
	CreatedAt   int64  `json:"createdAt"`
	PullRequest string `json:"pullRequest"`
	// State: idle, working, waiting, finished or failed (agenttask).
	State string `json:"state"`
	Error string `json:"error"`
	// Stale: the worktree folder is gone; the task can only be removed.
	Stale bool `json:"stale"`
	// Ahead counts the branch's commits that base doesn't have.
	Ahead int `json:"ahead"`
	// Merged: the branch has commits and all of them are in base.
	Merged bool `json:"merged"`
}

// MergeResult tells whether a task was merged or which files conflict.
type MergeResult struct {
	Merged    bool     `json:"merged"`
	Conflicts []string `json:"conflicts"`
	// Into is the checkout of the base branch the merge ran (or would run) in.
	Into string `json:"into"`
}

// RemoveOptions are the user's answers to the questions Remove may need.
type RemoveOptions struct {
	// DiscardChanges deletes a worktree with uncommitted changes or new files.
	DiscardChanges bool `json:"discardChanges"`
	// DeleteBranch deletes the branch although base lacks some of its commits.
	DeleteBranch bool `json:"deleteBranch"`
}

// RemoveInfo is what removing a task would lose, for the confirmation.
type RemoveInfo struct {
	Stale bool `json:"stale"`
	// Changes counts uncommitted changes and new files in the worktree.
	Changes int `json:"changes"`
	// Ignored counts ignored files and folders (.env, build output), which
	// deleting the worktree deletes too.
	Ignored int `json:"ignored"`
	// Unmerged counts the branch's commits base doesn't have.
	Unmerged int `json:"unmerged"`
}

// RemoveResult reports what Remove kept.
type RemoveResult struct {
	BranchKept bool `json:"branchKept"`
}

// task looks a task up by an id from the web view.
func (t *TaskAPI) task(id string) (agenttask.Task, error) {
	if !agenttask.ValidID(id) {
		return agenttask.Task{}, errors.New("invalid task id")
	}
	_, task, ok := t.app.taskStore().Find(id)
	if !ok {
		return agenttask.Task{}, errors.New("no such task (it was removed)")
	}
	return task, nil
}

// liveTask is a task whose worktree still exists.
func (t *TaskAPI) liveTask(id string) (agenttask.Task, error) {
	task, err := t.task(id)
	if err == nil && !isDir(task.Worktree) {
		err = fmt.Errorf("the worktree of %q is gone (%s): remove the task", task.Title, task.Worktree)
	}
	return task, err
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (t *TaskAPI) ctx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(t.app.ctx, timeout)
}

// maxListDirs bounds the folders one ListAll call accepts (the workspace roots).
const maxListDirs = 64

// List returns the tasks of the repository containing dir (none outside one).
func (t *TaskAPI) List(dir string) ([]TaskView, error) { return t.ListAll([]string{dir}) }

// ListAll returns the tasks of the repositories containing dirs (the
// workspace folders), each repository once however many of its worktrees
// are open. A task whose worktree vanished loses the trust Flint gave it.
func (t *TaskAPI) ListAll(dirs []string) ([]TaskView, error) {
	if len(dirs) > maxListDirs {
		return nil, fmt.Errorf("too many folders (max %d)", maxListDirs)
	}
	ctx, cancel := t.ctx(gitTimeout)
	defer cancel()
	out := []TaskView{}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		r, err := t.git.repo(ctx, dir)
		if errors.Is(err, git.ErrNotRepository) {
			continue
		}
		if err != nil {
			return nil, err
		}
		key := cleanFolder(r.CommonDir)
		if seen[key] {
			continue
		}
		seen[key] = true
		for _, task := range t.app.taskStore().List(key) {
			v := t.view(ctx, r, task)
			if v.Stale {
				t.app.untrustTask(task)
			}
			out = append(out, v)
		}
	}
	return out, nil
}

// untrustTask removes the trust Flint gave a task's worktree, if any.
func (a *App) untrustTask(task agenttask.Task) error {
	if !slices.Contains(a.trustedFolders(), task.Worktree) {
		return nil
	}
	err := a.setTrust(task.Worktree, false)
	if err != nil {
		a.logf("untrust task worktree: %v", err)
	}
	return err
}

func (t *TaskAPI) view(ctx context.Context, r *git.Repo, task agenttask.Task) TaskView {
	live := t.app.tasks.State(task.ID, task.State)
	if live.State == agenttask.Failed && live.Error == "" {
		live.Error = task.Error
	}
	v := TaskView{
		ID: task.ID, Title: task.Title, Prompt: task.Prompt, Agent: task.Agent, Branch: task.Branch, Base: task.Base,
		Worktree: task.Worktree, Checkout: task.Checkout, CreatedAt: task.CreatedAt, PullRequest: task.PullRequest,
		State: live.State, Error: live.Error, Stale: !isDir(task.Worktree),
	}
	v.Ahead, _ = r.Ahead(ctx, task.Base, task.Branch)
	if v.Ahead == 0 {
		// Base has every commit of the branch, and the branch has some of its
		// own (Update from base always adds a merge commit, so a task without
		// work never fast-forwards past where it started).
		tip, err := r.Resolve(ctx, git.HeadRef(task.Branch))
		v.Merged = err == nil && tip != task.BaseCommit && r.IsAncestor(ctx, git.HeadRef(task.Branch), git.HeadRef(task.Base))
	}
	return v
}

// Changes lists what a task changed since it left its base (committed or
// not, new files included) with line counts.
func (t *TaskAPI) Changes(id string) (git.DiffStat, error) {
	task, err := t.liveTask(id)
	if err != nil {
		return git.DiffStat{}, err
	}
	return with(t.git, task.Worktree, gitTimeout, func(ctx context.Context, r *git.Repo) (git.DiffStat, error) {
		return r.DiffStat(ctx, task.Base, taskChangesLimit)
	})
}

// Create makes the task's branch and worktree and remembers the task. The UI
// then opens the folder and starts the chat with the description.
func (t *TaskAPI) Create(req NewTask) (TaskView, error) {
	a := t.app
	desc := strings.TrimSpace(req.Description)
	switch {
	case desc == "":
		return TaskView{}, errors.New("describe the task first")
	case len(desc) > agenttask.MaxPrompt:
		return TaskView{}, fmt.Errorf("the description is longer than %d characters", agenttask.MaxPrompt)
	case strings.ContainsRune(desc, 0):
		return TaskView{}, errors.New("the description contains a NUL character")
	}
	if _, err := a.rootOf(req.Dir); err != nil {
		return TaskView{}, err
	}
	s := a.currentSettings()
	if !slices.Contains(installedAgents(s, exec.LookPath), req.Agent) {
		return TaskView{}, fmt.Errorf("the agent %q is not installed", req.Agent)
	}
	ctx, cancel := t.ctx(gitRemoteTimeout) // checkout hooks may run
	defer cancel()
	r, err := t.git.repo(ctx, req.Dir)
	if err != nil {
		return TaskView{}, err
	}
	base, err := taskBase(ctx, r, req.Base)
	if err != nil {
		return TaskView{}, err
	}
	baseCommit, err := r.Resolve(ctx, git.HeadRef(base))
	if err != nil {
		return TaskView{}, err
	}
	wts, err := r.Worktrees(ctx)
	if err != nil || len(wts) == 0 {
		return TaskView{}, fmt.Errorf("list worktrees: %v", err)
	}
	checkout := wts[0].Path
	home, _ := os.UserHomeDir()
	pathOf := func(branch string) string {
		return agenttask.WorktreePath(s.AgentTasks.WorktreeDir, home, checkout, branch)
	}
	branch, err := agenttask.Unique(s.AgentTasks.BranchPrefix+agenttask.Slug(desc), func(b string) bool {
		_, statErr := os.Lstat(pathOf(b))
		return r.BranchExists(ctx, b) || statErr == nil
	})
	if err != nil {
		return TaskView{}, err
	}
	path := pathOf(branch)
	if err := a.checkTaskLocation(r.Root, checkout, path); err != nil {
		return TaskView{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return TaskView{}, fmt.Errorf("create the worktree folder: %w", err)
	}
	if err := r.AddWorktree(ctx, path, branch, true, git.HeadRef(base)); err != nil {
		return TaskView{}, err
	}
	path = cleanFolder(path)
	task := agenttask.Task{
		ID: agenttask.NewID(), Title: agenttask.Title(desc), Prompt: redact.Secrets(desc), Agent: req.Agent,
		Branch: branch, Base: base, BaseCommit: baseCommit, Worktree: path, Checkout: cleanFolder(checkout),
		CreatedAt: time.Now().Unix(),
	}
	err = a.saveTasks(func(st agenttask.Store) (agenttask.Store, error) { return st.Add(cleanFolder(r.CommonDir), task) })
	if err == nil && a.isTrusted(r.Root) {
		// The worktree holds the repository's own commits, which the user
		// trusts in the checkout the task started from.
		err = a.setTrust(path, true)
	}
	if err != nil {
		_ = r.RemoveWorktree(ctx, path, true)
		_ = r.DeleteBranch(ctx, branch, true)
		_ = a.saveTasks(func(st agenttask.Store) (agenttask.Store, error) { return st.Remove(task.ID), nil })
		return TaskView{}, err
	}
	a.emit(eventTasksChanged)
	return t.view(ctx, r, task), nil
}

// checkTaskLocation refuses a worktree path inside the checkout the task
// starts from (or the main checkout), and, when that checkout is untrusted,
// one inside a trusted folder: trust is by path, so the worktree would be
// trusted by where it lies although its repository is not.
func (a *App) checkTaskLocation(source, checkout, path string) error {
	p := as.Resolve(path) // symlinks of the existing part, as trust compares them
	for _, root := range []string{source, checkout} {
		if r := cleanFolder(root); within(p, r) {
			return fmt.Errorf("the worktree folder %s would be inside %s: choose a folder outside the repository in Settings ▸ Agents ▸ Agent tasks", path, r)
		}
	}
	if !a.isTrusted(source) && a.isTrusted(p) {
		return fmt.Errorf("the worktree folder %s is inside a trusted folder, but this repository is not trusted: trust the repository first, or choose a worktree folder outside trusted folders", path)
	}
	return nil
}

// taskBase checks the requested base branch, or picks the current one.
func taskBase(ctx context.Context, r *git.Repo, base string) (string, error) {
	if base == "" {
		st, err := r.Status(ctx)
		if err != nil {
			return "", err
		}
		if st.Branch == "" {
			return "", errors.New("the checkout is on a detached HEAD: pick a base branch")
		}
		return st.Branch, nil
	}
	if !r.BranchExists(ctx, base) {
		return "", fmt.Errorf("%q is not a local branch: a task merges back into the branch it started from", base)
	}
	return base, nil
}

// Merge merges a task's branch into its base, in the checkout where base is
// checked out. It refuses while the task or that checkout has uncommitted
// changes, and reports conflicts without touching anything.
func (t *TaskAPI) Merge(id string) (MergeResult, error) {
	task, err := t.liveTask(id)
	if err != nil {
		return MergeResult{}, err
	}
	ctx, cancel := t.ctx(gitRemoteTimeout) // merge hooks may run
	defer cancel()
	wr, err := t.git.repo(ctx, task.Worktree)
	if err != nil {
		return MergeResult{}, err
	}
	if err := checkMergeable(ctx, wr, task); err != nil {
		return MergeResult{}, err
	}
	into, err := baseCheckout(ctx, wr, task)
	if err != nil {
		return MergeResult{}, err
	}
	ir, err := t.git.repo(ctx, into)
	if err != nil {
		return MergeResult{}, err
	}
	if dirty, err := ir.Dirty(ctx); err != nil || dirty {
		return MergeResult{}, errors.Join(err, fmt.Errorf("%s (where %s is checked out) has uncommitted changes or an unfinished merge: commit or stash them first", into, task.Base))
	}
	res := MergeResult{Into: into, Conflicts: []string{}}
	files, err := ir.MergeConflicts(ctx, task.Base, task.Branch)
	if err != nil {
		return MergeResult{}, err
	}
	if len(files) > 0 {
		res.Conflicts = files
		return res, nil
	}
	var conflict *git.MergeConflictError
	if err := ir.Merge(ctx, task.Branch); errors.As(err, &conflict) {
		res.Conflicts = conflict.Files
		return res, nil
	} else if err != nil {
		return MergeResult{}, err
	}
	res.Merged = true
	t.app.emit(eventTasksChanged)
	return res, nil
}

func checkMergeable(ctx context.Context, wr *git.Repo, task agenttask.Task) error {
	ahead, err := wr.Ahead(ctx, task.Base, task.Branch)
	if err != nil {
		return err
	}
	if ahead == 0 {
		return fmt.Errorf("%s has no commits beyond %s: there is nothing to merge yet (commit the agent's work first)", task.Branch, task.Base)
	}
	if dirty, err := wr.Dirty(ctx); err != nil || dirty {
		return errors.Join(err, fmt.Errorf("the task has uncommitted changes in %s: commit them first (or ask the agent to), then merge", task.Worktree))
	}
	return nil
}

// baseCheckout is the worktree where the task's base branch is checked out.
func baseCheckout(ctx context.Context, r *git.Repo, task agenttask.Task) (string, error) {
	wts, err := r.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if wt.Branch == task.Base && !wt.Prunable {
			return wt.Path, nil
		}
	}
	return "", fmt.Errorf("%s is not checked out anywhere: check it out (for example in %s) to merge into it", task.Base, task.Checkout)
}

// UpdateFromBase merges the base branch into the task's branch, in the task's
// worktree, and leaves conflicts there for an agent to resolve. It returns
// the conflicted files (none: the base merged cleanly).
func (t *TaskAPI) UpdateFromBase(id string) ([]string, error) {
	task, err := t.liveTask(id)
	if err != nil {
		return nil, err
	}
	files, err := with(t.git, task.Worktree, gitRemoteTimeout, func(ctx context.Context, r *git.Repo) ([]string, error) {
		if dirty, err := r.Dirty(ctx); err != nil || dirty {
			return nil, errors.Join(err, fmt.Errorf("the task has uncommitted changes in %s: commit them first", task.Worktree))
		}
		return r.MergeInto(ctx, task.Base)
	})
	if err != nil {
		return nil, err
	}
	t.app.emit(eventTasksChanged)
	return append([]string{}, files...), nil
}

// CreatePullRequest pushes the task's branch and opens a pull request into
// its base through the GitHub API, after the user confirmed in a native dialog (it
// publishes the branch and the description). It returns the URL.
func (t *TaskAPI) CreatePullRequest(id string) (string, error) {
	task, err := t.liveTask(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := t.ctx(gitRemoteTimeout)
	defer cancel()
	wr, err := t.git.repo(ctx, task.Worktree)
	if err != nil {
		return "", err
	}
	ahead, err := wr.Ahead(ctx, task.Base, task.Branch)
	if err != nil {
		return "", err
	}
	if ahead == 0 {
		return "", fmt.Errorf("%s has no commits beyond %s yet: commit the agent's work first", task.Branch, task.Base)
	}
	note := ""
	if dirty, _ := wr.Dirty(ctx); dirty {
		note = "\n\nUncommitted changes in the worktree are not included."
	}
	msg := fmt.Sprintf("Flint will push the branch %s (%d commit(s)) and open a pull request into %s on GitHub titled:\n\n%s\n\nIts description is the task's text.%s",
		task.Branch, ahead, task.Base, task.Title, note)
	if !t.app.askUser("Create a pull request?", msg, "Push and Create") {
		return "", errPRDeclined
	}
	remote, err := wr.PushRemote(ctx)
	if err != nil {
		return "", err
	}
	if err := wr.PushBranch(ctx, task.Branch); err != nil {
		return "", err
	}
	url, err := t.pr.CreatePullRequest(ctx, task.Worktree, github.NewPullRequest{
		Base: task.Base, Head: task.Branch, Title: task.Title, Body: task.Prompt, HeadRemote: remote,
	})
	if err != nil {
		return "", err
	}
	err = t.app.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		return s.Update(id, func(x agenttask.Task) agenttask.Task { x.PullRequest = url; return x }), nil
	})
	if err != nil {
		t.app.logf("save pull request of task: %v", err)
	}
	t.app.emit(eventTasksChanged)
	return url, nil
}

// askUser asks in a native dialog (a.ask in tests), one dialog at a time.
func (a *App) askUser(title, message, ok string) bool {
	a.confirmMu.Lock()
	defer a.confirmMu.Unlock()
	if a.ask != nil {
		return a.ask(title, message, ok)
	}
	return a.askNative(title, message, ok)
}

// RemoveCheck tells what removing a task would lose, for the confirmation.
func (t *TaskAPI) RemoveCheck(id string) (RemoveInfo, error) {
	task, err := t.task(id)
	if err != nil {
		return RemoveInfo{}, err
	}
	if !isDir(task.Worktree) {
		return RemoveInfo{Stale: true, Unmerged: t.unmerged(task)}, nil
	}
	info, err := with(t.git, task.Worktree, gitTimeout, func(ctx context.Context, r *git.Repo) (RemoveInfo, error) {
		st, err := r.Status(ctx)
		if err != nil {
			return RemoveInfo{}, err
		}
		ignored, err := r.IgnoredFiles(ctx)
		return RemoveInfo{Changes: len(st.Files), Ignored: ignored}, err
	})
	if err != nil {
		return RemoveInfo{}, err
	}
	info.Unmerged = t.unmerged(task)
	return info, nil
}

// unmerged counts the branch's own commits (-1 when unknown: base is gone).
func (t *TaskAPI) unmerged(task agenttask.Task) int {
	n, err := with(t.git, task.Checkout, gitTimeout, func(ctx context.Context, r *git.Repo) (int, error) {
		if !r.BranchExists(ctx, task.Branch) {
			return 0, nil
		}
		return r.Ahead(ctx, task.Base, task.Branch)
	})
	if err != nil {
		return -1
	}
	return n
}

// Remove stops the task's chats, deletes its worktree and forgets it. The
// branch goes too when base has all of its commits, or when the user said
// so; uncommitted work is only deleted when the user said so.
func (t *TaskAPI) Remove(id string, opts RemoveOptions) (RemoveResult, error) {
	task, err := t.task(id)
	if err != nil {
		return RemoveResult{}, err
	}
	a := t.app
	if roots := a.projectRoots(); len(roots) > 0 && roots[0] == task.Worktree {
		return RemoveResult{}, errors.New("the task's folder is the main project of this window: open another folder first")
	}
	info, err := t.RemoveCheck(id)
	if err != nil {
		return RemoveResult{}, err
	}
	if info.Changes+info.Ignored > 0 && !opts.DiscardChanges {
		return RemoveResult{}, fmt.Errorf("%s has %d uncommitted change(s) or new file(s) and %d ignored file(s) or folder(s)",
			task.Worktree, info.Changes, info.Ignored)
	}
	for _, s := range a.tasks.Sessions(id) {
		if a.agents != nil {
			_ = a.agents.Stop(s)
		}
	}
	a.tasks.Forget(id)
	res := RemoveResult{}
	err = do(t.git, task.Checkout, gitTimeout, func(ctx context.Context, r *git.Repo) error {
		if info.Stale {
			// Only this task's entry: other worktrees may just be unmounted.
			if err := r.RemoveWorktreeEntry(ctx, task.Worktree); err != nil && !errors.Is(err, git.ErrNoWorktree) {
				return err
			}
		} else if err := r.RemoveWorktree(ctx, task.Worktree, opts.DiscardChanges); err != nil {
			return err
		}
		if !r.BranchExists(ctx, task.Branch) {
			return nil
		}
		// Checked again right before deleting: commits made since RemoveCheck count.
		if ahead, err := r.Ahead(ctx, task.Base, task.Branch); (err != nil || ahead > 0) && !opts.DeleteBranch {
			res.BranchKept = true
			return nil
		}
		return r.DeleteBranch(ctx, task.Branch, true)
	})
	if err != nil {
		return RemoveResult{}, err
	}
	// Trust goes before the task: a task that is forgotten can't be untrusted later.
	if err := a.untrustTask(task); err != nil {
		return RemoveResult{}, err
	}
	err = a.saveTasks(func(s agenttask.Store) (agenttask.Store, error) { return s.Remove(id), nil })
	a.emit(eventTasksChanged)
	return res, err
}
