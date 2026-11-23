// Package cli is Flint's command line: `flint workflow run …`, `flint task
// list` and the rest. A command is declared by the feature that owns the
// operation (feature.Instance.CLI) and calls the same functions as the
// feature's bound API, so the command line has no second implementation of a
// check. This package is pure: the declarations, argument parsing, help and
// the dispatch of one request. The socket and the client are in
// internal/cli/clisock (docs/ARCHITECTURE.md, "Command-line socket").
package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Kind says whether a command changes anything. It is a required field of
// every Command (the zero value is invalid), so a new command cannot forget
// it: agents running inside Flint may use the read-only ones only.
type Kind int

const (
	// Reads only shows something.
	Reads Kind = iota + 1
	// Acts changes something (starts, approves, writes, removes).
	Acts
)

func (k Kind) String() string {
	switch k {
	case Reads:
		return "reads"
	case Acts:
		return "acts"
	}
	return "invalid"
}

// Exit statuses of the flint command.
const (
	ExitOK = 0
	// ExitFailed: the command ran and failed, or was refused.
	ExitFailed = 1
	// ExitUsage: the command line is wrong.
	ExitUsage = 2
	// ExitNeedsUser: run wait ended because the run needs you.
	ExitNeedsUser = 3
	// ExitTimeout: run wait gave up after --timeout.
	ExitTimeout = 4
)

// Groups are the first words of the commands, in help order. The client
// decides by this list alone whether `flint <word>` is a command or a folder
// to open, before anything of the app starts; a test checks that every
// registered command belongs to one.
var Groups = []string{"workflow", "run", "project", "task"}

// groupTitles head each group in `flint help`.
var groupTitles = map[string]string{
	"workflow": "Workflows",
	"run":      "Workflow runs",
	"project":  "Projects",
	"task":     "Agent tasks",
}

// IsCommand reports whether args (without the program name) ask for a
// command rather than a folder to open.
func IsCommand(args []string) bool {
	return len(args) > 0 && (args[0] == "help" || slices.Contains(Groups, args[0]))
}

// JoinedCommand is the mistake of a command line quoted as one argument
// (`flint "workflow list"`, a common slip in zsh): the first argument has
// white space in it and its first word is a command group or help. It
// returns what to tell the user, "" when args are not that. Whether a
// folder of that name exists is the caller's to check: one opens as usual.
func JoinedCommand(args []string) string {
	if len(args) == 0 || !strings.ContainsFunc(args[0], unicode.IsSpace) {
		return ""
	}
	words := strings.Fields(args[0])
	if len(words) == 0 || !IsCommand(words[:1]) {
		return ""
	}
	return fmt.Sprintf("flint: %q is one argument: write flint %s", args[0], strings.Join(words, " "))
}

// Bounds of a request.
const (
	// MaxWaiting bounds the commands that wait (Command.Waits, a human
	// decision's dialog) running at once: half of the socket's connections.
	MaxWaiting = 8
	// DefaultMaxValue bounds a flag's or argument's value in bytes when its
	// declaration says nothing.
	DefaultMaxValue = 4096
	// MaxText bounds any value, also one read from a file.
	MaxText = 64 << 10
)

// Arg is a positional argument.
type Arg struct {
	// Name is shown as <name>.
	Name string
	// Optional arguments come last.
	Optional bool
	// Max bounds the value in bytes (DefaultMaxValue).
	Max int
}

// Flag is a --flag.
type Flag struct {
	// Name without the dashes: "task".
	Name string
	// Value is the placeholder of the value ("<text>"); "" makes a switch
	// that takes none.
	Value string
	// Usage is one line.
	Usage string
	// Required: the command refuses to run without it.
	Required bool
	// File: the value may also come from --<name>-file <path>, or "-" for
	// standard input. The client reads the file (bounded, regular files
	// only) and sends its text; Flint never opens a path given here.
	File bool
	// Max bounds the value in bytes (DefaultMaxValue).
	Max int
}

// Command is one command of the command line.
type Command struct {
	// Path is the command's words: "workflow run".
	Path string
	// Summary is one line for `flint help`.
	Summary string
	// Help is more text for `flint <command> --help`: what Flint asks you
	// in a dialog first, what the output means.
	Help string
	// Kind is Reads or Acts; required.
	Kind Kind
	// Folder: the command acts in a workspace folder, --folder or the
	// working directory.
	Folder bool
	Args   []Arg
	Flags  []Flag
	// Run runs it. The text is printed as is; the value is printed as JSON
	// with --json.
	Run func(ctx context.Context, c *Call) (Result, error)
	// Waits: the command may wait without a bound (run wait). At most
	// MaxWaiting such commands and human decisions run at once, so waiters
	// cannot take every connection of the socket.
	Waits bool
	// Human marks a decision only the user takes (approving a step,
	// continuing past a failed one). It must act. Whoever runs it, a person
	// in a terminal included, Flint asks first in its own native dialog,
	// which the web view cannot answer (Env.Confirm, with Ask's question),
	// and runs it only after a yes ("not approved in Flint", exit 1,
	// otherwise). An agent's, a detached or an unknown caller is refused
	// before any dialog.
	Human bool
	// Ask describes a Human decision for the dialog; it must change
	// nothing. It is asked again after the yes, and the decision runs only
	// when its Key is the same (what the user saw is what is decided).
	Ask func(ctx context.Context, c *Call) (Question, error)
}

// Question is what Flint's dialog asks for a human decision.
type Question struct {
	// Title, Message and OK (the button that says yes) are shown; Cancel is
	// the default button. Dispatch adds to Message which process asked.
	Title, Message, OK string
	// Key identifies what is decided (a run, its step and attempt): the
	// decision runs only when it is the same after the yes.
	Key string
}

// Result is what a command prints.
type Result struct {
	// Text is the human output; a missing final newline is added.
	Text string
	// JSON is printed instead of Text with --json; nil prints {}.
	JSON any
	// Note goes to standard error in both modes (a hint, not the result).
	Note string
	// Exit is the exit status (ExitOK when 0).
	Exit int
}

// Call is one parsed invocation.
type Call struct {
	// Args are the positional arguments, as declared.
	Args []string
	// Folder is --folder or the client's working directory, absolute and
	// clean. Commands check it against the workspace like the UI's folders.
	Folder string
	// JSON: --json was given.
	JSON    bool
	flags   map[string]string
	workDir string
}

// NewCall builds a Call by hand, for tests of a feature's commands: flags by
// name ("" for a switch), the folder as Folder and the working directory.
func NewCall(folder string, args []string, flags map[string]string) *Call {
	c := &Call{Args: args, Folder: folder, workDir: folder, flags: map[string]string{}}
	for k, v := range flags {
		c.flags[k] = v
	}
	c.JSON = c.Has("json")
	return c
}

// Arg is the i-th positional argument, "" when absent.
func (c *Call) Arg(i int) string {
	if i < len(c.Args) {
		return c.Args[i]
	}
	return ""
}

// String is the value of a flag ("" when absent).
func (c *Call) String(name string) string { return c.flags[name] }

// Has reports whether a flag (or switch) was given.
func (c *Call) Has(name string) bool {
	_, ok := c.flags[name]
	return ok
}

// Bool reports whether a switch was given.
func (c *Call) Bool(name string) bool { return c.Has(name) }

// List splits a comma-separated flag into its trimmed, non-empty parts.
func (c *Call) List(name string) []string {
	var out []string
	for _, p := range strings.Split(c.flags[name], ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Int is a flag's value as a whole number; def when absent.
func (c *Call) Int(name string, def, lo, hi int) (int, error) {
	v, ok := c.flags[name]
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, Usagef("--%s must be a whole number from %d to %d", name, lo, hi)
	}
	return n, nil
}

// UsageError is a wrong command line: exit status 2 and the usage.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

// Usagef makes a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{msg: fmt.Sprintf(format, args...)}
}

// IsUsage reports whether err is a UsageError.
func IsUsage(err error) bool {
	var u *UsageError
	return errors.As(err, &u)
}

// Words splits the command's path.
func (c Command) Words() []string { return strings.Fields(c.Path) }

// Group is the command's first word.
func (c Command) Group() string {
	if w := c.Words(); len(w) > 0 {
		return w[0]
	}
	return ""
}
