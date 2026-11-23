package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/cli"
)

// flint runs a command line against the feature's commands from cwd, as
// a person (agent false) or an agent.
func (f *fixture) flint(cwd string, agent bool, args ...string) cli.Response {
	f.t.Helper()
	reg, err := cli.NewRegistry(f.api.cliCommands())
	if err != nil {
		f.t.Fatal(err)
	}
	return cli.Dispatch(f.t.Context(), reg, cli.Request{Args: args, Cwd: cwd}, func() cli.Caller {
		return cli.Caller{Agent: agent, What: "codex"}
	})
}

func TestCLIProjectBoard(t *testing.T) {
	f := newFixture(t, map[string]string{"auth": item("Auth", "todo", "repos: api")})
	if r := f.flint(f.root, false, "project", "list"); r.Exit != 0 || !strings.HasPrefix(r.Stdout, "No projects") {
		t.Fatalf("project list before open = %+v", r)
	}
	// Opened by a folder relative to the current directory.
	if r := f.flint(f.base, false, "project", "open", "plan"); r.Exit != 0 || !strings.Contains(r.Stdout, `Opened the project "Acme"`) {
		t.Fatalf("project open = %+v", r)
	}
	if r := f.flint(f.plan, false, "project", "show"); r.Exit != 0 || !strings.Contains(r.Stdout, "Acme ("+f.plan+")") || !strings.Contains(r.Stdout, "1 todo") {
		t.Fatalf("project show = %+v", r)
	}

	// Added by name from anywhere, with every field the board's file has.
	r := f.flint("/", false, "project", "add", "--project", "Acme", "--title", "Rate limit", "--text", "Limit it.", "--priority", "high", "--repos", "api, web", "--after", "auth", "--json")
	var added ItemView
	if r.Exit != 0 || json.Unmarshal([]byte(r.Stdout), &added) != nil || added.ID != "rate-limit" || added.Priority != "high" || len(added.Repos) != 2 {
		t.Fatalf("project add = %+v", r)
	}
	if got := f.read("rate-limit"); !strings.Contains(got, "after: auth") || !strings.Contains(got, "Limit it.") {
		t.Errorf("the file:\n%s", got)
	}
	// The checks are the item parser's and add_project_item's.
	if r := f.flint(f.plan, false, "project", "add", "--title", "X", "--after", "nope"); r.Exit != 1 || !strings.Contains(r.Stderr, `after: the project has no item "nope"`) {
		t.Fatalf("unknown after = %+v", r)
	}
	if r := f.flint(f.plan, false, "project", "add", "--title", "X", "--agent", "gemini"); r.Exit != 1 || !strings.Contains(r.Stderr, "gemini") {
		t.Fatalf("unknown agent = %+v", r)
	}

	r = f.flint(f.plan, false, "project", "items", "--status", "todo", "--json")
	var items []ItemView
	if r.Exit != 0 || json.Unmarshal([]byte(r.Stdout), &items) != nil || len(items) != 2 {
		t.Fatalf("project items = %+v", r)
	}
	if r := f.flint(f.plan, false, "project", "items"); !strings.Contains(r.Stdout, "after auth") {
		t.Errorf("project items:\n%s", r.Stdout)
	}
	if r := f.flint(f.plan, false, "project", "items", "--status", "later"); r.Exit != cli.ExitUsage {
		t.Errorf("a bad status = %+v", r)
	}

	if r := f.flint(f.plan, false, "project", "status", "auth", "done"); r.Exit != 0 || r.Stdout != "auth is done\n" {
		t.Fatalf("project status = %+v", r)
	}
	if r := f.flint(f.plan, false, "project", "status", "auth", "finished"); r.Exit != 1 || !strings.Contains(r.Stderr, `unknown status "finished"`) {
		t.Fatalf("a bad status = %+v", r)
	}
	if r := f.flint(f.plan, false, "project", "plan", "--goal", "Ship v2"); r.Exit != 0 || !strings.Contains(r.Stdout, "Ship v2") {
		t.Fatalf("project plan = %+v", r)
	}

	// An agent reads the board but changes nothing.
	if r := f.flint(f.plan, true, "project", "status", "rate-limit", "done"); r.Exit != 1 || !strings.Contains(r.Stderr, "run by an agent (codex)") {
		t.Fatalf("an agent's status = %+v", r)
	}
	if r := f.flint(f.plan, true, "project", "show"); r.Exit != 0 {
		t.Fatalf("an agent's read = %+v", r)
	}

	if r := f.flint("/", false, "project", "show", "Nope"); r.Exit != 1 || !strings.Contains(r.Stderr, `no open project is named "Nope"`) {
		t.Fatalf("an unknown project = %+v", r)
	}
	if r := f.flint("/", false, "project", "close", f.plan); r.Exit != 0 {
		t.Fatalf("project close = %+v", r)
	}
	if r := f.flint("/", false, "project", "show", f.plan); r.Exit != 1 || !strings.Contains(r.Stderr, "is not an open project") {
		t.Fatalf("show after close = %+v", r)
	}
}

func TestCLIProjectNewAndContext(t *testing.T) {
	f := newFixture(t, nil)
	dir := filepath.Join(f.base, "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := f.flint(dir, false, "project", "new", ".", "--name", "Fresh"); r.Exit != 0 || !strings.Contains(r.Stdout, `Created the project "Fresh"`) {
		t.Fatalf("project new = %+v", r)
	}
	f.h.SetTrusted(dir)
	if r := f.flint(dir, false, "project", "context"); r.Exit != 0 || r.Stdout != "Wrote "+filepath.Join(dir, ContextFile)+"\n" {
		t.Fatalf("project context = %+v", r)
	}
	// Two projects now, and the current directory is in neither.
	f.open()
	if r := f.flint("/", false, "project", "items"); r.Exit != 1 || !strings.Contains(r.Stderr, "which project?") {
		t.Fatalf("no project named = %+v", r)
	}
}
