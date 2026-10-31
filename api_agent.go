package main

import (
	"context"
	"errors"
	"slices"
	"time"

	"flint/internal/agent"
	"flint/internal/git"
)

const (
	agentTimeout   = 3 * time.Minute
	maxDiffForLLM  = 60 << 10 // keeps commit-message prompts fast and cheap
	recentSubjects = 10
)

// AgentInfo lists installed agents and the one Flint uses.
type AgentInfo struct {
	Available []string `json:"available"`
	Preferred string   `json:"preferred"`
}

// AgentAPI delegates work to Claude Code / Codex.
type AgentAPI struct {
	app *App
	git *GitAPI
}

// Info reports installed agents and the preferred one (first available by default).
func (a *AgentAPI) Info() AgentInfo {
	kinds := agent.Available(a.app.currentSettings().AgentConfigs())
	info := AgentInfo{Available: make([]string, 0, len(kinds))}
	for _, k := range kinds {
		info.Available = append(info.Available, string(k))
	}
	pref := a.app.preferredAgent()
	if slices.Contains(info.Available, pref) {
		info.Preferred = pref
	} else if len(info.Available) > 0 {
		info.Preferred = info.Available[0]
	}
	return info
}

// SuggestCommitMessage asks the agent for a message describing the given changes
// (repo-relative paths; untracked ones listed separately). Nothing is committed.
func (a *AgentAPI) SuggestCommitMessage(dir string, tracked, untracked []string) (string, error) {
	kind, err := a.kind()
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
		msg, err := agent.Headless(ctx, kind, a.app.agentConfig(kind), r.Root, prompt, diff, !a.app.isTrusted(r.Root))
		if err != nil {
			return "", err
		}
		return agent.CleanCommitMessage(msg), nil
	})
}

// ResolveConflictsCommand returns the shell command that starts an interactive agent
// session to resolve the conflicted files; the frontend runs it in a new terminal in
// root so the user can watch and answer the agent's questions. In an untrusted root
// the extra arguments from settings are left out, as for every agent run.
func (a *AgentAPI) ResolveConflictsCommand(root, operation string, files []string) (string, error) {
	kind, err := a.kind()
	if err != nil {
		return "", err
	}
	return agent.InteractiveCommand(kind, a.app.agentConfig(kind), agent.ConflictPrompt(operation, files), !a.app.isTrusted(root))
}

func (a *AgentAPI) kind() (agent.Kind, error) {
	info := a.Info()
	if info.Preferred == "" {
		return "", errors.New("no agent found: install Claude Code (claude) or Codex CLI (codex)")
	}
	return agent.Kind(info.Preferred), nil
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
