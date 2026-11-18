package main

import (
	"context"
	"errors"
	"fmt"
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
	// agent when it is a native CLI, the engine of the preferred agent when
	// that is a defined agent, else the first installed native one (custom
	// ACP agents have no interactive terminal mode); "" when none. It is
	// always "claude", "codex" or "".
	ConflictAgent string `json:"conflictAgent"`
}

// AgentAPI delegates work to Claude Code, Codex or a custom ACP agent.
type AgentAPI struct {
	app *App
	git *GitAPI
}

// Info reports installed agents and the preferred one (first available by default).
func (a *AgentAPI) Info() AgentInfo { return a.info(a.app.agentView()) }

// info is Info from what v read.
func (a *AgentAPI) info(v agentView) AgentInfo {
	info := AgentInfo{Available: v.available()}
	if slices.Contains(info.Available, v.preferred) {
		info.Preferred = v.preferred
	} else if len(info.Available) > 0 {
		info.Preferred = info.Available[0]
	}
	info.ConflictAgent = conflictAgent(info.Preferred, info.Available, func(name string) string {
		run, _, _ := v.native(name)
		return string(run.kind)
	})
	return info
}

// conflictAgent is the native agent that resolves conflicts in a terminal:
// the engine of preferred (engine returns "" for an agent without one, a
// custom ACP agent), else the first native agent available.
func conflictAgent(preferred string, available []string, engine func(name string) string) string {
	if preferred != "" {
		if k := engine(preferred); k != "" {
			return k
		}
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
	v := a.app.agentView()
	name, err := a.preferred(v)
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
		msg, err := a.headless(ctx, v, name, r.Root, prompt, diff, false)
		if err != nil {
			return "", err
		}
		return agent.CleanCommitMessage(msg), nil
	})
}

// headless runs one prompt with agent name (as v knows it) in dir and returns
// its answer. A defined agent runs as its engine with its model; its standing
// instructions go along only when withInstructions is set: for a review,
// whose answer Flint parses but whose judgement is the agent's. Runs whose
// answer has a fixed mechanical form (a commit message, a replacement for a
// selection) leave them out. A custom (ACP) agent gets the prompt alone.
func (a *AgentAPI) headless(ctx context.Context, v agentView, name, dir, prompt, input string, withInstructions bool) (string, error) {
	run, native, err := v.native(name)
	if err != nil {
		return "", err
	}
	if native {
		if !withInstructions {
			run.instructions = ""
		}
		return agent.Headless(ctx, run.kind, run.cfg, dir, prompt, input, run.instructions, !a.app.isTrusted(dir))
	}
	c, ok := v.settings.CustomAgent(name)
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
	kind := agent.Kind(name) // Info.ConflictAgent is always a native agent
	return agent.InteractiveCommand(kind, a.app.agentConfig(kind), agent.ConflictPrompt(operation, files), !a.app.isTrusted(root))
}

// preferred is the agent headless runs use, from what v read.
func (a *AgentAPI) preferred(v agentView) (string, error) {
	info := a.info(v)
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
