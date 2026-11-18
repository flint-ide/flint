package main

import (
	"errors"
	"fmt"

	"flint/internal/agentdefs"
)

// DefinedAgent is an agent definition as Settings and the agent pickers
// show it. The instructions are not sent: a definition is edited as a file.
type DefinedAgent struct {
	// Name is the agent name, used wherever an agent name is.
	Name        string `json:"name"`
	Description string `json:"description"`
	// Engine is the pinned engine ("claude" or "codex"); "" runs on the
	// default engine.
	Engine string `json:"engine"`
	// ResolvedEngine is the engine a new conversation would run on now; ""
	// when none is installed (the agent is then not listed in pickers).
	ResolvedEngine string `json:"resolvedEngine"`
	// Model is the model as the file names it: a tier ("fast", "balanced",
	// "strong") or a model name of the pinned engine; "" keeps the settings'.
	Model string `json:"model"`
	// ResolvedModel is the model a new conversation would use on
	// ResolvedEngine because of Model: a tier's mapped model, or Model
	// itself. "" when Model is empty, the tier is unmapped on that engine
	// (the engine's own model applies) or no engine is installed.
	ResolvedModel string           `json:"resolvedModel"`
	Source        agentdefs.Source `json:"source"`
	// Path is the Markdown file, for Open.
	Path string `json:"path"`
}

// AgentDefinitions lists the definitions of the personal folder and the
// workspace roots.
type AgentDefinitions struct {
	// Agents are sorted by name.
	Agents []DefinedAgent `json:"agents"`
	// Problems are files that are not used ("path: reason").
	Problems []string `json:"problems"`
	// Ignored are the project folders in Restricted Mode whose definitions
	// were not loaded.
	Ignored []agentdefs.Ignored `json:"ignored"`
	// PersonalDir holds the user's own definitions.
	PersonalDir string `json:"personalDir"`
}

// AgentDefsAPI lists agent definitions and creates new ones from a template.
// Nothing else: a definition is edited as a file.
type AgentDefsAPI struct {
	app *App
}

// List reads the definitions again (and keeps them for new chats).
func (s *AgentDefsAPI) List() AgentDefinitions {
	v := s.app.agentView()
	set := v.defined.Set
	out := AgentDefinitions{
		Agents: []DefinedAgent{}, Problems: set.Problems, Ignored: set.Ignored,
		PersonalDir: agentdefs.PersonalDir(s.app.configDir),
	}
	for _, d := range set.Agents {
		engine, err := v.resolveEngine(d)
		var model string
		if err == nil {
			model = definedModel(d, engine, v.settings.AgentConfig(string(engine)))
		}
		out.Agents = append(out.Agents, DefinedAgent{
			Name: d.Name, Description: d.Description, Engine: d.Engine, ResolvedEngine: string(engine),
			Model: d.Model, ResolvedModel: model, Source: d.Source, Path: d.Path,
		})
	}
	return out
}

// Create writes a new definition from a template, in the personal folder
// (personal true) or in .flint/agents of the workspace root holding root,
// which must be trusted, and returns its path for the editor.
func (s *AgentDefsAPI) Create(root string, personal bool, name string) (string, error) {
	if !agentdefs.ValidName(name) {
		return "", fmt.Errorf("invalid agent name %q: use lowercase letters, digits and dashes, starting with a letter (at most 32)", name)
	}
	if _, custom := s.app.currentSettings().CustomAgent(name); custom {
		return "", fmt.Errorf("%s is a custom agent's id; choose another name", name)
	}
	// A personal definition may take a project one's name: it wins (Load).
	if d, ok := s.app.loadDefinedAgents().Find(name); ok && !(personal && d.Source == agentdefs.SourceProject) {
		return "", fmt.Errorf("an agent named %s is already defined in %s", name, d.Path)
	}
	if personal {
		return agentdefs.Create(agentdefs.PersonalDir(s.app.configDir), "", name)
	}
	if root == "" {
		return "", errors.New("open a folder to add a project agent")
	}
	root, err := s.app.rootOf(root)
	if err != nil {
		return "", err
	}
	if !s.app.isTrusted(root) {
		return "", errors.New("this folder is in Restricted Mode, where Flint loads no project agents; trust it first")
	}
	return agentdefs.Create(agentdefs.ProjectDir(root), root, name)
}
