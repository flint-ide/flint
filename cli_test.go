//go:build !server

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/cli"
	"flint/internal/cli/clisock"
	"flint/internal/feature"
	"flint/internal/features/projects"
	"flint/internal/features/workflows"
	"flint/internal/orchestrate"
)

// cliApp is a task fixture with the Projects and Workflows features built,
// whose command line answers on a socket in a short temporary folder.
func cliApp(t *testing.T) (*taskFixture, *cli.Registry) {
	t.Helper()
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	f := queueFixture(t, 0)
	f.app.features = feature.New([]feature.Module{projects.Module, workflows.Module}, featureHost{f.app})
	reg, err := cli.NewRegistry(f.app.cliCommands())
	if err != nil {
		t.Fatal(err)
	}
	return f, reg
}

// Every command says whether it reads or acts, and this table is the
// review of that choice: a new command fails here until it is added.
func TestCLICommandKinds(t *testing.T) {
	_, reg := cliApp(t)
	acts := []string{
		"workflow new", "workflow run",
		"run approve", "run send-back", "run check", "run retry", "run continue", "run pause", "run resume", "run stop", "run remove",
		"project open", "project new", "project close", "project add", "project status", "project delegate",
		"project start-when-ready", "project cancel-start", "project context", "project locate", "project clone",
		"project cancel-clone", "project rename-config",
		"task new", "task start", "task update", "task merge", "task pr", "task remove",
	}
	reads := []string{
		"workflow list", "workflow show", "workflow coordinate",
		"run list", "run show", "run wait",
		"project list", "project show", "project items", "project plan",
		"task list", "task show", "task diff", "task compare",
	}
	var got []string
	for _, c := range reg.Commands() {
		got = append(got, c.Path)
		want := cli.Reads
		if slices.Contains(acts, c.Path) {
			want = cli.Acts
		} else if !slices.Contains(reads, c.Path) {
			t.Errorf("%q is in neither list: decide whether it reads or acts", c.Path)
			continue
		}
		if c.Kind != want {
			t.Errorf("%q is %v, want %v", c.Path, c.Kind, want)
		}
		if !slices.Contains(cli.Groups, c.Group()) {
			t.Errorf("%q is not in a group the client knows", c.Path)
		}
	}
	for _, p := range append(slices.Clone(acts), reads...) {
		if !slices.Contains(got, p) {
			t.Errorf("%q is not registered", p)
		}
	}
}

// An agent's (and a detached) caller is refused every command that acts,
// before it runs: approving a step and continuing past a failure above all.
func TestCLIRefusesAgentsEveryCommandThatActs(t *testing.T) {
	_, reg := cliApp(t)
	cmds := reg.Commands()
	for i, c := range cmds {
		path := c.Path
		cmds[i].Run = func(context.Context, *cli.Call) (cli.Result, error) {
			t.Errorf("%s ran for an agent", path)
			return cli.Result{}, nil
		}
	}
	guarded, err := cli.NewRegistry(cmds)
	if err != nil {
		t.Fatal(err)
	}
	agent := func() cli.Caller { return cli.Caller{Agent: true, What: "claude"} }
	for _, c := range cmds {
		if c.Kind != cli.Acts {
			continue
		}
		args := c.Words()
		for range c.Args {
			args = append(args, "x")
		}
		for _, fl := range c.Flags {
			if fl.Required {
				args = append(args, "--"+fl.Name, "x")
			}
		}
		res := cli.Dispatch(t.Context(), guarded, cli.Request{Args: args, Cwd: "/"}, agent)
		if res.Exit != cli.ExitFailed || !strings.Contains(res.Stderr, "this command was run by an agent (claude)") {
			t.Errorf("%q for an agent = %+v", args, res)
		}
	}
	for _, path := range []string{"run approve", "run continue"} {
		res := cli.Dispatch(t.Context(), guarded, cli.Request{Args: append(strings.Fields(path), "0123456789abcdef")}, agent)
		if res.Stderr != "flint: this command was run by an agent (claude). Agents change things through Flint's tools, which ask you first.\n" {
			t.Errorf("%s for an agent: %q", path, res.Stderr)
		}
		detached := func() cli.Caller { return cli.Caller{Detached: true, What: "it has no terminal"} }
		res = cli.Dispatch(t.Context(), guarded, cli.Request{Args: append(strings.Fields(path), "0123456789abcdef")}, detached)
		if res.Exit != cli.ExitFailed || !strings.Contains(res.Stderr, "did not come from a terminal") {
			t.Errorf("%s for a detached process: %q", path, res.Stderr)
		}
		// A person, with no way to ask in Flint (Dispatch): not approved.
		person := func() cli.Caller { return cli.Caller{PID: 1} }
		res = cli.Dispatch(t.Context(), guarded, cli.Request{Args: append(strings.Fields(path), "0123456789abcdef")}, person)
		if res.Exit != cli.ExitFailed || res.Stdout != "" {
			t.Errorf("%s for a person without a dialog: %+v", path, res)
		}
	}
}

// The agent tasks' commands through the socket and the flint client's code.
func TestCLITaskCommands(t *testing.T) {
	f, reg := cliApp(t)
	d, err := os.MkdirTemp("/tmp", "ftask")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	paths := clisock.PathsIn(filepath.Join(d, "run"))
	var mu sync.Mutex
	caller := cli.Caller{}
	srv, err := clisock.Start(clisock.Options{Paths: paths, Registry: reg, Classify: func(int) cli.Caller {
		mu.Lock()
		defer mu.Unlock()
		return caller
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	flint := func(stdin string, args ...string) (string, string, int) {
		t.Helper()
		var out, errb bytes.Buffer
		code := clisock.RunIn(t.Context(), paths, args, f.repo, strings.NewReader(stdin), &out, &errb)
		return out.String(), errb.String(), code
	}

	out, errs, code := flint("Add a Retry button\n\nRetry the last request.", "task", "new", "--description-file", "-", "--agent", "fake", "--json")
	var v TaskView
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v.Title != "Add a Retry button" || v.Agent != "fake" {
		t.Fatalf("task new = %d %s\n%s", code, errs, out)
	}
	f.waitStarts() // Go started its chat, as for a task a feature makes
	if !slices.ContainsFunc(f.app.orch.All(), func(n orchestrate.Node) bool { return cleanFolder(n.Cwd) == cleanFolder(v.Worktree) }) {
		t.Error("no chat was started for the task")
	}
	if out, _, code := flint("", "task", "list"); code != 0 || !strings.Contains(out, v.ID) || !strings.HasPrefix(out, "TASK") {
		t.Fatalf("task list = %d\n%s", code, out)
	}
	if out, _, code := flint("", "task", "show", v.ID); code != 0 || !strings.Contains(out, "Branch:   "+v.Branch) {
		t.Fatalf("task show = %d\n%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(v.Worktree, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, code := flint("", "task", "diff", v.ID); code != 0 || !strings.Contains(out, "b.txt (new)") || !strings.Contains(out, "1 file(s) changed") {
		t.Fatalf("task diff = %d\n%s", code, out)
	}
	if _, errs, code := flint("", "task", "merge", v.ID); code != 1 || !strings.Contains(errs, "there is nothing to merge yet") {
		t.Fatalf("task merge = %d %q", code, errs)
	}
	if _, errs, code := flint("", "task", "compare", "nope"); code != 1 || errs == "" {
		t.Fatalf("task compare = %d %q", code, errs)
	}

	// An agent lists but does not remove.
	mu.Lock()
	caller = cli.Caller{Agent: true, What: "fake"}
	mu.Unlock()
	if _, errs, code := flint("", "task", "remove", v.ID); code != 1 || !strings.Contains(errs, "run by an agent (fake)") {
		t.Fatalf("an agent's remove = %d %q", code, errs)
	}
	if _, _, code := flint("", "task", "list", "--all"); code != 0 {
		t.Fatalf("an agent's list = %d", code)
	}
	mu.Lock()
	caller = cli.Caller{}
	mu.Unlock()

	// The new file would be lost: Flint asks in its own dialog first.
	var asked []string
	f.app.ask = func(title, msg, ok string) bool {
		asked = append(asked, title+": "+msg)
		return false
	}
	if _, errs, code := flint("", "task", "remove", v.ID); code != 1 || errs != "flint: not removed: cancelled\n" {
		t.Fatalf("task remove declined = %d %q", code, errs)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "1 uncommitted change(s) or new file(s)") {
		t.Fatalf("asked %q", asked)
	}
	f.app.ask = func(string, string, string) bool { return true }
	if out, errs, code := flint("", "task", "remove", v.ID); code != 0 || !strings.HasPrefix(out, "Removed task "+v.ID) {
		t.Fatalf("task remove = %d %q %q", code, out, errs)
	}
	if _, errs, code := flint("", "task", "show", v.ID); code != 1 || errs != "flint: no such task (it was removed)\n" {
		t.Fatalf("show after remove = %d %q", code, errs)
	}
	outside := t.TempDir()
	if _, errs, code := flint("", "task", "list", "--folder", outside); code != 1 || !strings.Contains(errs, "is not in a folder open in Flint") {
		t.Fatalf("outside = %d %q", code, errs)
	}
}

// scripts/flint hands the same first words to the app's binary as the
// binary takes for commands.
func TestFlintScriptKnowsTheCommandGroups(t *testing.T) {
	data, err := os.ReadFile("scripts/flint")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(append(slices.Clone(cli.Groups), "help"), " | ") + ")"
	if !strings.Contains(string(data), "\n"+want+"\n") {
		t.Errorf("scripts/flint has no case line %q", want)
	}
}

// scripts/flint tells a command line quoted as one argument, for every
// group, and still opens a folder of that name.
func TestFlintScriptTellsAQuotedCommandLine(t *testing.T) {
	script, err := filepath.Abs("scripts/flint")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, group := range append(slices.Clone(cli.Groups), "help") {
		arg := group + " list"
		cmd := exec.Command("sh", script, arg)
		cmd.Dir = dir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != cli.ExitUsage {
			t.Errorf("flint %q: %v", arg, err)
		}
		if want := cli.JoinedCommand([]string{arg}) + "\n"; stderr.String() != want {
			t.Errorf("flint %q said %q, want %q", arg, stderr.String(), want)
		}
	}
	// A folder of that name is opened: the script goes on past the check to
	// `open` (a fake on PATH, so no Flint starts) or, without Flint.app
	// installed, says so.
	if err := os.Mkdir(filepath.Join(dir, "task list"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\necho \"open $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script, "task list")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir, "PATH="+bin+":/usr/bin:/bin")
	out, _ := cmd.CombinedOutput()
	if s := string(out); strings.Contains(s, "is one argument") || !strings.Contains(s, "task list") && !strings.Contains(s, "is not installed") {
		t.Errorf("a folder named like a command was not opened: %s", out)
	}
}
