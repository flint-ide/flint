package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/prompts"
)

// Project context (docs/design/projects.md, "Project context"): PROJECT.md at
// the top of a project folder says what the project is for, how its
// repositories fit together, its conventions and its definition of done.
// Every agent Delegate starts gets it, with where its item stands in the
// project, at the start of its chat's first message, composed when the chat
// starts (taskContext, through feature.Instance.TaskContext).

const (
	// ContextFile is the project's context file, at the top of its folder.
	ContextFile = "PROJECT.md"
	// MaxContextChars bounds the text of ContextFile that is used
	// (characters); the file itself is bounded by MaxFileSize.
	MaxContextChars = 16000
	// maxContextBlock bounds the whole context a chat's first message starts
	// with (bytes of UTF-8). It is not counted against MaxDescription: that
	// limit stays on the item's own text.
	maxContextBlock = 24 << 10
	// maxInProgress bounds the other items in progress the context lists.
	maxInProgress = 20
	// maxConfirmContext bounds how much of ContextFile the confirmation of a
	// Delegate from an untrusted project shows (characters).
	maxConfirmContext = 1000
	// maxPlanContext bounds how much of ContextFile the prompt of Plan with
	// Agent… includes (characters).
	maxPlanContext = 8000
)

// ProjectContext is a project's ContextFile as read now.
type ProjectContext struct {
	// Exists: the file is there (a regular file, or a link inside the folder).
	Exists bool
	// Text is what agents get ("" when the file is absent, empty or a problem).
	Text string
	// Problem says why the file is not used ("" when it is, or absent).
	Problem string
}

// ReadContext reads folder's ContextFile (folder resolved): resolved inside the
// folder (a symbolic link that leaves it is refused), at most MaxFileSize
// bytes, UTF-8, at most MaxContextChars characters, and no character that
// hides text from the person reading it (the rule of items, hiddenRune). Line
// endings become \n, a leading byte order mark is dropped and the text is
// trimmed. A file that breaks a rule is a problem and its text is not used.
func ReadContext(folder string) ProjectContext {
	path := filepath.Join(folder, ContextFile)
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return ProjectContext{}
	}
	data, err := readInside(folder, ContextFile)
	if errors.Is(err, fs.ErrNotExist) {
		return ProjectContext{Exists: true, Problem: "links to a file that does not exist"}
	}
	if err != nil {
		return ProjectContext{Exists: true, Problem: describe(err)}
	}
	text, err := checkContext(data)
	if err != nil {
		return ProjectContext{Exists: true, Problem: err.Error()}
	}
	return ProjectContext{Exists: true, Text: text}
}

// checkContext checks the content of a context file and returns its text.
func checkContext(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", errors.New("not UTF-8 text")
	}
	text := strings.TrimPrefix(string(data), "\uFEFF")
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if n := utf8.RuneCountInString(text); n > MaxContextChars {
		return "", fmt.Errorf("longer than %d characters (%d): Flint sends at most %d to agents", MaxContextChars, n, MaxContextChars)
	}
	if r, ok := hiddenRune(text); ok {
		return "", fmt.Errorf("contains %s", runeKind(r))
	}
	return text, nil
}

// problem is the problem a context file is listed with.
func (c ProjectContext) problem() (Problem, bool) {
	if c.Problem == "" {
		return Problem{}, false
	}
	return Problem{File: ContextFile, Reason: c.Problem + " (agents don't get the file until it is fixed)"}, true
}

// ContextView is the project's context file, as the panel and the board show it.
type ContextView struct {
	// Path is the file, to open in the editor.
	Path string `json:"path"`
	// Exists: the file is there (Open); else the UI offers Add Project Context….
	Exists bool `json:"exists"`
	// Problem says why agents don't get it ("" when they do or it is absent).
	Problem string `json:"problem"`
}

func contextView(folder string, c ProjectContext) ContextView {
	return ContextView{Path: filepath.Join(folder, ContextFile), Exists: c.Exists, Problem: c.Problem}
}

// contextTemplate is the ContextFile Add Project Context… writes.
func contextTemplate(c Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", c.Name)
	b.WriteString("Flint gives this file to every agent that works on an item of this project, at the start of its first message. " +
		"Keep it short: what an agent needs to know before it starts.\n\n")
	b.WriteString("## What this project is for\n\nThe goal, who it is for, and what is out of scope.\n\n")
	b.WriteString("## Repositories\n\n")
	if len(c.Repositories) == 0 {
		b.WriteString("How the code is laid out, and what lives where.\n\n")
	} else {
		for _, r := range c.Repositories {
			fmt.Fprintf(&b, "- %s: what it holds, and how it talks to the others.\n", r.Name)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Conventions\n\nCoding style, naming, tests, commit messages, what not to touch.\n\n")
	b.WriteString("## Definition of done\n\nWhat must be true before an item is done: tests, docs, review.\n")
	return b.String()
}

// AddContext writes the project's ContextFile from a template with the
// headings What this project is for, Repositories, Conventions and Definition
// of done (Add Project Context…; the UI then opens it). It never overwrites a
// file, and it is refused for a project whose folder is not trusted.
func (a *API) AddContext(folder string) (ContextView, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return ContextView{}, err
	}
	if !a.host.Trusted(real) {
		return ContextView{}, errors.New("the project's folder is in Restricted Mode: trust it first")
	}
	c, err := LoadConfig(real)
	if err != nil {
		return ContextView{}, err
	}
	path := filepath.Join(real, ContextFile)
	a.mu.Lock()
	defer a.mu.Unlock()
	// O_EXCL: never overwrites, and never follows a link that is there.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return ContextView{}, fmt.Errorf("the project has a %s already: open it", ContextFile)
	}
	if err != nil {
		return ContextView{}, err
	}
	_, err = f.WriteString(contextTemplate(c))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return ContextView{}, err
	}
	a.emit(real)
	return contextView(real, ReadContext(real)), nil
}

// ---- the origin of an item's agent tasks

// originPrefix starts the origins this feature gives its agent tasks.
const originPrefix = ID + ":"

// origin is what an agent task of an item is for (feature.NewAgentTask.Origin):
// the project, the item and the repository it works in ("" for the window's
// active folder, and for a joint group's lead it is the lead's repository).
type origin struct {
	Project string `json:"project"`
	Item    string `json:"item"`
	Repo    string `json:"repo,omitempty"`
	// Confirmed is set for a project whose folder was not trusted at
	// Delegate: the hash (contextHash) of the part of the context text the
	// user was shown in the confirmation (shownContext). Such a task's chat
	// gets that part only, and only while the file still starts with it.
	Confirmed string `json:"confirmed,omitempty"`
	// Title and Repos are the item's title and repositories as the
	// confirmation showed them (set with Confirmed): the context of such a
	// task takes them from here, never from the folder as it is later.
	Title string   `json:"title,omitempty"`
	Repos []string `json:"repos,omitempty"`
}

func (o origin) String() string {
	raw, _ := json.Marshal(o)
	return originPrefix + string(raw)
}

// parseOrigin reads an origin of this feature; ok is false for another's or
// a malformed one.
func parseOrigin(s string) (origin, bool) {
	raw, ok := strings.CutPrefix(s, originPrefix)
	if !ok || len(s) > agenttask.MaxOrigin {
		return origin{}, false
	}
	var o origin
	if err := json.Unmarshal([]byte(raw), &o); err != nil || !filepath.IsAbs(o.Project) || !prompts.ValidName(o.Item) {
		return origin{}, false
	}
	return o, true
}

// contextHash identifies the context text a user confirmed.
func contextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ---- what an agent gets

// taskContext is the feature.Instance.TaskContext hook: the project context
// for the chat of an item's agent task that starts now, built from the
// project as it is now. An origin that is not this feature's, or that names a
// project Flint does not know or an item that is not there (any more), gives
// "" and the chat starts with the task's description alone. A project whose
// folder is not trusted now, or was not when the user confirmed the Delegate,
// gets restrictedContext instead.
func (a *API) taskContext(ctx context.Context, st feature.TaskStart) string {
	o, ok := parseOrigin(st.Origin)
	if !ok || !slices.Contains(a.knownSet(), o.Project) {
		return ""
	}
	if o.Confirmed != "" || !a.host.Trusted(o.Project) {
		return restrictedContext(o, st)
	}
	l, err := Load(o.Project)
	if err != nil {
		return ""
	}
	i := slices.IndexFunc(l.Items, func(it Item) bool { return it.ID == o.Item })
	if i < 0 {
		return ""
	}
	it := l.Items[i]
	// The chat starts for this item: it is in doing (Delegate writes that
	// after the chats may have started, and a queued task starts later).
	it.Status = StatusDoing
	if o.Repo != "" {
		if _, ok := l.Config.Repository(o.Repo); !ok {
			o.Repo = ""
		}
	}
	in := contextInput{
		Name: l.Config.Name, Folder: o.Project, Context: l.Context, Item: it,
		Repos: l.Config.Repositories, Repo: o.Repo, Start: st,
	}
	byID := map[string]Item{}
	for _, x := range l.Items {
		byID[x.ID] = x
	}
	a.mu.Lock()
	itemLinks := a.links()[o.Project]
	a.mu.Unlock()
	var ids []string
	for _, dep := range it.After {
		if d, ok := byID[dep]; ok && d.Status == StatusDone {
			for _, ln := range itemLinks[dep] {
				if !ln.Gone {
					ids = append(ids, ln.Task)
				}
			}
		}
	}
	tasks := map[string]feature.AgentTask{}
	if len(ids) > 0 {
		tasks = a.host.AgentTasks(ctx, ids)
	}
	for _, dep := range it.After {
		d, ok := byID[dep]
		info := depInfo{ID: dep, Item: d, Known: ok}
		if ok && d.Status == StatusDone {
			for _, ln := range itemLinks[dep] {
				if ln.abandoned() {
					continue
				}
				dt := depTask{Repo: ln.Repo, PullRequest: ln.PullRequest, Merged: ln.Merged}
				if t, ok := tasks[ln.Task]; ok && !ln.Gone {
					dt.Branch, dt.Merged = t.Branch, t.Merged
					if t.PullRequest != "" {
						dt.PullRequest = t.PullRequest
					}
				}
				if dt.Branch == "" && dt.PullRequest == "" {
					continue
				}
				info.Tasks = append(info.Tasks, dt)
			}
		}
		in.Deps = append(in.Deps, info)
	}
	for _, x := range l.Items {
		if x.ID != it.ID && (x.Status == StatusDoing || x.Status == StatusReview) {
			in.InProgress = append(in.InProgress, x)
		}
	}
	return renderContext(in)
}

// restrictedContext is the context of a task of a project whose folder is
// not trusted (now, or when the user confirmed the Delegate): only what the
// confirmation showed reaches the agent (docs/design/projects.md, "Trust and
// Restricted Mode"). That is the beginning of PROJECT.md exactly as shown
// (shownContext, sent only while the file still starts with it), and the
// item's id, title and repositories as the origin keeps them from the
// Delegate. Nothing else is read from the folder: no project name, no other
// item, no URL, no dependency. A task without a confirmed origin (the project
// was trusted at Delegate) gets no PROJECT.md and no title.
func restrictedContext(o origin, st feature.TaskStart) string {
	in := contextInput{
		Folder: o.Project, Item: Item{ID: o.Item, Title: o.Title, Status: StatusDoing, Repos: []string{}}, Start: st, Restricted: true,
	}
	for _, r := range o.Repos {
		if repoNamePattern.MatchString(r) && len(r) <= maxRepoName {
			in.Item.Repos = append(in.Item.Repos, r)
			in.Repos = append(in.Repos, RepositoryDecl{Name: r})
		}
	}
	if o.Repo != "" && !slices.Contains(in.Item.Repos, o.Repo) {
		if len(in.Item.Repos) > 0 || !repoNamePattern.MatchString(o.Repo) || len(o.Repo) > maxRepoName {
			o.Repo = ""
		} else {
			in.Item.Repos = []string{o.Repo}
			in.Repos = []RepositoryDecl{{Name: o.Repo}}
		}
	}
	in.Repo = o.Repo
	pc := ReadContext(o.Project)
	shown, cut := shownContext(pc.Text)
	switch {
	case o.Confirmed != "" && pc.Text != "" && contextHash(shown) == o.Confirmed:
		in.Context = ProjectContext{Exists: true, Text: shown}
		if cut {
			in.ContextNote = fmt.Sprintf("(The rest of %s was left out: the project's folder is not trusted, and only this beginning was shown when the Delegate was confirmed.)", ContextFile)
		}
	case pc.Exists:
		in.Context = ProjectContext{Exists: true, Problem: "the project's folder is not trusted, or was not when the Delegate was confirmed, and the file was not shown then or changed since"}
	}
	return renderContext(in)
}

// shownContext is the part of a context text the confirmation of a Delegate
// from an untrusted project shows (maxConfirmContext characters), and
// whether the text is longer. Exactly this part is what such a project's
// agents may get.
func shownContext(text string) (string, bool) {
	if utf8.RuneCountInString(text) <= maxConfirmContext {
		return text, false
	}
	return string([]rune(text)[:maxConfirmContext]), true
}

// contextInput is what renderContext writes.
type contextInput struct {
	Name, Folder string
	Context      ProjectContext
	Item         Item
	// Deps are the items of Item.After, in its order.
	Deps []depInfo
	// InProgress are the project's other items in doing or review.
	InProgress []Item
	Repos      []RepositoryDecl
	// Repo is the repository this task works in ("" for the active folder,
	// or for a joint group: the lead's).
	Repo  string
	Start feature.TaskStart
	// Restricted: the project's folder is not trusted; only what the user
	// confirmed is written (restrictedContext: no project name, no other
	// item, no URL, no dependency).
	Restricted bool
	// ContextNote follows the context text (what of it was left out).
	ContextNote string
}

type depInfo struct {
	ID    string
	Item  Item
	Known bool // the item is there (not a problem)
	Tasks []depTask
}

// depTask is a delivered or known agent task of an item that is done.
type depTask struct {
	Repo, Branch, PullRequest string
	Merged                    bool
}

// The lines that open and close the context, around the task's description.
const (
	contextStart = "===== Project context (from Flint) ====="
	contextEnd   = "===== Your task ====="
)

// cuts says what renderContext leaves out to stay within maxContextBlock,
// least important first.
type cuts struct {
	inProgress  int  // in-progress items listed (at most maxInProgress)
	depDetails  bool // branches and pull requests of the dependencies
	otherRepos  bool // repositories the item does not name
	contextText int  // characters of the context text (-1: all)
}

// renderContext writes the context block, within maxContextBlock: when it is
// longer it leaves out, in this order, the items in progress, the branches
// and pull requests of the dependencies, the repositories the item does not
// name, and then the end of the context text, and says what it left out.
func renderContext(in contextInput) string {
	c := cuts{inProgress: min(len(in.InProgress), maxInProgress), depDetails: true, otherRepos: true, contextText: -1}
	out := writeContext(in, c)
	for len(out) > maxContextBlock && c.inProgress > 0 {
		c.inProgress--
		out = writeContext(in, c)
	}
	if len(out) > maxContextBlock && c.depDetails {
		c.depDetails = false
		out = writeContext(in, c)
	}
	if len(out) > maxContextBlock && c.otherRepos {
		c.otherRepos = false
		out = writeContext(in, c)
	}
	for i := 0; len(out) > maxContextBlock && in.Context.Text != "" && i < 4; i++ {
		// Cut the text by what is over, plus room for the note.
		cur := in.Context.Text
		if c.contextText >= 0 {
			cur = string([]rune(cur)[:c.contextText])
		}
		c.contextText = utf8.RuneCountInString(cutBytes(cur, len(cur)-(len(out)-maxContextBlock)-300))
		out = writeContext(in, c)
	}
	if len(out) > maxContextBlock { // never: the other parts are bounded
		out = cutBytes(out, maxContextBlock-len(contextEnd)-40) + "\n[… cut]\n\n" + contextEnd
	}
	return out
}

// cutBytes cuts s to at most n bytes, on a character boundary.
func cutBytes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n < 0 {
		n = 0
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func agentName(agent string) string {
	if agent == "" {
		return "the default agent"
	}
	return agent
}

// writeContext writes the block between its delimiters. Free text in it (the
// context file above all) can't close or reopen it: fenceSafe quotes every
// line that looks like a delimiter.
func writeContext(in contextInput, c cuts) string {
	limit := fmt.Sprintf("Flint sends at most %d KB of project context", maxContextBlock>>10)
	var b strings.Builder
	if in.Restricted {
		fmt.Fprintf(&b, "You are one of the agents working on an item of a Flint project (a backlog of work items, in %s). "+
			"Flint passes on only what the user confirmed for this start; your task follows after it.\n", in.Folder)
	} else {
		fmt.Fprintf(&b, "You are one of the agents working on the Flint project %q (a backlog of work items, in %s). "+
			"Flint wrote this part of the message from the project's files as they are now; your task follows after it.\n", in.Name, in.Folder)
	}

	switch {
	case in.Context.Text != "":
		text := in.Context.Text
		note := ""
		if c.contextText >= 0 && c.contextText < utf8.RuneCountInString(text) {
			text = strings.TrimRight(string([]rune(text)[:c.contextText]), " \t\n")
			note = fmt.Sprintf("\n\n[… %s is cut here: %s; read the whole file at %s]", ContextFile, limit, filepath.Join(in.Folder, ContextFile))
		}
		if in.ContextNote != "" {
			note += "\n\n" + in.ContextNote
		}
		fmt.Fprintf(&b, "\n## About the project (%s)\n\n%s%s\n", ContextFile, text, note)
	case in.Context.Problem != "":
		fmt.Fprintf(&b, "\n## About the project (%s)\n\n(The project's %s was left out: %s.)\n", ContextFile, ContextFile, in.Context.Problem)
	}

	it := in.Item
	if in.Restricted {
		title := ""
		if it.Title != "" {
			title = ": " + it.Title
		}
		fmt.Fprintf(&b, "\n## Your item\n\n%s%s\nstatus: %s; file: %s\n", it.ID, title, it.Status, itemPath(it.ID))
		b.WriteString("\nThe project's other items are left out: its folder is not trusted in Flint, or was not when the user confirmed this start.\n")
	} else {
		fmt.Fprintf(&b, "\n## Your item\n\n%s: %s\nstatus: %s; priority: %s; file: %s\n", it.ID, it.Title, it.Status, it.Priority, itemPath(it.ID))
		if len(in.Deps) > 0 {
			b.WriteString("\n## What your item depends on\n\n")
			for _, d := range in.Deps {
				if !d.Known {
					fmt.Fprintf(&b, "- %s: (no such item can be read now)\n", d.ID)
					continue
				}
				fmt.Fprintf(&b, "- %s: %s (%s)\n", d.ID, d.Item.Title, d.Item.Status)
				if d.Item.Status != StatusDone || !c.depDetails {
					continue
				}
				if len(d.Tasks) == 0 {
					b.WriteString("  no branch or pull request of it is known on this computer\n")
				}
				for _, t := range d.Tasks {
					where := t.Repo
					if where == "" {
						where = "the active folder"
					}
					var parts []string
					if t.Branch != "" {
						parts = append(parts, "branch "+t.Branch)
					}
					if t.PullRequest != "" {
						parts = append(parts, "pull request "+t.PullRequest)
					}
					if t.Merged {
						parts = append(parts, "merged")
					}
					fmt.Fprintf(&b, "  - %s: %s\n", where, strings.Join(parts, ", "))
				}
			}
			if !c.depDetails {
				fmt.Fprintf(&b, "(Their branches and pull requests are left out: %s.)\n", limit)
			}
		}
		if len(in.InProgress) > 0 {
			b.WriteString("\n## In progress in this project\n\n")
			for _, x := range in.InProgress[:c.inProgress] {
				repos := "the active folder"
				if len(x.Repos) > 0 {
					repos = strings.Join(x.Repos, ", ")
				}
				fmt.Fprintf(&b, "- %s: %s (%s; agent: %s; repositories: %s)\n", x.ID, x.Title, x.Status, agentName(x.Agent), repos)
			}
			if more := len(in.InProgress) - c.inProgress; more > 0 {
				why := fmt.Sprintf("Flint lists at most %d", maxInProgress)
				if c.inProgress < min(len(in.InProgress), maxInProgress) {
					why = limit
				}
				fmt.Fprintf(&b, "(%d more %s not listed: %s.)\n", more, plural(more, "item", "items"), why)
			}
		}
	}

	st := in.Start
	b.WriteString("\n## Repositories and where you work\n\n")
	others := 0
	for _, r := range in.Repos {
		mine := slices.Contains(it.Repos, r.Name)
		if !mine && !c.otherRepos {
			others++
			continue
		}
		line := "- " + r.Name
		if !in.Restricted && r.URL != "" {
			line += ": " + r.URL
		}
		switch {
		case st.Joint && mine:
			line += " (you work in it)"
		case r.Name == in.Repo && in.Repo != "":
			line += " (you work in it)"
		case mine:
			line += " (another agent works on your item in it)"
		}
		b.WriteString(line + "\n")
	}
	if others > 0 {
		fmt.Fprintf(&b, "(%d more %s of the project not listed: %s.)\n", others, plural(others, "repository", "repositories"), limit)
	}
	if len(in.Repos) > 0 {
		b.WriteString("\n")
	}
	switch {
	case st.Joint && len(it.Repos) == 0:
		b.WriteString("You work in several repositories at once; after your task Flint lists which folder is which.\n")
	case st.Joint:
		fmt.Fprintf(&b, "You work in the repositories %s at once; after your task Flint lists which folder is which.\n", strings.Join(it.Repos, ", "))
	case in.Repo != "":
		fmt.Fprintf(&b, "You work in %s only, in %s on the branch %s (from %s).\n", in.Repo, st.Worktree, st.Branch, st.Base)
		if len(it.Repos) > 1 {
			b.WriteString("Other agents work on the item's other repositories at the same time, each on its own branch.\n")
		}
	default:
		fmt.Fprintf(&b, "Your item names no repository: you work in %s on the branch %s (from %s), a worktree of the folder that was active in Flint's window.\n", st.Worktree, st.Branch, st.Base)
	}

	if st.MCP && !in.Restricted {
		fmt.Fprintf(&b, "\n## Reading more\n\nFlint's %s tool returns this project's whole backlog, your item first.\n", ToolName)
	}
	return contextStart + "\n" + fenceSafe(b.String()) + "\n" + contextEnd
}

// fenceSafe keeps text inside the block from closing or reopening it: a line
// that starts (after spaces) with "=====", as both delimiters do, gets "> " in
// front of it, so no line of the text equals contextStart or contextEnd and
// none looks like them. Applied to the whole inside of the block, so it
// covers the context file and every title, name and reason in it.
func fenceSafe(s string) string {
	if !strings.Contains(s, "=====") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "=====") {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

// confirmContext is what the confirmation of a Delegate from an untrusted
// project says about its context file: that it is sent too, and its
// beginning (shownContext: maxConfirmContext characters, saying when it is
// cut, and that only this beginning is sent then).
func confirmContext(c ProjectContext) string {
	switch {
	case c.Text != "":
		text, cut := shownContext(c.Text)
		if cut {
			text += fmt.Sprintf("\n[… cut here: the file has %d characters; only this beginning is sent, because the project's folder is not trusted]", utf8.RuneCountInString(c.Text))
		}
		return fmt.Sprintf("\n\nThe project's context file %s is sent too, before the description:\n%s", ContextFile, text)
	case c.Problem != "":
		return fmt.Sprintf("\n\nThe project's %s is not sent (%s).", ContextFile, c.Problem)
	}
	return ""
}
