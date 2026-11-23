package projects

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/cli"
)

// cliCommands are the feature's commands of the flint command line
// (`flint project …`): everything the Projects panel and the board do,
// through the same functions as the bound API.
func (a *API) cliCommands() []cli.Command {
	projectFlag := cli.Flag{Name: "project", Value: "<folder|name>", Usage: "the project (default: the one the current directory is in)", Max: 4096}
	projectArg := []cli.Arg{{Name: "project", Optional: true, Max: 4096}}
	itemArg := cli.Arg{Name: "item", Max: 128}
	return []cli.Command{
		{Path: "project list", Summary: "List the projects Flint knows", Kind: cli.Reads, Run: a.cliList},
		{
			Path: "project open", Summary: "Open a folder that holds flint-project.json as a project", Kind: cli.Acts,
			Args: []cli.Arg{{Name: "dir", Max: 4096}},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				p, err := a.Open(c.Path(c.Arg(0)))
				return infoResult("Opened", p, err)
			},
		},
		{
			Path: "project new", Summary: "Make a folder a project (writes flint-project.json and items/)", Kind: cli.Acts,
			Args:  []cli.Arg{{Name: "dir", Max: 4096}},
			Flags: []cli.Flag{{Name: "name", Value: "<name>", Usage: "the project's name (default: the folder's)", Max: 4 * MaxTitle}},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				p, err := a.Create(c.Path(c.Arg(0)), c.String("name"))
				return infoResult("Created", p, err)
			},
		},
		{
			Path: "project close", Summary: "Forget a project and its items' links to agent tasks (the tasks stay)", Kind: cli.Acts,
			Args: []cli.Arg{{Name: "project", Max: 4096}},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				if err := a.Close(real); err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: "Closed " + real, JSON: map[string]string{"closed": real}}, nil
			},
		},
		{Path: "project show", Summary: "Show a project: repositories, item counts, problems", Kind: cli.Reads, Args: projectArg, Run: a.cliShow},
		{
			Path: "project items", Summary: "List a project's items", Kind: cli.Reads, Args: projectArg,
			Flags: []cli.Flag{{Name: "status", Value: "<status>", Usage: "only items with this status: todo, doing, review or done", Max: 16}},
			Run:   a.cliItems,
		},
		{
			Path: "project add", Summary: "Add an item (status todo)", Kind: cli.Acts,
			Flags: []cli.Flag{
				{Name: "title", Value: "<title>", Usage: fmt.Sprintf("one line, at most %d characters", MaxTitle), Required: true, Max: 4 * MaxTitle},
				{Name: "text", Value: "<text>", Usage: "what to do and how to tell it is done", File: true, Max: MaxDescription},
				{Name: "priority", Value: "<low|normal|high>", Usage: "default: normal", Max: 16},
				{Name: "agent", Value: "<agent>", Usage: "who works on it (default: the default agent)", Max: 200},
				{Name: "repos", Value: "<name,…>", Usage: "the project's repositories it changes", Max: 4096},
				{Name: "after", Value: "<item,…>", Usage: "items that must be done first", Max: 4096},
				{Name: "workflow", Value: "<name>", Usage: "a workflow that takes the item's work", Max: maxWorkflowName},
				{Name: "together", Usage: "one agent in all of its repositories at once"},
				projectFlag,
			},
			Help: "The id comes from the title; a file is never overwritten. Checked like New Item… and add_project_item.",
			Run:  a.cliAdd,
		},
		{
			Path: "project status", Summary: "Set an item's status", Kind: cli.Acts,
			Args: []cli.Arg{itemArg, {Name: "todo|doing|review|done", Max: 16}}, Flags: []cli.Flag{projectFlag},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				v, err := a.SetStatus(real, c.Arg(0), c.Arg(1))
				if err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: fmt.Sprintf("%s is %s", v.ID, v.Status), JSON: v}, nil
			},
		},
		{
			Path: "project delegate", Summary: "Start an item's agent tasks (or its workflow runs)", Kind: cli.Acts, Folder: true,
			Args: []cli.Arg{itemArg}, Flags: []cli.Flag{projectFlag},
			Help: "As Delegate on the board. An item of a project whose folder is not trusted is sent only after you\n" +
				"confirm it in Flint's own dialog; the command waits for your answer. An item without repositories\n" +
				"works in the workspace folder of --folder (default: the current directory).",
			Run: a.cliDelegate,
		},
		{
			Path: "project start-when-ready", Summary: "Start an item by itself once the items it waits for are done", Kind: cli.Acts, Folder: true,
			Args: []cli.Arg{itemArg}, Flags: []cli.Flag{projectFlag},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				v, err := a.StartWhenReady(real, c.Arg(0), a.cliRoot(c.Folder))
				if err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: fmt.Sprintf("%s starts once %s are done", v.ID, strings.Join(v.WaitingFor, ", ")), JSON: v}, nil
			},
		},
		{
			Path: "project cancel-start", Summary: "Forget an item's waiting start", Kind: cli.Acts,
			Args: []cli.Arg{itemArg}, Flags: []cli.Flag{projectFlag},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				if err := a.CancelStart(real, c.Arg(0)); err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: "Cancelled the start of " + c.Arg(0), JSON: map[string]string{"cancelled": c.Arg(0)}}, nil
			},
		},
		{
			Path: "project plan", Summary: "Print the prompt that asks an agent to plan a goal as items", Kind: cli.Reads,
			Flags: []cli.Flag{{Name: "goal", Value: "<text>", Usage: "what the project should achieve", Required: true, File: true, Max: 4 * MaxGoal}, projectFlag},
			Help:  "Paste the prompt into an agent chat in Flint. The agent adds items through Flint's add_project_item tool, which asks you before each item.",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				p, err := a.PlanPrompt(real, c.String("goal"))
				if err != nil {
					return cli.Result{}, err
				}
				res := cli.Result{Text: p.Prompt, JSON: p}
				if !p.ToolOn {
					res.Note = "Note: agents can add items once Flint's MCP server and Settings ▸ Agent Context ▸ Actions ▸ \"Add project items\" are on."
				}
				return res, nil
			},
		},
		{
			Path: "project context", Summary: "Add PROJECT.md, the context every item's agent gets", Kind: cli.Acts, Args: projectArg,
			Help: "Never overwrites the file; refused for a project whose folder is not trusted.",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				v, err := a.AddContext(real)
				if err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: "Wrote " + v.Path, JSON: v}, nil
			},
		},
		{
			Path: "project locate", Summary: "Use a checkout for one of the project's repositories", Kind: cli.Acts,
			Args: []cli.Arg{{Name: "repo", Max: 256}, {Name: "dir", Max: 4096}}, Flags: []cli.Flag{projectFlag},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				v, err := a.Locate(real, c.Arg(0), c.Path(c.Arg(1)))
				return repoResult(v, err)
			},
		},
		{
			Path: "project clone", Summary: "Clone one of the project's repositories into a new folder inside <parent>", Kind: cli.Acts,
			Args: []cli.Arg{{Name: "repo", Max: 256}, {Name: "parent", Max: 4096}}, Flags: []cli.Flag{projectFlag},
			Help: "Flint shows the URL and the folder in its own dialog first; the command waits for your answer and\n" +
				"for the clone (stop it with flint project cancel-clone).",
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				v, err := a.Clone(real, c.Arg(0), c.Path(c.Arg(1)))
				return repoResult(v, err)
			},
		},
		{
			Path: "project cancel-clone", Summary: "Stop the project's running clone", Kind: cli.Acts, Flags: []cli.Flag{projectFlag},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.String("project"))
				if err != nil {
					return cli.Result{}, err
				}
				if err := a.CancelClone(real); err != nil {
					return cli.Result{}, err
				}
				return cli.Result{Text: "Cancelled the clone", JSON: map[string]string{"cancelled": real}}, nil
			},
		},
		{
			Path: "project rename-config", Summary: "Rename an old project's flint-backlog.json to flint-project.json", Kind: cli.Acts, Args: projectArg,
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				real, err := a.cliProject(c, c.Arg(0))
				if err != nil {
					return cli.Result{}, err
				}
				p, err := a.RenameOldConfig(real)
				return infoResult("Renamed the file of", p, err)
			},
		},
	}
}

func infoResult(verb string, p ProjectInfo, err error) (cli.Result, error) {
	if err != nil {
		return cli.Result{}, err
	}
	return cli.Result{Text: fmt.Sprintf("%s the project %q (%s)", verb, p.Name, p.Path), JSON: p}, nil
}

func repoResult(v RepositoryView, err error) (cli.Result, error) {
	if err != nil {
		return cli.Result{}, err
	}
	text := fmt.Sprintf("%s: %s", v.Name, v.Folder)
	if !v.Trusted {
		text += " (Restricted Mode: trust it before Delegate)"
	}
	if !v.InWorkspace {
		text += "\nAdd the folder to the workspace in Flint before Delegate."
	}
	return cli.Result{Text: text, JSON: v}, nil
}

// cliProject finds the project a command names: a folder (absolute or
// relative to the current directory) of a project Flint knows, or the name
// of one; with no name, the known project the current directory is in, or
// the only one there is.
func (a *API) cliProject(c *cli.Call, ref string) (string, error) {
	known := a.knownSet()
	if ref == "" {
		var best string
		for _, f := range known {
			if within(c.Folder, f) && len(f) > len(best) {
				best = f
			}
		}
		switch {
		case best != "":
			return best, nil
		case len(known) == 1:
			return known[0], nil
		}
		return "", errors.New("which project? Give its folder or name (flint project list shows them)")
	}
	if p := c.Path(ref); strings.ContainsRune(ref, filepath.Separator) || ref == "." || ref == ".." {
		real, _, err := a.known(p)
		return real, err
	} else if real, err := Resolve(p); err == nil && slices.Contains(known, real) {
		return real, nil
	}
	var match []string
	for _, f := range known {
		if cfg, err := LoadConfig(f); err == nil && cfg.Name == ref {
			match = append(match, f)
		}
	}
	switch len(match) {
	case 0:
		return "", fmt.Errorf("no open project is named %q (flint project list shows them)", ref)
	case 1:
		return match[0], nil
	}
	return "", fmt.Errorf("several open projects are named %q: give the folder instead", ref)
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	if path == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// cliRoot is the open workspace folder that holds folder ("" when none):
// Delegate's active folder for an item without repositories.
func (a *API) cliRoot(folder string) string {
	best := ""
	for _, r := range a.host.Roots() {
		r = filepath.Clean(r)
		if within(folder, r) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

func (a *API) cliList(context.Context, *cli.Call) (cli.Result, error) {
	list := a.Projects()
	rows := make([][]string, len(list))
	for i, p := range list {
		var notes []string
		if p.Workspace {
			notes = append(notes, "workspace folder")
		}
		if !p.Trusted {
			notes = append(notes, "Restricted Mode")
		}
		if p.OldFormat {
			notes = append(notes, "old format")
		}
		if p.Error != "" {
			notes = append(notes, "cannot be read: "+p.Error)
		}
		rows[i] = []string{p.Name, p.Path, strings.Join(notes, ", ")}
	}
	text := cli.Table([]string{"NAME", "FOLDER", "NOTES"}, rows)
	if len(list) == 0 {
		text = "No projects. Open one with flint project open <dir> or make one with flint project new <dir>.\n"
	}
	return cli.Result{Text: text, JSON: list}, nil
}

func (a *API) cliShow(_ context.Context, c *cli.Call) (cli.Result, error) {
	real, err := a.cliProject(c, c.Arg(0))
	if err != nil {
		return cli.Result{}, err
	}
	p, err := a.Load(real)
	if err != nil {
		return cli.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", p.Name, p.Path)
	if !p.Trusted {
		b.WriteString("The folder is in Restricted Mode: agents can't read it, and Delegate asks first.\n")
	}
	if p.Context.Exists {
		fmt.Fprintf(&b, "Context: %s\n", p.Context.Path)
	} else {
		b.WriteString("Context: none (flint project context adds PROJECT.md)\n")
	}
	counts := map[string]int{}
	for _, it := range p.Items {
		counts[it.Status]++
	}
	var parts []string
	for _, s := range Statuses {
		parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
	}
	fmt.Fprintf(&b, "Items:   %s\n", strings.Join(parts, ", "))
	if len(p.Repositories) > 0 {
		rows := make([][]string, len(p.Repositories))
		for i, r := range p.Repositories {
			folder := r.Folder
			switch {
			case r.Problem != "":
				folder = "problem: " + r.Problem
			case folder == "":
				folder = "no checkout (flint project locate or clone)"
			case !r.Trusted:
				folder += " (Restricted Mode)"
			}
			rows[i] = []string{r.Name, r.URL, folder}
		}
		b.WriteString("\n" + cli.Table([]string{"REPOSITORY", "URL", "CHECKOUT"}, rows))
	}
	if p.Clone != nil {
		fmt.Fprintf(&b, "\nCloning %s into %s: %s\n", p.Clone.URL, p.Clone.Destination, p.Clone.Progress)
	}
	for _, pr := range p.Problems {
		fmt.Fprintf(&b, "problem: %s: %s\n", pr.File, pr.Reason)
	}
	return cli.Result{Text: b.String(), JSON: p}, nil
}

func (a *API) cliItems(_ context.Context, c *cli.Call) (cli.Result, error) {
	status := c.String("status")
	if status != "" && !slices.Contains(Statuses, status) {
		return cli.Result{}, cli.Usagef("unknown status %q: todo, doing, review or done", status)
	}
	real, err := a.cliProject(c, c.Arg(0))
	if err != nil {
		return cli.Result{}, err
	}
	p, err := a.Load(real)
	if err != nil {
		return cli.Result{}, err
	}
	items := []ItemView{}
	for _, it := range p.Items {
		if status == "" || it.Status == status {
			items = append(items, it)
		}
	}
	rows := make([][]string, len(items))
	for i, it := range items {
		var notes []string
		switch {
		case it.DependencyProblem != "":
			notes = append(notes, it.DependencyProblem)
		case len(it.WaitingFor) > 0:
			notes = append(notes, "after "+strings.Join(it.WaitingFor, ", "))
		}
		if it.Waiting != nil {
			notes = append(notes, "starts when ready")
		}
		if it.Workflow != "" {
			notes = append(notes, "workflow "+it.Workflow)
		}
		if it.Failed {
			notes = append(notes, "a task failed")
		}
		rows[i] = []string{it.ID, it.Status, it.Priority, it.Agent, cli.Cut(it.Title, 60), strings.Join(notes, "; ")}
	}
	text := cli.Table([]string{"ITEM", "STATUS", "PRIORITY", "AGENT", "TITLE", "NOTES"}, rows)
	if len(items) == 0 {
		text = "No items.\n"
	}
	return cli.Result{Text: text, JSON: items}, nil
}

func (a *API) cliAdd(_ context.Context, c *cli.Call) (cli.Result, error) {
	real, err := a.cliProject(c, c.String("project"))
	if err != nil {
		return cli.Result{}, err
	}
	spec := ItemSpec{
		Title: c.String("title"), Text: c.String("text"), Priority: c.String("priority"), Agent: c.String("agent"),
		Repos: c.List("repos"), After: c.List("after"), Together: c.Bool("together"), Workflow: c.String("workflow"),
	}
	if len(spec.Repos) > MaxRepositories || len(spec.After) > MaxAfter {
		return cli.Result{}, fmt.Errorf("at most %d repositories and %d items in --after", MaxRepositories, MaxAfter)
	}
	cfg, err := LoadConfig(real)
	if err != nil {
		return cli.Result{}, err
	}
	a.mu.Lock()
	it, err := a.writeNewItem(real, cfg, ItemID(spec.Title), func(id string) ([]byte, error) { return a.checkNewItem(real, cfg, spec, id) })
	a.mu.Unlock()
	if err != nil {
		return cli.Result{}, err
	}
	a.emit(real)
	a.kickStarter()
	v := itemView(real, it, nil, nil)
	return cli.Result{Text: fmt.Sprintf("Added %s (%s) to %q", it.ID, v.Path, cfg.Name), JSON: v}, nil
}

func (a *API) cliDelegate(_ context.Context, c *cli.Call) (cli.Result, error) {
	real, err := a.cliProject(c, c.String("project"))
	if err != nil {
		return cli.Result{}, err
	}
	r, err := a.Delegate(real, c.Arg(0), a.cliRoot(c.Folder))
	if err != nil {
		return cli.Result{}, err
	}
	if r.NotReady {
		return cli.Result{
			Text: fmt.Sprintf("Not started: %s waits for %s.\nStart it once they are done: flint project start-when-ready %s", c.Arg(0), strings.Join(r.WaitingFor, ", "), c.Arg(0)),
			JSON: r, Exit: cli.ExitFailed,
		}, nil
	}
	rows := make([][]string, len(r.Tasks))
	for i, t := range r.Tasks {
		rows[i] = []string{t.ID, t.Repo, t.State, t.Worktree}
	}
	text := fmt.Sprintf("Delegated %s (%s):\n", r.Item.ID, r.Item.Status) + cli.Table([]string{"TASK", "REPOSITORY", "STATE", "WORKTREE"}, rows)
	res := cli.Result{Text: text, JSON: r}
	if r.Warning != "" {
		res.Note = "Warning: " + r.Warning
	}
	return res, nil
}
