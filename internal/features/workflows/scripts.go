package workflows

import (
	"fmt"
	"strings"
)

// What a script step runs, said where the user decides. Only the run
// configuration's entry comes from the checkout the run starts from; the
// code it runs (the package's scripts, the Makefile, the tests, the
// program) is the agent's, in the worktree. Run Workflow… and
// start_workflow's confirmation show each script step's command line and
// whether the sandbox confines it.

// SandboxUnavailable is Listing.Sandbox when workflows.sandbox is on and
// this system has no sandbox (a script step then fails).
const SandboxUnavailable = "unavailable"

// sandboxState is how script steps run now: SandboxOn, SandboxOff or
// SandboxUnavailable.
func (a *API) sandboxState() string {
	if Section.Get(a.host.Settings()).Sandbox != SandboxOn {
		return SandboxOff
	}
	if !a.host.ScriptSandbox().Available {
		return SandboxUnavailable
	}
	return SandboxOn
}

// scriptCommand is the command line the run configuration name of checkout
// runs, or why it cannot be resolved.
func (a *API) scriptCommand(checkout, name string) (line, problem string) {
	l, err := a.host.RunLaunch(checkout, name)
	if err != nil {
		return "", err.Error()
	}
	return cut(l.CommandLine(), maxToolText), ""
}

// withScripts fills in the script steps' commands of l, offered in dir,
// and how they run (Listing.Sandbox).
func (a *API) withScripts(dir string, l Listing) Listing {
	l.Sandbox = a.sandboxState()
	checkout := ""
	for wi, w := range l.Workflows {
		for si, st := range w.Steps {
			if st.Kind != string(KindScript) {
				continue
			}
			if checkout == "" {
				ctx, cancel := a.ctx(requestTimeout)
				repo, err := a.host.Repository(ctx, dir)
				cancel()
				if err != nil {
					l.Workflows[wi].Steps[si].CommandProblem = fmt.Sprintf("%s is not in a git repository", dir)
					continue
				}
				checkout = repo.Root
			}
			l.Workflows[wi].Steps[si].Command, l.Workflows[wi].Steps[si].CommandProblem = a.scriptCommand(checkout, st.Run)
		}
	}
	return l
}

// scriptLines says, for the confirmation of a start, what each script step
// of c runs and whether the sandbox confines it; "" without script steps.
func (a *API) scriptLines(c checkedRun) string {
	var b strings.Builder
	for _, st := range c.steps {
		if st.Kind != KindScript {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("Script steps run on the agent's code in the worktree (only the run configuration comes from your checkout):\n")
		}
		line, problem := a.scriptCommand(c.checkout, st.Run)
		if problem != "" {
			line = "cannot be resolved: " + problem
		}
		fmt.Fprintf(&b, "- %s: run configuration %q: %s\n", st.Title, st.Run, line)
	}
	if b.Len() == 0 {
		return ""
	}
	switch a.sandboxState() {
	case SandboxOn:
		b.WriteString("They run in the sandbox (workflows.sandbox is on).\n")
	case SandboxUnavailable:
		b.WriteString("The sandbox is on but this system has none, so they will fail: switch off “Run script steps in a sandbox” in Settings ▸ Workflows to run them without one (Flint asks you once per folder).\n")
	default:
		b.WriteString("They run WITHOUT a sandbox, with your rights (workflows.sandbox is off).\n")
	}
	return b.String()
}
