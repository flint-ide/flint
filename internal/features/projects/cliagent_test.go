package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"flint/internal/cli"
)

// agentFlint runs a command line as claude outside Flint, with a dialog
// that answers yes (asked counts it).
func (f *fixture) agentFlint(asked *int, args ...string) cli.Response {
	f.t.Helper()
	reg, err := cli.NewRegistry(f.api.cliCommands())
	if err != nil {
		f.t.Fatal(err)
	}
	return cli.DispatchWith(f.t.Context(), reg, cli.Request{Args: args, Cwd: f.root}, cli.Env{
		Who:     func() cli.Caller { return cli.Caller{Agent: true, What: "claude", PID: 9} },
		Confirm: func(context.Context, cli.Question) bool { f.t.Error("a human decision's dialog"); return false },
		ConfirmAgent: func(context.Context, cli.AgentRequest) error {
			*asked++
			return errors.New(cli.NotApproved) // the user said no: nothing starts
		},
	})
}

// An agent outside Flint changes a project only after Flint asks the user,
// only in a project whose folder is trusted, adds and delegates only while
// their switches are on, and never starts an item when ready or clones.
func TestCLIAgentOutsideFlintProject(t *testing.T) {
	f := newFixture(t, map[string]string{"auth": item("Auth", "todo", "repos: api"), "rl": item("Rate limit", "todo", "after: auth")})
	f.open()
	asked := 0
	expect := func(want string, wantAsked int, args ...string) {
		t.Helper()
		r := f.agentFlint(&asked, args...)
		if r.Exit != cli.ExitFailed || !strings.Contains(r.Stderr, want) || asked != wantAsked {
			t.Fatalf("%q = %+v, asked %d (want %q, %d)", args, r, asked, want, wantAsked)
		}
	}
	expect("not approved in Flint", 1, "project", "status", "auth", "done", "--project", f.plan)
	expect(`"Delegate project items" is on`, 1, "project", "delegate", "auth", "--project", f.plan)
	expect(`"Add project items" is on`, 1, "project", "add", "--title", "X", "--project", f.plan)

	s := f.h.Settings()
	s.MCPServer.Actions.Delegate, s.MCPServer.Actions.Projects = true, true
	f.h.SetSettings(s)
	expect("not approved in Flint", 2, "project", "delegate", "auth", "--project", f.plan)
	expect("not approved in Flint", 3, "project", "add", "--title", "X", "--project", f.plan)

	expect("cannot run flint project start-when-ready, not even with your yes", 3, "project", "start-when-ready", "rl", "--project", f.plan)
	expect("cannot run flint project clone, not even with your yes", 3, "project", "clone", "api", f.base, "--project", f.plan)

	f.h.SetUntrusted(f.plan)
	expect("is in Restricted Mode in Flint: agents cannot change it", 3, "project", "delegate", "auth", "--project", f.plan)
	expect("is in Restricted Mode in Flint: agents cannot change it", 3, "project", "status", "auth", "done", "--project", f.plan)
	if b := f.load(); b.Items[0].Status != "todo" {
		t.Fatalf("an agent changed the board: %+v", b.Items[0])
	}
}
