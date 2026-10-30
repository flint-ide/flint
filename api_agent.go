package main

import (
	"context"
	"errors"
	"slices"
	"time"

	"flint/internal/agent"
	"flint/internal/config"
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
	kinds := agent.Available()
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

// SetPreferred remembers which agent to use.
func (a *AgentAPI) SetPreferred(kind string) error {
	if !agent.Valid(agent.Kind(kind)) {
		return errors.New("unsupported agent " + kind)
	}
	return a.app.setPreferredAgent(kind)
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
		msg, err := agent.Headless(ctx, kind, r.Root, prompt, diff)
		if err != nil {
			return "", err
		}
		return agent.CleanCommitMessage(msg), nil
	})
}

// ResolveConflictsCommand returns the shell command that starts an interactive agent
// session to resolve the conflicted files; the frontend runs it in a new terminal so
// the user can watch and answer the agent's questions.
func (a *AgentAPI) ResolveConflictsCommand(operation string, files []string) (string, error) {
	kind, err := a.kind()
	if err != nil {
		return "", err
	}
	return agent.InteractiveCommand(kind, agent.ConflictPrompt(operation, files))
}

func (a *AgentAPI) kind() (agent.Kind, error) {
	info := a.Info()
	if info.Preferred == "" {
		return "", errors.New("no agent found: install Claude Code (claude) or Codex CLI (codex)")
	}
	return agent.Kind(info.Preferred), nil
}

func (a *App) preferredAgent() string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.state.Agent
}

func (a *App) setPreferredAgent(kind string) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	next := a.state
	next.Agent = kind
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	return nil
}
