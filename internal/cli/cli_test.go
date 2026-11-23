package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// testCommands are a small command set: one that reads, one that acts with
// a file flag, a switch and an optional argument.
func testCommands(ran *[]string) []Command {
	record := func(c *Call) {
		if ran != nil {
			*ran = append(*ran, strings.Join(c.Args, ","))
		}
	}
	return []Command{
		{
			Path: "workflow list", Summary: "List them", Kind: Reads, Folder: true,
			Run: func(_ context.Context, c *Call) (Result, error) {
				record(c)
				return Result{Text: "a\nb", JSON: []string{"a", "b"}}, nil
			},
		},
		{
			Path: "workflow run", Summary: "Run one", Kind: Acts, Folder: true,
			Args: []Arg{{Name: "name"}, {Name: "extra", Optional: true}},
			Flags: []Flag{
				{Name: "task", Value: "<text>", Required: true, File: true, Max: 10},
				{Name: "agent", Value: "<agent>"},
				{Name: "together"},
			},
			Run: func(_ context.Context, c *Call) (Result, error) {
				record(c)
				if c.Arg(0) == "fail" {
					return Result{}, errEscape
				}
				return Result{Text: c.String("task") + "|" + c.String("agent") + "|" + c.Folder, JSON: map[string]bool{"together": c.Bool("together")}, Note: "note\x1b[31m"}, nil
			},
		},
	}
}

type escapeError struct{}

func (escapeError) Error() string { return "bad \x1b]0;title\x07 thing" }

var errEscape = escapeError{}

func registry(t *testing.T, ran *[]string) *Registry {
	t.Helper()
	r, err := NewRegistry(testCommands(ran))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func human() Caller { return Caller{} }

func TestNewRegistryRefusesBadDeclarations(t *testing.T) {
	ok := func(_ context.Context, _ *Call) (Result, error) { return Result{}, nil }
	cases := map[string]Command{
		"no kind":           {Path: "task list", Summary: "x", Run: ok},
		"unknown group":     {Path: "thing list", Summary: "x", Kind: Reads, Run: ok},
		"one word":          {Path: "task", Summary: "x", Kind: Reads, Run: ok},
		"three words":       {Path: "task a b", Summary: "x", Kind: Reads, Run: ok},
		"no run":            {Path: "task list", Summary: "x", Kind: Reads},
		"no summary":        {Path: "task list", Kind: Reads, Run: ok},
		"reserved flag":     {Path: "task list", Summary: "x", Kind: Reads, Run: ok, Flags: []Flag{{Name: "json"}}},
		"file suffix flag":  {Path: "task list", Summary: "x", Kind: Reads, Run: ok, Flags: []Flag{{Name: "task-file", Value: "<p>"}}},
		"file switch":       {Path: "task list", Summary: "x", Kind: Reads, Run: ok, Flags: []Flag{{Name: "x", File: true}}},
		"repeated flag":     {Path: "task list", Summary: "x", Kind: Reads, Run: ok, Flags: []Flag{{Name: "x"}, {Name: "x"}}},
		"optional first":    {Path: "task list", Summary: "x", Kind: Reads, Run: ok, Args: []Arg{{Name: "a", Optional: true}, {Name: "b"}}},
		"upper-case word":   {Path: "task List", Summary: "x", Kind: Reads, Run: ok},
		"kind out of range": {Path: "task list", Summary: "x", Kind: Kind(7), Run: ok},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRegistry([]Command{c}); err == nil {
				t.Fatalf("NewRegistry accepted %+v", c)
			}
		})
	}
	good := Command{Path: "task list", Summary: "x", Kind: Reads, Run: ok}
	if _, err := NewRegistry([]Command{good, good}); err == nil {
		t.Error("a path declared twice was accepted")
	}
}

func TestEveryGroupIsACommandWord(t *testing.T) {
	for _, g := range Groups {
		if !IsCommand([]string{g}) {
			t.Errorf("%s is not a command", g)
		}
	}
	for _, args := range [][]string{nil, {"."}, {"/tmp/x"}, {"--install-extension", "x"}, {"-psn_0_1"}, {"workflows"}} {
		if IsCommand(args) {
			t.Errorf("%q is taken for a command", args)
		}
	}
	if !IsCommand([]string{"help"}) {
		t.Error("help is not a command")
	}
}

func TestParse(t *testing.T) {
	c := testCommands(nil)[1]
	cases := []struct {
		name  string
		args  []string
		files map[string]string
		want  string // task|agent|args or the error
	}{
		{"flags after args", []string{"x", "--task", "t", "--agent=a"}, nil, "t|a|x"},
		{"optional arg", []string{"x", "y", "--task=t"}, nil, "t||x,y"},
		{"value may start with a dash", []string{"x", "--task", "-t"}, nil, "-t||x"},
		{"after --", []string{"--task", "t", "--", "--x"}, nil, "t||--x"},
		{"dash is an argument", []string{"-", "--task", "t"}, nil, "t||-"},
		{"from a file", []string{"x"}, map[string]string{"task": "from file"}, ""},
		{"file too long", []string{"x"}, map[string]string{"task": "01234567890"}, "--task is longer than 10 bytes"},
		{"both", []string{"x", "--task", "t"}, map[string]string{"task": "f"}, "give --task or --task-file, not both"},
		{"file of a flag without one", []string{"x", "--task", "t"}, map[string]string{"agent": "f"}, "unknown option --agent-file"},
		{"missing required", []string{"x"}, nil, "--task or --task-file is required"},
		{"missing arg", []string{"--task", "t"}, nil, "<name> is missing"},
		{"too many args", []string{"a", "b", "c", "--task", "t"}, nil, `unexpected argument "c"`},
		{"unknown flag", []string{"x", "--task", "t", "--nope"}, nil, "unknown option --nope"},
		{"short flag", []string{"x", "-t"}, nil, "unknown option -t (options start with --)"},
		{"twice", []string{"x", "--task", "t", "--task", "u"}, nil, "--task is given twice"},
		{"no value", []string{"x", "--task"}, nil, "--task needs a value <text>"},
		{"switch with value", []string{"x", "--task", "t", "--together=yes"}, nil, "--together takes no value"},
		{"not text", []string{"x", "--task", "\xff"}, nil, "--task is not UTF-8 text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call, err := Parse(c, tc.args, tc.files, "/home/u/proj")
			if tc.name == "from a file" {
				if err != nil || call.String("task") != "from file" {
					t.Fatalf("Parse = %v, %v", call, err)
				}
				return
			}
			got := ""
			if err != nil {
				if !IsUsage(err) {
					t.Fatalf("not a usage error: %v", err)
				}
				got = err.Error()
			} else {
				got = call.String("task") + "|" + call.String("agent") + "|" + strings.Join(call.Args, ",")
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseFolder(t *testing.T) {
	c := testCommands(nil)[0]
	for _, tc := range []struct {
		args      []string
		cwd, want string
	}{
		{nil, "/home/u/proj", "/home/u/proj"},
		{[]string{"--folder", "sub/../web"}, "/home/u/proj", "/home/u/proj/web"},
		{[]string{"--folder=/abs/x/"}, "/home/u/proj", "/abs/x"},
		{nil, "", ""},
	} {
		call, err := Parse(c, tc.args, nil, tc.cwd)
		if err != nil || call.Folder != tc.want {
			t.Errorf("Parse(%q, %q).Folder = %q, %v; want %q", tc.args, tc.cwd, call.Folder, err, tc.want)
		}
	}
	if _, err := Parse(testCommands(nil)[0], []string{"--folder", "x", "y"}, nil, "/"); err == nil {
		t.Error("an argument the command does not take was accepted")
	}
	noFolder := Command{Path: "run list", Summary: "x", Kind: Reads, Run: testCommands(nil)[0].Run}
	if _, err := Parse(noFolder, []string{"--folder", "x"}, nil, "/"); err == nil {
		t.Error("--folder accepted by a command without a folder")
	}
}

func TestCallHelpers(t *testing.T) {
	c := NewCall("/w", []string{"a"}, map[string]string{"repos": " web, ,api ", "n": "5", "bad": "x", "json": ""})
	if got := c.List("repos"); strings.Join(got, "|") != "web|api" {
		t.Errorf("List = %q", got)
	}
	if n, err := c.Int("n", 0, 1, 10); err != nil || n != 5 {
		t.Errorf("Int = %d, %v", n, err)
	}
	if n, err := c.Int("missing", 7, 1, 10); err != nil || n != 7 {
		t.Errorf("Int default = %d, %v", n, err)
	}
	for _, name := range []string{"bad", "n"} {
		hi := 10
		if name == "n" {
			hi = 4
		}
		if _, err := c.Int(name, 0, 1, hi); !IsUsage(err) {
			t.Errorf("Int(%s) = %v", name, err)
		}
	}
	if !c.JSON || c.Arg(0) != "a" || c.Arg(3) != "" || c.Path("x/y") != "/w/x/y" || c.Path("/a/../b") != "/b" {
		t.Errorf("call = %+v", c)
	}
}

func TestDispatch(t *testing.T) {
	var ran []string
	r := registry(t, &ran)
	agent := func() Caller { return Caller{Agent: true, What: "claude"} }
	unknown := func() Caller { return Caller{Unknown: true, What: "no pid"} }
	cases := []struct {
		name       string
		args       []string
		who        func() Caller
		exit       int
		out, err   string // substrings ("" = must be empty)
		ranCommand bool
	}{
		{"help", []string{"help"}, human, 0, "workflow run*  Run one", "", false},
		{"no args", nil, human, 0, "usage: flint <command>", "", false},
		{"group help", []string{"workflow", "--help"}, human, 0, "workflow list  List them", "", false},
		{"help group", []string{"help", "workflow"}, human, 0, "Workflows:", "", false},
		{"help command", []string{"help", "workflow", "run"}, human, 0, "(--task <text> | --task-file <path|->)", "", false},
		{"command help", []string{"workflow", "run", "--help"}, human, 0, "agents cannot run it", "", false},
		{"bare group", []string{"workflow"}, human, 2, "", "which command?", false},
		{"unknown command", []string{"workflow", "nope"}, human, 2, "", `unknown command "workflow nope"`, false},
		{"unknown group help", []string{"help", "nope"}, human, 2, "", "no help", false},
		{"usage", []string{"workflow", "run"}, human, 2, "", "usage: flint workflow run <name>", false},
		{"reads", []string{"workflow", "list"}, agent, 0, "a\nb\n", "", true},
		{"acts as a person", []string{"workflow", "run", "x", "--task", "t", "--agent", "codex"}, human, 0, "t|codex|/cwd", "note[31m\n", true},
		{"acts as an agent", []string{"workflow", "run", "x", "--task", "t"}, agent, 1, "",
			"this command was run by an agent (claude). Agents change things through Flint's tools, which ask you first.", false},
		{"acts for an unknown caller", []string{"workflow", "run", "x", "--task", "t"}, unknown, 1, "", "could not tell which program ran this command (no pid)", false},
		{"fails", []string{"workflow", "run", "fail", "--task", "t"}, human, 1, "", "flint: bad ]0;title thing\n", true},
		{"json", []string{"workflow", "run", "x", "--task", "t", "--together", "--json"}, human, 0, "{\n  \"together\": true\n}\n", "note", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran = nil
			res := Dispatch(t.Context(), r, Request{Args: tc.args, Cwd: "/cwd"}, tc.who)
			if res.Exit != tc.exit || !contains(res.Stdout, tc.out) || !contains(res.Stderr, tc.err) {
				t.Errorf("Dispatch(%q) = exit %d\nstdout %q\nstderr %q", tc.args, res.Exit, res.Stdout, res.Stderr)
			}
			if (len(ran) > 0) != tc.ranCommand {
				t.Errorf("ran = %q", ran)
			}
			if strings.ContainsRune(res.Stdout+res.Stderr, 0x1b) {
				t.Error("an escape character reached the output")
			}
		})
	}
}

func contains(s, sub string) bool {
	if sub == "" {
		return s == ""
	}
	return strings.Contains(s, sub)
}

func TestDispatchAsksWhoOnlyForCommandsThatAct(t *testing.T) {
	r := registry(t, nil)
	asked := 0
	who := func() Caller { asked++; return Caller{} }
	Dispatch(t.Context(), r, Request{Args: []string{"workflow", "list"}, Cwd: "/"}, who)
	if asked != 0 {
		t.Error("asked who for a read")
	}
	Dispatch(t.Context(), r, Request{Args: []string{"workflow", "run", "x", "--task", "t"}, Cwd: "/"}, who)
	if asked != 1 {
		t.Errorf("asked %d times for an act", asked)
	}
}

func TestDispatchJSONOfNothingIsAnObject(t *testing.T) {
	r, err := NewRegistry([]Command{{Path: "run list", Summary: "x", Kind: Reads, Run: func(context.Context, *Call) (Result, error) {
		return Result{Text: "t"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	res := Dispatch(t.Context(), r, Request{Args: []string{"run", "list", "--json"}}, human)
	var v map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &v); err != nil || len(v) != 0 {
		t.Errorf("stdout %q: %v", res.Stdout, err)
	}
}

func TestTableAndClean(t *testing.T) {
	got := Table([]string{"A", "B", "C"}, [][]string{{"x", "long value", "z"}, {"yy\x1b[2J", "b\nc", ""}})
	want := "A      B           C\nx      long value  z\nyy[2J  b c         \n"
	if got != want {
		t.Errorf("Table =\n%q\nwant\n%q", got, want)
	}
	if got := Clean("a\tb\nc\x07\x1b[31md‮e\u009bf"); got != "a\tb\nc[31mdef" {
		t.Errorf("Clean = %q", got)
	}
	if got := Cut("abcdef", 4); got != "abc…" {
		t.Errorf("Cut = %q", got)
	}
}

// A command line quoted as one argument is told, not opened as a folder.
func TestJoinedCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"workflow list"}, `flint: "workflow list" is one argument: write flint workflow list`},
		{[]string{"run  wait\tr1", "--json"}, "flint: \"run  wait\\tr1\" is one argument: write flint run wait r1"},
		{[]string{"help workflow"}, `flint: "help workflow" is one argument: write flint help workflow`},
		{[]string{"task "}, `flint: "task " is one argument: write flint task`},
		{[]string{"workflow", "list"}, ""},
		{[]string{"my project"}, ""},
		{[]string{"workflows list"}, ""},
		{[]string{"/Users/me/run list"}, ""},
		{[]string{" "}, ""},
		{nil, ""},
	} {
		if got := JoinedCommand(tc.args); got != tc.want {
			t.Errorf("JoinedCommand(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
