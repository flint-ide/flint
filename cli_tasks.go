package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"flint/internal/agenttask"
	"flint/internal/cli"
)

// The agent tasks' commands of the flint command line (`flint task …`):
// what the Agent Tasks panel does, through TaskAPI's own methods. Where the
// panel's next step is the UI's (starting a new task's chat), Go takes it,
// as it does for the tasks features make.

func (t *TaskAPI) cliCommands() []cli.Command {
	idArg := []cli.Arg{{Name: "task", Max: 128}}
	return []cli.Command{
		{
			Path: "task list", Summary: "List the agent tasks of the folder's repository", Kind: cli.Reads, Folder: true,
			Flags: []cli.Flag{{Name: "all", Usage: "the tasks of every folder open in Flint"}},
			Run:   t.cliList,
		},
		{
			Path: "task show", Summary: "Show a task: state, branch, worktree, pull request", Kind: cli.Reads, Args: idArg,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				v, err := t.cliView(c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: taskText(v), JSON: v}, nil
			},
		},
		{
			Path: "task new", Summary: "Hand a description to an agent in a new branch and worktree", Kind: cli.Acts, Folder: true,
			Flags: []cli.Flag{
				{Name: "description", Value: "<text>", Usage: "what the agent should do", Required: true, File: true, Max: agenttask.MaxPrompt},
				{Name: "agent", Value: "<agent>", Usage: "who works on it (default: the default agent)", Max: 200},
				{Name: "base", Value: "<branch>", Usage: "the local branch to start from (default: the folder's current one)", Max: 256},
			},
			Help: "Like New Agent Task…: the folder must be open in Flint. Flint starts the chat, or queues the task\n" +
				"when agentTasks.maxParallel tasks work already.",
			Agents: cli.AgentAsks,
			Run:    t.cliNew,
		},
		{
			Path: "task start", Summary: "Start a queued task now", Kind: cli.Acts, Args: idArg,
			Agents: cli.AgentAsks, AgentCheck: t.agentTaskCheck,
			Run: t.cliStart,
		},
		{
			Path: "task diff", Summary: "List the files a task changed since its base, with line counts", Kind: cli.Reads, Args: idArg,
			Run: t.cliDiff,
		},
		{
			Path: "task compare", Summary: "Compare the tasks of a group, file by file", Kind: cli.Reads,
			Args: []cli.Arg{{Name: "group", Max: 128}},
			Run:  t.cliCompare,
		},
		{
			Path: "task update", Summary: "Merge the task's base into its branch (Update from base)", Kind: cli.Acts, Args: idArg,
			Agents: cli.AgentAsks, AgentCheck: t.agentTaskCheck,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				files, err := t.UpdateFromBase(c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				if files == nil {
					files = []string{}
				}
				res := cli.Result{Text: "Updated from base.", JSON: map[string]any{"conflicts": files}}
				if len(files) > 0 {
					res.Text = "Conflicts to resolve in the task's worktree:\n  " + strings.Join(files, "\n  ")
					res.Exit = cli.ExitFailed
				}
				return res, nil
			},
		},
		{
			Path: "task merge", Summary: "Merge the task's branch into its base", Kind: cli.Acts, Args: idArg,
			Agents: cli.AgentAsks, AgentCheck: t.agentTaskCheck,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				r, err := t.Merge(c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				if r.Conflicts == nil {
					r.Conflicts = []string{}
				}
				res := cli.Result{Text: "Merged into " + r.Into, JSON: r}
				if !r.Merged {
					res.Text = fmt.Sprintf("Not merged: these files conflict in %s:\n  %s", r.Into, strings.Join(r.Conflicts, "\n  "))
					res.Exit = cli.ExitFailed
				}
				return res, nil
			},
		},
		{
			Path: "task pr", Summary: "Push the branch and open a pull request", Kind: cli.Acts, Args: idArg,
			Agents: cli.AgentAsks, AgentCheck: t.agentTaskCheck,
			Help: "Flint shows what it will push in its own dialog first; the command waits for your answer.",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				url, err := t.CreatePullRequest(c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: url, JSON: map[string]string{"url": url}}, nil
			},
		},
		{
			Path: "task remove", Summary: "Stop the task's chats, delete its worktree and forget it", Kind: cli.Acts, Args: idArg,
			Agents: cli.AgentAsks, AgentCheck: t.agentTaskCheck,
			Help: "When the worktree has uncommitted or ignored files, or the branch has commits its base lacks,\n" +
				"Flint asks in its own dialog first; the command waits for your answer.",
			Run: t.cliRemove,
		},
	}
}

// agentTaskCheck refuses an agent's command on a task of a repository in
// Restricted Mode (cli.Command.AgentCheck).
func (t *TaskAPI) agentTaskCheck(_ context.Context, c *cli.Call) error {
	if !agenttask.ValidID(c.Arg(0)) {
		return errors.New("invalid task id")
	}
	repo, _, ok := t.app.taskStore().Find(c.Arg(0))
	if !ok {
		return errors.New("no such task (it was removed)")
	}
	if filepath.Base(repo) == ".git" {
		repo = filepath.Dir(repo) // tasks are kept by the repository's git directory
	}
	if !t.app.isTrusted(repo) {
		return fmt.Errorf("the task's repository %s is in Restricted Mode in Flint: agents cannot act on its tasks", repo)
	}
	return nil
}

// cliFolder checks a folder from the command line like the panel's: inside a
// folder open in Flint.
func (t *TaskAPI) cliFolder(folder string) error {
	if folder == "" {
		return errors.New("choose a folder")
	}
	_, err := t.app.rootOf(folder)
	if err != nil {
		return fmt.Errorf("%s is not in a folder open in Flint", folder)
	}
	return nil
}

func (t *TaskAPI) cliList(_ context.Context, c *cli.Call) (cli.Result, error) {
	var views []TaskView
	var err error
	if c.Bool("all") {
		views, err = t.ListAll(t.app.projectRoots())
	} else if err = t.cliFolder(c.Folder); err == nil {
		views, err = t.List(c.Folder)
	}
	if err != nil {
		return cli.Result{}, err
	}
	rows := make([][]string, len(views))
	for i, v := range views {
		state := v.State
		if v.Stale {
			state += " (folder gone)"
		}
		if v.Blocked != "" {
			state += " (" + v.Blocked + ")"
		}
		rows[i] = []string{v.ID, state, v.Agent, v.Branch, cli.Cut(v.Title, 60)}
	}
	text := cli.Table([]string{"TASK", "STATE", "AGENT", "BRANCH", "TITLE"}, rows)
	if len(views) == 0 {
		text = "No agent tasks.\n"
	}
	return cli.Result{Text: text, JSON: views}, nil
}

// cliView is one task as the panel shows it.
func (t *TaskAPI) cliView(id string) (TaskView, error) {
	task, err := t.task(id)
	if err != nil {
		return TaskView{}, err
	}
	ctx, cancel := t.ctx(gitTimeout)
	defer cancel()
	dir := task.Worktree
	if !isDir(dir) {
		dir = task.Checkout
	}
	r, err := t.git.repo(ctx, dir)
	if err != nil {
		return TaskView{}, err
	}
	views := []TaskView{t.view(ctx, r, task)}
	t.addUsage(views)
	return views[0], nil
}

func taskText(v TaskView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", v.ID, v.Title)
	fmt.Fprintf(&b, "State:    %s\n", v.State)
	if v.Error != "" {
		fmt.Fprintf(&b, "Error:    %s\n", v.Error)
	}
	if v.Blocked != "" {
		fmt.Fprintf(&b, "Waits:    %s\n", v.Blocked)
	}
	fmt.Fprintf(&b, "Agent:    %s\n", v.Agent)
	fmt.Fprintf(&b, "Branch:   %s (from %s, %d commit(s) ahead", v.Branch, v.Base, v.Ahead)
	if v.Merged {
		b.WriteString(", merged")
	}
	b.WriteString(")\n")
	fmt.Fprintf(&b, "Worktree: %s", v.Worktree)
	if v.Stale {
		b.WriteString(" (gone)")
	}
	b.WriteString("\n")
	if v.PullRequest != "" {
		fmt.Fprintf(&b, "PR:       %s\n", v.PullRequest)
	}
	if v.Group != "" {
		fmt.Fprintf(&b, "Group:    %s (flint task compare %s)\n", v.Group, v.Group)
	}
	if v.Tokens > 0 {
		fmt.Fprintf(&b, "Usage:    %d tokens, $%.2f\n", v.Tokens, v.CostUSD)
	}
	fmt.Fprintf(&b, "Created:  %s\n", time.Unix(v.CreatedAt, 0).Format("2006-01-02 15:04"))
	return b.String()
}

func (t *TaskAPI) cliNew(_ context.Context, c *cli.Call) (cli.Result, error) {
	if err := t.cliFolder(c.Folder); err != nil {
		return cli.Result{}, err
	}
	views, err := t.CreateGroup(NewTaskGroup{Dir: c.Folder, Description: c.String("description"), Base: c.String("base"), Agents: []string{c.String("agent")}})
	if err != nil {
		return cli.Result{}, err
	}
	v := views[0]
	text := fmt.Sprintf("Created task %s on %s in %s", v.ID, v.Branch, v.Worktree)
	if v.State == agenttask.Queued {
		text += "\nIt waits in the queue (flint task start " + v.ID + " starts it now)."
	} else {
		t.startChat(v.ID)
	}
	t.app.tasksChanged()
	return cli.Result{Text: text, JSON: v}, nil
}

// startChat starts the chat of a task that holds a start slot, as the
// queue and the features' tasks do; startTaskChat releases the slot.
func (t *TaskAPI) startChat(id string) {
	task, err := t.task(id)
	if err != nil {
		t.app.releaseStart(id)
		return
	}
	a := t.app
	a.queue.starts.Add(1)
	go func() {
		defer a.queue.starts.Done()
		a.startTaskChat(task)
	}()
}

func (t *TaskAPI) cliStart(_ context.Context, c *cli.Call) (cli.Result, error) {
	v, err := t.StartNow(c.Arg(0))
	if err != nil {
		return cli.Result{}, err
	}
	if !v.FlintStarts {
		t.startChat(v.ID) // the panel starts this one's chat itself
	}
	return cli.Result{Text: "Started task " + v.ID, JSON: v}, nil
}

func (t *TaskAPI) cliDiff(_ context.Context, c *cli.Call) (cli.Result, error) {
	st, err := t.Changes(c.Arg(0))
	if err != nil {
		return cli.Result{}, err
	}
	rows := make([][]string, len(st.Files))
	for i, f := range st.Files {
		path := f.Path
		if f.OrigPath != "" {
			path = f.OrigPath + " → " + f.Path
		}
		add, del := fmt.Sprintf("+%d", f.Added), fmt.Sprintf("-%d", f.Deleted)
		if f.Binary {
			add, del = "binary", ""
		}
		if f.Untracked {
			path += " (new)"
		}
		rows[i] = []string{add, del, path}
	}
	text := cli.Table(nil, rows) + fmt.Sprintf("%d file(s) changed, +%d -%d\n", st.Count, st.Added, st.Deleted)
	if st.Count == 0 {
		text = "No changes.\n"
	}
	return cli.Result{Text: text, JSON: st}, nil
}

func (t *TaskAPI) cliCompare(_ context.Context, c *cli.Call) (cli.Result, error) {
	cmp, err := t.Compare(c.Arg(0))
	if err != nil {
		return cli.Result{}, err
	}
	header := []string{"FILE"}
	var totals []string
	for _, ct := range cmp.Tasks {
		header = append(header, ct.Task.ID+" ("+ct.Task.Agent+")")
		tot := fmt.Sprintf("%d file(s) +%d -%d", ct.Count, ct.Added, ct.Deleted)
		if ct.Error != "" {
			tot = "error: " + ct.Error
		}
		totals = append(totals, tot)
	}
	rows := make([][]string, 0, len(cmp.Files)+1)
	for _, f := range cmp.Files {
		r := []string{f.Path}
		for _, ch := range f.Changes {
			switch {
			case ch == nil:
				r = append(r, "-")
			case ch.Binary:
				r = append(r, "binary")
			default:
				r = append(r, fmt.Sprintf("+%d -%d", ch.Added, ch.Deleted))
			}
		}
		rows = append(rows, r)
	}
	rows = append(rows, append([]string{"(total)"}, totals...))
	text := cli.Table(header, rows)
	if cmp.Truncated {
		text += "More files changed than are listed.\n"
	}
	return cli.Result{Text: text, JSON: cmp}, nil
}

// cliRemove removes a task. What the panel asks in the web view before it
// deletes work (uncommitted or ignored files, a branch with commits its base
// lacks) Flint asks here in a native dialog, which no program can answer.
func (t *TaskAPI) cliRemove(_ context.Context, c *cli.Call) (cli.Result, error) {
	id := c.Arg(0)
	task, err := t.task(id)
	if err != nil {
		return cli.Result{}, err
	}
	info, err := t.RemoveCheck(id)
	if err != nil {
		return cli.Result{}, err
	}
	var opts RemoveOptions
	if q := removeDiscardQuestion(info); q != "" {
		if !t.app.askUser("Remove the agent task?", fmt.Sprintf("%q (%s): %s", task.Title, task.Branch, q), "Delete them") {
			return cli.Result{}, errors.New("not removed: cancelled")
		}
		opts.DiscardChanges = true
	}
	if q := removeBranchQuestion(info); q != "" {
		opts.DeleteBranch = t.app.askUser("Delete the branch?", fmt.Sprintf("%q (%s): %s", task.Title, task.Branch, q), "Delete Branch")
	}
	res, err := t.Remove(id, opts)
	if err != nil {
		return cli.Result{}, err
	}
	text := "Removed task " + id
	if res.BranchKept {
		text += "; the branch " + task.Branch + " was kept"
	}
	return cli.Result{Text: text, JSON: res}, nil
}

// removeDiscardQuestion and removeBranchQuestion are the panel's questions
// (frontend/src/lib/agentTasks.ts, removeQuestions); "" when none is needed.
func removeDiscardQuestion(info RemoveInfo) string {
	var lost []string
	if info.Changes > 0 {
		lost = append(lost, fmt.Sprintf("%d uncommitted change(s) or new file(s)", info.Changes))
	}
	if info.Ignored > 0 {
		lost = append(lost, fmt.Sprintf("%d ignored file(s) or folder(s), such as .env or build output", info.Ignored))
	}
	if len(lost) == 0 {
		return ""
	}
	return "Its worktree has " + strings.Join(lost, " and ") + ". Delete them with it?"
}

func removeBranchQuestion(info RemoveInfo) string {
	switch {
	case info.Unmerged > 0:
		return fmt.Sprintf("The branch has %d commit(s) that are not in its base. Delete the branch too?", info.Unmerged)
	case info.Unmerged < 0:
		return "Flint could not tell whether the branch was merged (its base is gone). Delete the branch too?"
	}
	return ""
}
