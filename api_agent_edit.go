package main

import (
	"context"
	"errors"
	"path/filepath"

	"flint/internal/agent"
)

// InlineEdit asks the default agent to rewrite req.Selection of the file at
// req.Path (absolute, inside an open folder) as req.Instruction says and
// returns the replacement text; the editor applies it. The run is headless,
// in the file's folder, like commit messages: in Restricted Mode Claude
// loads only the user's settings and extra arguments are dropped, and a
// custom agent refuses to start. Cancelling the call (Esc in the editor)
// stops the agent.
func (a *AgentAPI) InlineEdit(ctx context.Context, req agent.EditRequest) (string, error) {
	if !filepath.IsAbs(req.Path) {
		return "", errors.New("the file path must be absolute")
	}
	root, err := a.app.rootOf(filepath.Clean(req.Path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, filepath.Clean(req.Path))
	if err != nil {
		return "", err
	}
	req.Path = filepath.ToSlash(rel)
	prompt, input, err := agent.InlineEditPrompt(req)
	if err != nil {
		return "", err
	}
	name, err := a.preferred()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, agentTimeout)
	defer cancel()
	if a.app.ctx != nil {
		stop := context.AfterFunc(a.app.ctx, cancel) // Flint quits
		defer stop()
	}
	answer, err := a.headless(ctx, name, root, prompt, input)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", ctx.Err()
		}
		return "", err
	}
	return agent.CleanReplacement(answer, req.Selection), nil
}
