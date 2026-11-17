// Package agentdefs reads agent definitions: Markdown files that make a named
// agent out of an engine (Claude Code or Codex), an optional model and
// standing instructions (docs/design/agent-definitions.md). The file format is
// the prompt library's (prompts.FrontMatter).
//
// A definition only chooses an engine, a model and text: it cannot add
// arguments, tools, permissions or environment to the agent it describes.
package agentdefs

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/agent"
	"flint/internal/prompts"
)

// Source says where a definition comes from.
type Source string

const (
	SourcePersonal Source = "personal"
	SourceProject  Source = "project"
)

// Definition is one defined agent.
type Definition struct {
	// Name is the agent name (the file name without .md).
	Name string `json:"name"`
	// Description is the one line pickers show; may be empty.
	Description string `json:"description"`
	// Engine is the pinned engine ("claude" or "codex"); "" runs on the
	// user's default engine (ResolveEngine).
	Engine string `json:"engine"`
	// Model replaces the engine's model from settings; only with Engine.
	Model string `json:"model"`
	// Instructions are the standing instructions (the file's body).
	Instructions string `json:"instructions"`
	Source       Source `json:"source"`
	// Path is the Markdown file.
	Path string `json:"path"`
}

// Limits, fixed because they protect Flint from a hostile repository.
const (
	MaxFileSize     = prompts.MaxFileSize
	MaxDefinitions  = 50
	MaxInstructions = 16000 // characters
	maxName         = 32
	maxDescription  = 200
)

// namePattern is the rule for a custom agent's id: the name is used as an
// agent name and in task branch names.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ValidName reports whether name can name a defined agent: lowercase letters,
// digits and dashes, starting with a letter, at most 32 characters.
func ValidName(name string) bool {
	return len(name) <= maxName && namePattern.MatchString(name)
}

// Parse reads a definition file. Keys: description, engine, model. Unknown
// keys are ignored, so newer files still load.
func Parse(name string, data []byte) (Definition, error) {
	if !ValidName(name) {
		return Definition{}, fmt.Errorf("name %q: use lowercase letters, digits and dashes, starting with a letter (at most %d)", name, maxName)
	}
	fields, body, err := prompts.FrontMatter(data)
	if err != nil {
		return Definition{}, err
	}
	d := Definition{Name: name}
	for _, f := range fields {
		switch f.Key {
		case "description":
			if utf8.RuneCountInString(f.Value) > maxDescription {
				return Definition{}, fmt.Errorf("description longer than %d characters", maxDescription)
			}
			d.Description = f.Value
		case "engine":
			if f.Value != "" && !agent.Valid(agent.Kind(f.Value)) {
				return Definition{}, fmt.Errorf("engine %q: use %s", f.Value, engineList())
			}
			d.Engine = f.Value
		case "model":
			if err := agent.CheckModel(f.Value); err != nil {
				return Definition{}, err
			}
			d.Model = f.Value
		}
	}
	if d.Model != "" && d.Engine == "" {
		return Definition{}, errors.New("a model needs an engine: model names belong to one engine")
	}
	d.Instructions = strings.TrimSpace(body)
	switch {
	case d.Instructions == "":
		return Definition{}, errors.New("the instructions (the text after the front matter) are empty")
	case utf8.RuneCountInString(d.Instructions) > MaxInstructions:
		return Definition{}, fmt.Errorf("the instructions are longer than %d characters", MaxInstructions)
	}
	return d, nil
}

// Engines are the engines a definition may name, in preference order.
func Engines() []agent.Kind { return []agent.Kind{agent.Claude, agent.Codex} }

func engineList() string {
	var names []string
	for _, k := range Engines() {
		names = append(names, string(k))
	}
	return strings.Join(names, " or ")
}

// ErrNoEngine is returned when no engine is installed for a definition.
var ErrNoEngine = errors.New("no agent CLI installed: install Claude Code (claude) or Codex CLI (codex)")

// ResolveEngine picks the engine a conversation of d starts on. A pinned
// engine is used when installed. Otherwise it is defaultAgent when that is a
// native agent that is installed, else the first native agent installed:
// defaultAgent may name a defined (or custom) agent, which is never followed,
// so a definition can't resolve through another one. installed lists the
// native agents found, in preference order.
func ResolveEngine(d Definition, defaultAgent string, installed []agent.Kind) (agent.Kind, error) {
	if d.Engine != "" {
		k := agent.Kind(d.Engine)
		if !slices.Contains(installed, k) {
			return "", fmt.Errorf("%s runs on %s, which is not installed", d.Name, d.Engine)
		}
		return k, nil
	}
	if k := agent.Kind(defaultAgent); agent.Valid(k) && slices.Contains(installed, k) {
		return k, nil
	}
	if len(installed) == 0 {
		return "", ErrNoEngine
	}
	return installed[0], nil
}

// Same reports whether two definitions run the same way (the description and
// path do not matter to a running agent).
func (d Definition) Same(o Definition) bool {
	return d.Name == o.Name && d.Engine == o.Engine && d.Model == o.Model && d.Instructions == o.Instructions
}
