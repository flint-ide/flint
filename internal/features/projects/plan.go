package projects

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"flint/internal/agenttask"
)

// Bounds of Plan with Agent….
const (
	// MaxGoal bounds the goal the user states (characters).
	MaxGoal = 2000
	// maxPromptAgents bounds the agents the prompt lists.
	maxPromptAgents = 30
)

// PlanPrompt is the prompt of Plan with Agent… for the chat's draft.
type PlanPrompt struct {
	// Prompt is the text to put into the chat's draft (the user sends it).
	Prompt string `json:"prompt"`
	// ToolOn: agents can use add_project_item now (Flint's MCP server is on
	// and Settings ▸ Agent Context ▸ Actions ▸ "Add project items" is switched on). When
	// false the UI says what to switch on; the prompt is still usable.
	ToolOn bool `json:"toolOn"`
}

// PlanPrompt builds the prompt that asks an agent to split goal into items
// of the project: the goal, the project's name, folder and repositories, the
// item format with its limits, the agents available here, and how to use
// get_project and add_project_item. The goal is one to MaxGoal characters
// without control characters other than tab and newline or invisible ones.
// The project's folder must be trusted: agents can neither read nor add to
// an untrusted project.
func (a *API) PlanPrompt(folder, goal string) (PlanPrompt, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return PlanPrompt{}, err
	}
	goal = strings.TrimSpace(strings.ReplaceAll(goal, "\r\n", "\n"))
	switch {
	case goal == "":
		return PlanPrompt{}, errors.New("say what the goal is")
	case utf8.RuneCountInString(goal) > MaxGoal:
		return PlanPrompt{}, fmt.Errorf("the goal is longer than %d characters", MaxGoal)
	}
	if r, ok := hiddenRune(goal); ok {
		return PlanPrompt{}, fmt.Errorf("the goal contains %s", runeKind(r))
	}
	if !a.host.Trusted(real) {
		return PlanPrompt{}, errors.New("the project's folder is in Restricted Mode: trust it first, so agents can read the project and add items to it")
	}
	c, err := LoadConfig(real)
	if err != nil {
		return PlanPrompt{}, err
	}
	mcp := a.host.Settings().MCPServer
	return PlanPrompt{Prompt: planText(c, real, goal, a.host.Agents().Available, a.host.Agents().Default, ReadContext(real)), ToolOn: mcp.Enabled && mcp.Actions.Projects}, nil
}

// planContext is the project's context file for the planning prompt: its
// text, cut at maxPlanContext characters (saying so), or why it is left out.
func planContext(folder string, pc ProjectContext) string {
	switch {
	case pc.Text != "":
		text := pc.Text
		if n := utf8.RuneCountInString(text); n > maxPlanContext {
			text = string([]rune(text)[:maxPlanContext]) + fmt.Sprintf("\n[… cut here: the file has %d characters; read the rest in %s]", n, filepath.Join(folder, ContextFile))
		}
		return fmt.Sprintf("What the project is for, how its repositories fit together, its conventions and its definition of done (its %s; every agent that works on an item gets it too):\n\n%s\n\n"+
			"Split the goal in the project's terms: items that fit its repositories, follow its conventions and can meet its definition of done.\n\n", ContextFile, text)
	case pc.Problem != "":
		return fmt.Sprintf("(The project's %s is left out: %s.)\n\n", ContextFile, pc.Problem)
	}
	return ""
}

// planText is the prompt of Plan with Agent….
func planText(c Config, folder, goal string, agents []string, def string, pc ProjectContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan this goal as work items of the Flint project %q (folder %s):\n\n%s\n\n", c.Name, folder, goal)
	b.WriteString(planContext(folder, pc))
	fmt.Fprintf(&b, "First read the project's backlog with Flint's %s tool (project: %q) and don't add what is planned there already. "+
		"Then split the goal into items that one agent can finish and that can be checked, and add each with Flint's %s tool, "+
		"in an order where an item only waits for items that exist already. Flint asks me before each item is written.\n\n", ToolName, c.Name, AddToolName)
	b.WriteString("Each item:\n")
	fmt.Fprintf(&b, "- project: %q\n", folder)
	fmt.Fprintf(&b, "- title: one line, at most %d characters; the item's id (its file name) is made from it\n", MaxTitle)
	fmt.Fprintf(&b, "- text: what to do and how to tell it is done (\"Done when:\" and a list); title and text together are at most %d bytes\n", MaxDescription)
	b.WriteString("- priority: low, normal or high (default normal)\n")
	if len(agents) > 0 {
		list := agents
		more := ""
		if len(list) > maxPromptAgents {
			list, more = list[:maxPromptAgents], ", …"
		}
		fmt.Fprintf(&b, "- agent (optional): the agent that works on it, one of %s%s; default: %s\n", strings.Join(list, ", "), more, def)
	}
	if len(c.Repositories) > 0 {
		b.WriteString("- repos: the repositories it changes, by name (an item with several gets one agent task per repository, unless together is true):\n")
		for _, r := range c.Repositories {
			fmt.Fprintf(&b, "  - %s: %s\n", r.Name, r.URL)
		}
		fmt.Fprintf(&b, "- together (optional, only with 2 to %d repos): true when one agent should work in all of them at once, in one chat "+
			"(a change that must stay consistent across them); it needs an agent that can work in several folders (Claude Code, not Codex); default false\n", agenttask.MaxJoint)
	} else {
		b.WriteString("- repos: leave it out (this project lists no repositories; its items run in the folder active in Flint's window)\n")
	}
	fmt.Fprintf(&b, "- after (optional): ids of items of this project that must be done first, at most %d, no cycles\n\n", MaxAfter)
	b.WriteString("Title and text must not contain control characters other than tab and newline, or invisible characters. " +
		"New items start as todo. You can't change, delete, start or set the status of items. " +
		"If a call is refused, correct the item as the message says and try again. " +
		"If the " + AddToolName + " tool is missing, tell me to switch on \"Add project items\" in Flint's Settings ▸ Agent Context ▸ Actions. " +
		"When you are done, list the items you added with their ids.")
	return b.String()
}
