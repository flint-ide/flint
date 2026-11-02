package settings

import (
	"fmt"
	"slices"

	"flint/internal/agent"
	"flint/internal/lsp"
)

// AgentSettings configures one agent CLI ("agents" → "claude" / "codex").
// Changes apply to sessions started afterwards.
type AgentSettings struct {
	// Path of the executable; empty looks the agent up on the login PATH.
	Path string `json:"path"`
	// Model passed to the CLI; empty keeps the CLI's default.
	Model string `json:"model"`
	// Args are extra arguments. They come before Flint's own flags, may not
	// name the flags Flint sets (agent.CheckArgs) and are ignored in
	// Restricted Mode.
	Args []string `json:"args"`
}

// LanguageServerSettings configures one language's server ("languageServers"
// → "go" / "typescript" / "python" / "rust"). A change restarts the server.
type LanguageServerSettings struct {
	// Disabled keeps the server off.
	Disabled bool `json:"disabled"`
	// Command is a known server's name, an absolute path, or another server
	// (which, like rust-analyzer and Python servers, needs a trusted folder);
	// empty picks the first installed known server.
	Command string `json:"command"`
	// Args are added after the server's own; ignored in Restricted Mode.
	Args []string `json:"args"`
}

// AgentConfig is the agent package's view of name's settings.
func (s Settings) AgentConfig(name string) agent.Config {
	a := s.Agents[name]
	return agent.Config{Path: a.Path, Model: a.Model, Args: slices.Clone(a.Args)}
}

// AgentConfigs maps every configured agent to its agent.Config.
func (s Settings) AgentConfigs() map[agent.Kind]agent.Config {
	out := make(map[agent.Kind]agent.Config, len(s.Agents))
	for name := range s.Agents {
		out[agent.Kind(name)] = s.AgentConfig(name)
	}
	return out
}

// ServerConfig is the lsp package's view of language's settings.
func (s Settings) ServerConfig(language string) lsp.ServerConfig {
	l := s.LanguageServers[language]
	return lsp.ServerConfig{Disabled: l.Disabled, Command: l.Command, Args: slices.Clone(l.Args)}
}

func validateAgents(agents map[string]AgentSettings) error {
	for name, a := range agents {
		if err := CheckAgent(name, a); err != nil {
			return fmt.Errorf("agents.%s: %w", name, err)
		}
	}
	return nil
}

// CheckAgent reports why the settings of agent name would be refused.
func CheckAgent(name string, a AgentSettings) error {
	k := agent.Kind(name)
	if !agent.Valid(k) {
		return fmt.Errorf("unsupported agent %q", name)
	}
	if err := agent.CheckPath(a.Path); err != nil {
		return err
	}
	if err := agent.CheckModel(a.Model); err != nil {
		return err
	}
	return agent.CheckArgs(k, a.Args)
}

func validateLanguageServers(servers map[string]LanguageServerSettings) error {
	for language, l := range servers {
		if err := CheckLanguageServer(language, l); err != nil {
			return fmt.Errorf("languageServers.%s: %w", language, err)
		}
	}
	return nil
}

// CheckLanguageServer reports why the settings of language's server would be
// refused. The command follows the agents' path rules: a bare name or an
// absolute path, never one relative to the project.
func CheckLanguageServer(language string, l LanguageServerSettings) error {
	if !lsp.KnownLanguage(language) {
		return fmt.Errorf("unknown language server %q", language)
	}
	if err := agent.CheckPath(l.Command); err != nil {
		return err
	}
	return lsp.CheckArgs(l.Args)
}

// normalizeAgents keeps the known agents, each value valid or empty: a bad
// path or model falls back to the default, bad arguments are all dropped
// (dropping one could leave its value behind as a stray argument).
func normalizeAgents(agents map[string]AgentSettings) map[string]AgentSettings {
	out := map[string]AgentSettings{}
	for name, a := range agents {
		k := agent.Kind(name)
		if !agent.Valid(k) {
			continue
		}
		n := AgentSettings{Path: a.Path, Model: a.Model, Args: cloneArgs(a.Args)}
		if agent.CheckPath(n.Path) != nil {
			n.Path = ""
		}
		if agent.CheckModel(n.Model) != nil {
			n.Model = ""
		}
		if agent.CheckArgs(k, n.Args) != nil {
			n.Args = []string{}
		}
		out[name] = n
	}
	return out
}

func normalizeLanguageServers(servers map[string]LanguageServerSettings) map[string]LanguageServerSettings {
	out := map[string]LanguageServerSettings{}
	for language, l := range servers {
		if !lsp.KnownLanguage(language) {
			continue
		}
		n := LanguageServerSettings{Disabled: l.Disabled, Command: l.Command, Args: cloneArgs(l.Args)}
		if agent.CheckPath(n.Command) != nil {
			n.Command = ""
		}
		if lsp.CheckArgs(n.Args) != nil {
			n.Args = []string{}
		}
		out[language] = n
	}
	return out
}

func cloneArgs(args []string) []string {
	if args == nil {
		return []string{}
	}
	return slices.Clone(args)
}

func cloneAgents(m map[string]AgentSettings) map[string]AgentSettings {
	out := make(map[string]AgentSettings, len(m))
	for k, v := range m {
		v.Args = cloneArgs(v.Args)
		out[k] = v
	}
	return out
}

func cloneLanguageServers(m map[string]LanguageServerSettings) map[string]LanguageServerSettings {
	out := make(map[string]LanguageServerSettings, len(m))
	for k, v := range m {
		v.Args = cloneArgs(v.Args)
		out[k] = v
	}
	return out
}

// Clone returns a deep copy, safe to hand out while the original is shared.
func (s Settings) Clone() Settings {
	out := s
	out.Keymap = make(map[string]string, len(s.Keymap))
	for id, keys := range s.Keymap {
		out.Keymap[id] = keys
	}
	out.WatchExclude = cloneArgs(s.WatchExclude)
	out.Agents = cloneAgents(s.Agents)
	out.CustomAgents = cloneCustomAgents(s.CustomAgents)
	out.LanguageServers = cloneLanguageServers(s.LanguageServers)
	out.CustomTheme = s.CustomTheme.clone()
	out.Usage = s.Usage.clone()
	out.GitHub.Scopes = cloneArgs(s.GitHub.Scopes)
	out.sections = cloneSections(s.sections)
	return out
}

// ChangedLanguageServers lists the languages whose server settings differ.
func ChangedLanguageServers(prev, next Settings) []string {
	var out []string
	for _, l := range lsp.Languages() {
		a, b := prev.LanguageServers[l.Language], next.LanguageServers[l.Language]
		if a.Disabled != b.Disabled || a.Command != b.Command || !slices.Equal(a.Args, b.Args) {
			out = append(out, l.Language)
		}
	}
	return out
}
