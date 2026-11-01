package settings

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"flint/internal/agent"
)

// CustomAgent is an agent Flint talks to over the Agent Client Protocol
// ("customAgents"): any CLI with an ACP mode, added without code. There is no
// environment field on purpose: settings.json is plain text, so API keys
// don't belong here; agents log in with their own command and keep the
// credentials themselves.
type CustomAgent struct {
	// ID names the agent in the agent pickers and in "defaultAgent"
	// (lowercase, may not be a built-in agent's name).
	ID string `json:"id"`
	// Name is shown in Settings; empty shows the ID.
	Name string `json:"name"`
	// Command is the executable: a bare name looked up on the login PATH or
	// an absolute path, never one relative to the project.
	Command string `json:"command"`
	// Args start the CLI in ACP mode (for example "--experimental-acp").
	Args []string `json:"args"`
}

// Limits for custom agents.
const (
	MaxCustomAgents   = 16
	maxCustomNameLen  = 64
	maxCustomAgentID  = 32
	customAgentIDHint = "lowercase letters, digits and dashes, starting with a letter"
)

var customAgentIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// CheckCustomAgent reports why one custom agent would be refused. The list
// rules (unique IDs, count) are checked by validateCustomAgents.
func CheckCustomAgent(c CustomAgent) error {
	switch {
	case c.ID == "":
		return errors.New("id is empty")
	case len(c.ID) > maxCustomAgentID || !customAgentIDPattern.MatchString(c.ID):
		return fmt.Errorf("id %q: use %s (at most %d)", c.ID, customAgentIDHint, maxCustomAgentID)
	case agent.Valid(agent.Kind(c.ID)):
		return fmt.Errorf("id %q is a built-in agent", c.ID)
	case len(c.Name) > maxCustomNameLen:
		return fmt.Errorf("name longer than %d characters", maxCustomNameLen)
	case c.Name != strings.TrimSpace(c.Name) || strings.ContainsFunc(c.Name, unicode.IsControl):
		return fmt.Errorf("invalid name %q", c.Name)
	case c.Command == "":
		return errors.New("command is empty")
	}
	if err := agent.CheckPath(c.Command); err != nil {
		return fmt.Errorf("command: %w", err)
	}
	// Flint adds no flags of its own after these, so none is reserved.
	return agent.CheckPlainArgs(c.Args)
}

// CheckCustomAgents reports why a list of custom agents would be refused.
func CheckCustomAgents(list []CustomAgent) error { return validateCustomAgents(list) }

func validateCustomAgents(list []CustomAgent) error {
	if len(list) > MaxCustomAgents {
		return fmt.Errorf("too many custom agents (max %d)", MaxCustomAgents)
	}
	seen := map[string]bool{}
	for _, c := range list {
		if err := CheckCustomAgent(c); err != nil {
			return fmt.Errorf("customAgents: %w", err)
		}
		if seen[c.ID] {
			return fmt.Errorf("customAgents: id %q is used twice", c.ID)
		}
		seen[c.ID] = true
	}
	return nil
}

// normalizeCustomAgents keeps the valid entries (a broken one is dropped
// whole: half an agent could start the wrong command), the first of each ID,
// up to the limit.
func normalizeCustomAgents(list []CustomAgent) []CustomAgent {
	out := []CustomAgent{}
	for _, c := range list {
		if len(out) >= MaxCustomAgents {
			break
		}
		c.Args = cloneArgs(c.Args)
		if CheckCustomAgent(c) != nil || slices.ContainsFunc(out, func(o CustomAgent) bool { return o.ID == c.ID }) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func cloneCustomAgents(list []CustomAgent) []CustomAgent {
	out := make([]CustomAgent, len(list))
	for i, c := range list {
		c.Args = cloneArgs(c.Args)
		out[i] = c
	}
	return out
}

// CustomAgent returns the custom agent with id.
func (s Settings) CustomAgent(id string) (CustomAgent, bool) {
	i := slices.IndexFunc(s.CustomAgents, func(c CustomAgent) bool { return c.ID == id })
	if i < 0 {
		return CustomAgent{}, false
	}
	c := s.CustomAgents[i]
	c.Args = cloneArgs(c.Args)
	return c, true
}

// Equal reports whether two custom agents start the same way.
func (c CustomAgent) Equal(o CustomAgent) bool {
	return c.ID == o.ID && c.Name == o.Name && c.Command == o.Command && slices.Equal(c.Args, o.Args)
}
