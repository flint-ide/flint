package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Caller is who ran a command, as Flint decided from the process at the
// other end of the socket (clisock.Classify).
type Caller struct {
	// Agent: the process descends from one Flint started for an agent (a
	// chat, a workflow step, another process that is not a terminal tab) or
	// runs under an agent CLI. What names it.
	Agent bool
	What  string
	// Detached: the process does not come from a terminal (it has none, or
	// left the session it was started in); What says which.
	Detached bool
	// Unknown: Flint could not tell; What says why.
	Unknown bool
	// PID is the process, Program its program's name ("" when unknown),
	// named in Flint's question for a human decision.
	PID     int
	Program string
	// AgentName is the agent Flint knows the caller runs under (a custom
	// agent by its id), "" when only the program is known (What).
	AgentName string
	// InFlint (an agent's only): the agent runs under a process Flint
	// started for an agent (a chat, a workflow step, an agent task's chat),
	// not under a terminal, or Flint could not tell. Such an agent acts
	// through Flint's tools only: the command line refuses it every command
	// that acts, with no dialog.
	InFlint bool
}

// agentLabel names the agent c runs under for a sentence.
func agentLabel(c Caller) string {
	if c.AgentName != "" {
		return "the agent " + c.AgentName
	}
	return fmt.Sprintf("an agent (%s)", c.What)
}

// AgentLabel names the agent c runs under, for Flint's question.
func AgentLabel(c Caller) string { return agentLabel(c) }

// UserOnlyText refuses an agent a human decision (Command.Human): no dialog
// is shown for it, whatever the agent.
func UserOnlyText(c Caller, path string) string {
	return fmt.Sprintf("flint: only you can take this decision: %s cannot run flint %s, not even with your yes. Run it yourself in a terminal.", agentLabel(c), path)
}

// AgentNeverText refuses an agent a command that acts and is AgentRefused.
func AgentNeverText(c Caller, path string) string {
	return fmt.Sprintf("flint: %s cannot run flint %s, not even with your yes. Run it yourself in a terminal.", agentLabel(c), path)
}

// AgentRequest is what Flint's question to the user about an agent's
// command says: who, which command and where.
type AgentRequest struct {
	Caller Caller
	// Path and Summary are the command's ("project delegate").
	Path, Summary string
	// Args are the words of the command line after the command's path.
	Args []string
	// Files are the texts of its --<flag>-file options, by flag name.
	Files map[string]string
	// Folder is the command's folder (Command.Folder), "" for a command
	// without one; Cwd the client's working directory.
	Folder, Cwd string
}

// RefuseText is the refusal of a command that acts, run by an agent, a
// detached or an unknown caller.
func RefuseText(c Caller) string {
	switch {
	case c.Unknown:
		return fmt.Sprintf("flint: Flint could not tell which program ran this command (%s), so it changes nothing for it. Run it from a terminal.", c.What)
	case c.Detached:
		return fmt.Sprintf("flint: this command did not come from a terminal (%s), so Flint changes nothing for it. Run it in a terminal.", c.What)
	}
	if c.AgentName != "" {
		return fmt.Sprintf("flint: this command was run by the agent %s. Agents change things through Flint's tools, which ask you first.", c.AgentName)
	}
	return fmt.Sprintf("flint: this command was run by an agent (%s). Agents change things through Flint's tools, which ask you first.", c.What)
}

// NotApproved is what a human decision prints when the user did not say
// yes in Flint.
const NotApproved = "not approved in Flint"

// Env is what Dispatch asks besides the request.
type Env struct {
	// Who decides who sent the request; asked only for a command that acts.
	Who func() Caller
	// Confirm asks the user in Flint's native dialog (Cancel the default)
	// and reports a yes; nil answers no. It may wait as long as the dialog
	// is open; ctx ends when the client goes away.
	Confirm func(ctx context.Context, q Question) bool
	// ConfirmAgent asks the user, in Flint's native dialog (Cancel the
	// default), whether an agent outside Flint may run this one command
	// (Command.Agents is AgentAsks): nil after a yes, otherwise the reason
	// nothing was done (declined, not answered in time, a check of
	// Flint's own). nil refuses every agent.
	ConfirmAgent func(ctx context.Context, a AgentRequest) error
}

// maxStdout keeps a response inside MaxMessage.
const maxStdout = MaxMessage - 64<<10

// Dispatch runs one request with no way to ask the user: a human decision
// is never approved (DispatchWith).
func Dispatch(ctx context.Context, r *Registry, req Request, who func() Caller) Response {
	return DispatchWith(ctx, r, req, Env{Who: who})
}

// DispatchWith runs one request. env.Who is asked only for a command that
// acts, before it runs: a detached or an unknown caller's is refused. An
// agent's is refused too, unless the agent runs outside Flint and the
// command lets agents ask (AgentAsks): then its AgentCheck and the user's
// yes in Flint (env.ConfirmAgent) come first. A human decision
// (Command.Human) is never an agent's; for anyone else it is asked in Flint
// with env.Confirm and runs only after a yes, if what it decides on did not
// change meanwhile.
func DispatchWith(ctx context.Context, r *Registry, req Request, env Env) Response {
	args := req.Args
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return help(r, args)
	}
	group := args[0]
	if !slices.Contains(Groups, group) {
		return usageFail(fmt.Sprintf("unknown command %q", short(group)), r.Help())
	}
	if len(args) == 1 {
		return usageFail("which command?", r.GroupHelp(group))
	}
	if isHelp(args[1]) {
		return Response{Stdout: r.GroupHelp(group)}
	}
	c, ok := r.find(group, args[1])
	if !ok {
		return usageFail(fmt.Sprintf("unknown command %q", short(group+" "+args[1])), r.GroupHelp(group))
	}
	rest := args[2:]
	if wantsHelp(rest) {
		return Response{Stdout: CommandHelp(c)}
	}
	call, err := Parse(c, rest, req.Files, req.Cwd)
	if err != nil {
		return usageFail(err.Error(), Usage(c)+"\n")
	}
	if c.Waits || c.Human {
		release, ok := r.wait()
		if !ok {
			return busy()
		}
		defer release()
	}
	var caller Caller
	if c.Kind == Acts {
		caller = Caller{Unknown: true, What: "nobody asked"}
		if env.Who != nil {
			caller = env.Who()
		}
		switch {
		case caller.Unknown || caller.Detached:
			return refuse(RefuseText(caller))
		case caller.Agent:
			if resp, ok := agentAsks(ctx, r, c, call, req, caller, env); !ok {
				return resp
			}
		case c.Human:
			if resp, ok := decide(ctx, c, call, caller, env.Confirm); !ok {
				return resp
			}
		}
	}
	if c.Kind == Acts {
		ctx = WithCaller(ctx, caller)
	}
	res, err := c.Run(ctx, call)
	return result(c, call, res, err)
}

type callerKey struct{}

// WithCaller puts who runs a command on its context (DispatchWith does, for
// a command that acts).
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom is who runs the command whose Run gets ctx: for a command
// that acts, the caller DispatchWith let through (an agent's only after the
// user's yes in Flint). ok is false when nobody said.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// AgentBy says who an agent's caller is for a log line: "an agent (claude,
// pid 4242) outside Flint, from the command line".
func AgentBy(c Caller) string {
	name := c.What
	if c.AgentName != "" {
		name = c.AgentName
	}
	return fmt.Sprintf("an agent (%s, pid %d) outside Flint, from the command line", name, c.PID)
}

// wait takes one of the MaxWaiting slots of the commands that wait; ok is
// false when none is free.
func (r *Registry) wait() (release func(), ok bool) {
	select {
	case r.waiting <- struct{}{}:
		return func() { <-r.waiting }, true
	default:
		return nil, false
	}
}

func busy() Response {
	return Response{Stderr: fmt.Sprintf("flint: %d commands are waiting already: try again when one has ended\n", MaxWaiting), Exit: ExitFailed}
}

func refuse(text string) Response {
	return Response{Stderr: Clean(text) + "\n", Exit: ExitFailed}
}

// agentAsks decides an agent's call to a command that acts. ok is true
// only for an agent outside Flint, an AgentAsks command, a passed
// AgentCheck and the user's yes in Flint; else resp says why nothing was
// done. Agents Flint started, and a Flint that cannot ask, get the refusal
// of old; a human decision is never asked about for an agent.
func agentAsks(ctx context.Context, r *Registry, c Command, call *Call, req Request, caller Caller, env Env) (Response, bool) {
	switch {
	case caller.InFlint || env.ConfirmAgent == nil:
		return refuse(RefuseText(caller)), false
	case c.Human:
		return refuse(UserOnlyText(caller, c.Path)), false
	case c.Agents != AgentAsks:
		return refuse(AgentNeverText(caller, c.Path)), false
	}
	if c.AgentCheck != nil {
		if err := c.AgentCheck(ctx, call); err != nil {
			return result(c, call, Result{}, err), false
		}
	}
	release, ok := r.wait()
	if !ok {
		return busy(), false
	}
	defer release()
	a := AgentRequest{Caller: caller, Path: c.Path, Summary: c.Summary, Args: slices.Clone(req.Args[len(c.Words()):]), Files: maps.Clone(req.Files), Cwd: req.Cwd}
	if c.Folder {
		a.Folder = call.Folder
	}
	if err := env.ConfirmAgent(ctx, a); err != nil {
		return refuse("flint: " + err.Error()), false
	}
	return Response{}, true
}

// decide asks the user about a human decision in Flint. ok is true after a
// yes when the decision is still the one asked about; else resp says why
// nothing was done.
func decide(ctx context.Context, c Command, call *Call, caller Caller, confirm func(context.Context, Question) bool) (Response, bool) {
	q, err := c.Ask(ctx, call)
	if err != nil {
		return result(c, call, Result{}, err), false
	}
	program := caller.Program
	if program == "" {
		program = "an unknown program"
	}
	asked := q
	asked.Message = strings.TrimRight(q.Message, "\n") + fmt.Sprintf("\n\nAsked from the command line (pid %d, %s).", caller.PID, program)
	if confirm == nil || !confirm(ctx, asked) {
		return Response{Stderr: "flint: " + NotApproved + "\n", Exit: ExitFailed}, false
	}
	again, err := c.Ask(ctx, call)
	if err != nil {
		return result(c, call, Result{}, err), false
	}
	if again.Key != q.Key {
		return Response{Stderr: "flint: it changed while Flint asked you, so nothing was done: run the command again\n", Exit: ExitFailed}, false
	}
	return Response{}, true
}

// result is the response to what a command's Run returned.
func result(c Command, call *Call, res Result, err error) Response {
	switch {
	case IsUsage(err):
		return usageFail(err.Error(), Usage(c)+"\n")
	case err != nil:
		return Response{Stderr: "flint: " + Clean(err.Error()) + "\n", Exit: ExitFailed}
	}
	return render(call, res)
}

func render(call *Call, res Result) Response {
	out := Response{Exit: res.Exit}
	if call.JSON {
		v := res.JSON
		if v == nil {
			v = struct{}{}
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return Response{Stderr: "flint: " + err.Error() + "\n", Exit: ExitFailed}
		}
		out.Stdout = buf.String()
	} else {
		out.Stdout = Clean(res.Text)
		if out.Stdout != "" && !strings.HasSuffix(out.Stdout, "\n") {
			out.Stdout += "\n"
		}
	}
	if len(out.Stdout) > maxStdout {
		out.Stdout = out.Stdout[:maxStdout] + "\n"
		out.Stderr = "flint: the output was cut at 960 KiB\n"
		if call.JSON {
			out.Exit = ExitFailed
		}
	}
	if res.Note != "" {
		out.Stderr += Clean(strings.TrimRight(res.Note, "\n")) + "\n"
	}
	return out
}

func help(r *Registry, args []string) Response {
	switch {
	case len(args) <= 1:
		return Response{Stdout: r.Help()}
	case len(args) == 2 && slices.Contains(Groups, args[1]):
		return Response{Stdout: r.GroupHelp(args[1])}
	case len(args) == 3:
		if c, ok := r.find(args[1], args[2]); ok {
			return Response{Stdout: CommandHelp(c)}
		}
	}
	return usageFail(fmt.Sprintf("no help for %q", short(strings.Join(args[1:], " "))), r.Help())
}

func usageFail(msg, usage string) Response {
	return Response{Stderr: "flint: " + Clean(msg) + "\n" + usage, Exit: ExitUsage}
}

func isHelp(s string) bool { return s == "--help" || s == "-h" || s == "help" }

// wantsHelp: --help or -h among the options (before a --).
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}
