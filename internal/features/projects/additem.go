package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/agenttask"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// AddToolName is the acting agent tool that writes a new project item.
const AddToolName = mcptools.ToolProjectAdd

// Bounds of add_project_item.
const (
	// maxAddsPerChat bounds the items one chat adds (MaxItems bounds the
	// project).
	maxAddsPerChat = 50
	// maxAddCounters bounds the chats counted (then the counts start again).
	maxAddCounters = 1000
	// maxSummaryText is how much of the text the confirmation shows.
	maxSummaryText = 600
	// maxWorkflowName bounds a workflow's name in a call (prompts' names).
	maxWorkflowName = 64
)

// addTool is add_project_item, offered while the user has switched it on
// (settings.json mcpServer.actions.projects). It acts: Flint asks the user
// before each call, showing what will be written.
func (a *API) addTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:  AddToolName,
		Title: "Add a project item",
		Description: "Adds one work item to the backlog of a Flint project the user has open (status todo). Use it to plan a goal as items, after reading the project with get_project. " +
			"The item's id (its file name) is made from the title; an existing item is never changed. " +
			"The user is asked before each item is written. Items of a project whose folder is in Restricted Mode in Flint can't be added. " +
			"A call that would make an item Flint can't read is refused with the reason: correct it and try again.",
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
			`"project":{"type":"string","description":"The project: its folder or its name, as get_project shows them."},`+
			`"title":{"type":"string","description":"One line, at most %d characters."},`+
			`"text":{"type":"string","description":"What to do and how to tell it is done. Title and text together are at most %d bytes."},`+
			`"priority":{"type":"string","enum":["low","normal","high"],"description":"Default: normal."},`+
			`"agent":{"type":"string","description":"The agent that works on it (default: the user's default agent)."},`+
			`"repos":{"type":"array","items":{"type":"string"},"maxItems":%d,"description":"Names of the project's repositories it changes (default: the folder active in Flint's window)."},`+
			`"after":{"type":"array","items":{"type":"string"},"maxItems":%d,"description":"Ids of existing items of this project that must be done first."},`+
			`"together":{"type":"boolean","description":"Only with 2 to %d repos: true makes one agent work in all of them at once, in one chat, instead of one agent per repository (only agents that can work in several folders, such as Claude Code). Default: false."},`+
			`"workflow":{"type":"string","maxLength":%d,"description":"The name of a Flint workflow that takes the item's work step by step (plan, implement, test, review, the user's approval) instead of one agent. Not with together. Default: none."}`+
			`},"required":["project","title","text"],"additionalProperties":false}`, MaxTitle, MaxDescription, MaxRepositories, MaxAfter, agenttask.MaxJoint, maxWorkflowName)),
		Acts: true,
		Plan: a.planAdd,
	}
}

type addArgs struct {
	Project  string   `json:"project"`
	Title    string   `json:"title"`
	Text     *string  `json:"text"`
	Priority string   `json:"priority"`
	Agent    string   `json:"agent"`
	Repos    []string `json:"repos"`
	After    []string `json:"after"`
	Together bool     `json:"together"`
	Workflow string   `json:"workflow"`
}

// planAdd checks a call of add_project_item and says what it will write; the
// write checks everything again (the user may take a while to answer).
func (a *API) planAdd(ctx context.Context, root string, raw json.RawMessage) (mcpserver.Planned, error) {
	var in addArgs
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return mcpserver.Planned{}, fmt.Errorf("invalid arguments: %v", err)
	}
	if dec.More() {
		return mcpserver.Planned{}, errors.New("invalid arguments: more than one object")
	}
	if in.Text == nil {
		return mcpserver.Planned{}, errors.New("text is required (it may be empty)")
	}
	if len(in.Repos) > MaxRepositories || len(in.After) > MaxAfter || len(*in.Text) > MaxDescription || len(in.Title) > 4*MaxTitle || len(in.Workflow) > maxWorkflowName {
		return mcpserver.Planned{}, fmt.Errorf("too long: at most %d repositories, %d items in after, %d bytes of text", MaxRepositories, MaxAfter, MaxDescription)
	}
	real, c, err := a.toolProject(in.Project)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	spec := ItemSpec{Title: in.Title, Text: *in.Text, Priority: in.Priority, Agent: in.Agent, Repos: in.Repos, After: in.After, Together: in.Together, Workflow: in.Workflow}
	caller := mcpserver.CallerFrom(ctx)
	if caller == "" {
		caller = "folder:" + root
	}
	base := ItemID(in.Title)
	a.mu.Lock()
	err = a.chatRoom(caller)
	if err == nil {
		_, err = a.checkNewItem(real, c, spec, base)
	}
	a.mu.Unlock()
	if err != nil {
		return mcpserver.Planned{}, err
	}
	return mcpserver.Planned{
		Summary: addSummary(c.Name, real, base, spec),
		Run: func(context.Context) (string, error) {
			return a.addItem(real, in.Project, caller, base, spec)
		},
	}, nil
}

// addItem writes the item after the user's yes, checking everything again.
func (a *API) addItem(real, ref, caller, base string, spec ItemSpec) (string, error) {
	again, c, err := a.toolProject(ref)
	if err != nil {
		return "", err
	}
	if again != real {
		return "", errors.New("the project changed while the user was asked: try again")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.chatRoom(caller); err != nil {
		return "", err
	}
	it, err := a.writeNewItem(real, c, base, func(id string) ([]byte, error) { return a.checkNewItem(real, c, spec, id) })
	if err != nil {
		return "", err
	}
	if len(a.added) >= maxAddCounters && a.added[caller] == 0 {
		clear(a.added)
	}
	a.added[caller]++
	a.emit(real)
	msg := fmt.Sprintf("Added the item %s (%s) to the project %q with status todo.", it.ID, itemPath(it.ID), c.Name)
	if len(it.After) > 0 {
		msg += " It waits for " + strings.Join(it.After, ", ") + "."
	}
	return msg, nil
}

// chatRoom refuses a chat that added maxAddsPerChat items. Callers hold mu.
func (a *API) chatRoom(caller string) error {
	if a.added[caller] >= maxAddsPerChat {
		return fmt.Errorf("this chat added %d items to projects, the most one chat may: the user can add more in Flint", maxAddsPerChat)
	}
	return nil
}

// checkNewItem makes the file of spec as item id and checks it against the
// project as it is: the parser's rules, an agent that is available, and
// after naming existing items without a cycle. Callers hold mu.
func (a *API) checkNewItem(real string, c Config, spec ItemSpec, id string) ([]byte, error) {
	if spec.Agent != "" {
		if p := agentProblem(spec.Agent, a.host.Agents()); p != "" {
			return nil, errors.New(p)
		}
	}
	data, err := BuildItemFile(spec, id, c)
	if err != nil {
		return nil, err
	}
	it, err := ParseItem(id, data, c)
	if err != nil {
		return nil, err
	}
	if len(it.After) == 0 {
		return data, nil
	}
	l, err := Load(real)
	if err != nil {
		return nil, err
	}
	known := slices.DeleteFunc(slices.Clone(l.Items), func(x Item) bool { return x.ID == id })
	for _, dep := range it.After {
		if !slices.ContainsFunc(known, func(x Item) bool { return x.ID == dep }) {
			return nil, fmt.Errorf("after: the project has no item %q (use the ids get_project shows)", dep)
		}
	}
	if d := Dependencies(append(known, it))[id]; d.Problem != "" {
		return nil, errors.New(d.Problem)
	}
	return data, nil
}

// toolProject finds the project an agent named: a known project's folder
// (as get_project shows it) or its name, when one known project with a
// trusted folder has it. Its folder must be trusted.
func (a *API) toolProject(ref string) (string, Config, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > 4096 {
		return "", Config{}, errors.New("project is required: its folder or its name, as get_project shows them")
	}
	known := a.knownSet()
	var match []string
	if filepath.IsAbs(ref) {
		if real, err := Resolve(ref); err == nil && slices.Contains(known, real) {
			match = append(match, real)
		}
	}
	if len(match) == 0 {
		for _, f := range known {
			// By name only the trusted ones, which get_project shows.
			if c, err := LoadConfig(f); err == nil && c.Name == ref && a.host.Trusted(f) {
				match = append(match, f)
			}
		}
	}
	switch len(match) {
	case 0:
		return "", Config{}, fmt.Errorf("no open project is %q: call get_project for the open ones", ref)
	case 1:
	default:
		return "", Config{}, fmt.Errorf("several open projects are named %q: pass the folder instead", ref)
	}
	if !a.host.Trusted(match[0]) {
		return "", Config{}, errors.New("the project's folder is in Restricted Mode in Flint: agents can't add items to it until the user trusts the folder")
	}
	c, err := LoadConfig(match[0])
	if err != nil {
		return "", Config{}, err
	}
	if c.OldFormat {
		return "", Config{}, fmt.Errorf("the project is in the old format (%s) and read-only in Flint until the user renames the file to %s in the Projects panel", OldConfigFile, ConfigFile)
	}
	return match[0], c, nil
}

// addSummary is what the user is asked about.
func addSummary(name, folder, base string, spec ItemSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Add an item (status todo) to the project %q (%s):\n", name, folder)
	fmt.Fprintf(&b, "Title: %s\n", strings.TrimSpace(spec.Title))
	prio := spec.Priority
	if prio == "" {
		prio = PriorityNormal
	}
	fmt.Fprintf(&b, "Priority: %s", prio)
	if spec.Agent != "" {
		fmt.Fprintf(&b, " · Agent: %s", spec.Agent)
	}
	if len(spec.Repos) > 0 {
		fmt.Fprintf(&b, " · Repositories: %s", strings.Join(spec.Repos, ", "))
	}
	if spec.Together {
		b.WriteString(" · Together: one agent in all of its repositories")
	}
	if len(spec.After) > 0 {
		fmt.Fprintf(&b, " · After: %s", strings.Join(spec.After, ", "))
	}
	if spec.Workflow != "" {
		fmt.Fprintf(&b, " · Workflow: %s", spec.Workflow)
	}
	fmt.Fprintf(&b, "\nFile: %s (or the next free name)\n", itemPath(base))
	if text := strings.TrimSpace(spec.Text); text != "" {
		if utf8.RuneCountInString(text) > maxSummaryText {
			text = string([]rune(text)[:maxSummaryText]) + " […]"
		}
		b.WriteString("\n" + text)
	}
	return b.String()
}
