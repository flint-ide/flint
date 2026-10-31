package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"time"

	"flint/internal/agent"
	"flint/internal/agentsession/acpdriver"
	"flint/internal/git"
)

const (
	agentTimeout   = 3 * time.Minute
	maxDiffForLLM  = 60 << 10 // keeps commit-message prompts fast and cheap
	recentSubjects = 10
)

// AgentInfo lists installed agents and the one Flint uses.
type AgentInfo struct {
	// Available are the agents that can start: native CLIs first, then
	// custom ACP agents.
	Available []string `json:"available"`
	Preferred string   `json:"preferred"`
	// ConflictAgent resolves merge conflicts in a terminal: the preferred
	// agent when it is a native CLI, else the first installed native one
	// (custom ACP agents have no interactive terminal mode); "" when none.
	ConflictAgent string `json:"conflictAgent"`
}

// AgentAPI delegates work to Claude Code, Codex or a custom ACP agent.
type AgentAPI struct {
	app *App
	git *GitAPI
}

// Info reports installed agents and the preferred one (first available by default).
func (a *AgentAPI) Info() AgentInfo {
	info := AgentInfo{Available: installedAgents(a.app.currentSettings(), exec.LookPath)}
	pref := a.app.preferredAgent()
	if slices.Contains(info.Available, pref) {
		info.Preferred = pref
	} else if len(info.Available) > 0 {
		info.Preferred = info.Available[0]
	}
	info.ConflictAgent = conflictAgent(info.Preferred, info.Available)
	return info
}

// conflictAgent is the agent that resolves conflicts in a terminal.
func conflictAgent(preferred string, available []string) string {
	if isNative(preferred) {
		return preferred
	}
	if i := slices.IndexFunc(available, isNative); i >= 0 {
		return available[i]
	}
	return ""
}

// SuggestCommitMessage asks the agent for a message describing the given changes
// (repo-relative paths; untracked ones listed separately). Nothing is committed.
// A custom agent answers in a one-shot ACP session that may read the project
// but not change it.
func (a *AgentAPI) SuggestCommitMessage(dir string, tracked, untracked []string) (string, error) {
	name, err := a.preferred()
	if err != nil {
		return "", err
	}
	return with(a.git, dir, agentTimeout, func(ctx context.Context, r *git.Repo) (string, error) {
		diff, err := r.ChangesDiff(ctx, tracked, untracked, maxDiffForLLM)
		if err != nil {
			return "", err
		}
		if diff == "" {
			return "", errors.New("no changes to describe")
		}
		prompt := agent.CommitMessagePrompt(r.RecentSubjects(ctx, recentSubjects))
		msg, err := a.headless(ctx, name, r.Root, prompt, diff)
		if err != nil {
			return "", err
		}
		return agent.CleanCommitMessage(msg), nil
	})
}

// headless runs one prompt with agent name in dir and returns its answer.
func (a *AgentAPI) headless(ctx context.Context, name, dir, prompt, input string) (string, error) {
	if isNative(name) {
		kind := agent.Kind(name)
		return agent.Headless(ctx, kind, a.app.agentConfig(kind), dir, prompt, input, !a.app.isTrusted(dir))
	}
	c, ok := a.app.currentSettings().CustomAgent(name)
	if !ok {
		return "", fmt.Errorf("unknown agent %q", name)
	}
	return acpdriver.Headless(ctx, customAgentOptions(c, a.app.untrusted), dir, prompt+"\n\n"+input)
}

// ResolveConflictsCommand returns the shell command that starts an interactive agent
// session to resolve the conflicted files; the frontend runs it in a new terminal in
// root so the user can watch and answer the agent's questions. In an untrusted root
// the extra arguments from settings are left out, as for every agent run. Custom
// ACP agents have no terminal mode, so a native agent does it (Info.ConflictAgent).
func (a *AgentAPI) ResolveConflictsCommand(root, operation string, files []string) (string, error) {
	name := a.Info().ConflictAgent
	if name == "" {
		return "", errors.New("resolving conflicts runs Claude Code (claude) or Codex CLI (codex) in a terminal; install one of them")
	}
	kind := agent.Kind(name)
	return agent.InteractiveCommand(kind, a.app.agentConfig(kind), agent.ConflictPrompt(operation, files), !a.app.isTrusted(root))
}

func (a *AgentAPI) preferred() (string, error) {
	info := a.Info()
	if info.Preferred == "" {
		return "", errors.New("no agent found: install Claude Code (claude) or Codex CLI (codex), or add an ACP agent in Settings ▸ Agents")
	}
	return info.Preferred, nil
}

// agentConfig is the agent CLI's setup from settings (path, model, arguments).
func (a *App) agentConfig(k agent.Kind) agent.Config {
	return a.currentSettings().AgentConfig(string(k))
}

// preferredAgent is the default agent from settings, else the one remembered
// before settings existed.
func (a *App) preferredAgent() string {
	if s := a.currentSettings(); s.DefaultAgent != "" {
		return s.DefaultAgent
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.state.Agent
}
