package projects

import (
	"context"
	"errors"
	"fmt"

	"flint/internal/cli"
	"flint/internal/prompts"
	"flint/internal/settings"
)

// The checks of a project command an agent outside Flint sends
// (cli.Command.AgentCheck), before Flint asks the user about it. They keep
// the rules of the agents' own tools: no agent changes a project whose
// folder is in Restricted Mode, an agent adds items only while "Add project
// items" is on and delegates only while "Delegate project items" is on.
// Starting an item when it is ready and cloning are refused to agents
// altogether (cli.AgentRefused).

var (
	errAgentAddOff      = errors.New("agents can add project items only while Settings ▸ Agent Context ▸ Actions ▸ \"Add project items\" is on")
	errAgentDelegateOff = errors.New("agents can delegate project items only while Settings ▸ Agent Context ▸ Actions ▸ \"Delegate project items\" is on")
)

// projectRef is where a command names its project: --project or its first
// argument.
type projectRef func(c *cli.Call) string

func flagRef(c *cli.Call) string { return c.String("project") }

func argRef(c *cli.Call) string { return c.Arg(0) }

// agentProject refuses an agent's command on a project whose folder is
// not trusted.
func (a *API) agentProject(ref projectRef) func(context.Context, *cli.Call) error {
	return func(_ context.Context, c *cli.Call) error {
		real, err := a.cliProject(c, ref(c))
		if err != nil {
			return err
		}
		if !a.host.Trusted(real) {
			return fmt.Errorf("the project's folder %s is in Restricted Mode in Flint: agents cannot change it", real)
		}
		return nil
	}
}

// agentProjectWith is agentProject after a switch of
// Settings ▸ Agent Context ▸ Actions: on says whether it is on, off is the
// refusal while it is not.
func (a *API) agentProjectWith(ref projectRef, on func(settings.MCPServer) bool, off error) func(context.Context, *cli.Call) error {
	trusted := a.agentProject(ref)
	return func(ctx context.Context, c *cli.Call) error {
		if !on(a.host.Settings().MCPServer) {
			return off
		}
		return trusted(ctx, c)
	}
}

// delegateAgent is Delegate for an agent outside Flint whose call the user
// approved: by says who, for the tasks and the runs.
func (a *API) delegateAgent(folder, id, root, by string) (DelegateResult, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return DelegateResult{}, err
	}
	if !prompts.ValidName(id) {
		return DelegateResult{}, errors.New("invalid item id")
	}
	return a.delegate(real, id, root, nil, by)
}
