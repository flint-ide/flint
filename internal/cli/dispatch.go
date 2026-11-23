package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// refused reports whether c may not run a command that acts.
func refused(c Caller) bool { return c.Agent || c.Unknown || c.Detached }

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
}

// maxStdout keeps a response inside MaxMessage.
const maxStdout = MaxMessage - 64<<10

// Dispatch runs one request with no way to ask the user: a human decision
// is never approved (DispatchWith).
func Dispatch(ctx context.Context, r *Registry, req Request, who func() Caller) Response {
	return DispatchWith(ctx, r, req, Env{Who: who})
}

// DispatchWith runs one request. env.Who is asked only for a command that
// acts, before it runs: an agent's, a detached or an unknown caller's is
// refused. A human decision (Command.Human) is then asked in Flint with
// env.Confirm, whoever the caller is, and runs only after a yes, if what it
// decides on did not change meanwhile.
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
		select {
		case r.waiting <- struct{}{}:
			defer func() { <-r.waiting }()
		default:
			return Response{Stderr: fmt.Sprintf("flint: %d commands are waiting already: try again when one has ended\n", MaxWaiting), Exit: ExitFailed}
		}
	}
	if c.Kind == Acts {
		caller := Caller{Unknown: true, What: "nobody asked"}
		if env.Who != nil {
			caller = env.Who()
		}
		if refused(caller) {
			return Response{Stderr: Clean(RefuseText(caller)) + "\n", Exit: ExitFailed}
		}
		if c.Human {
			if resp, ok := decide(ctx, c, call, caller, env.Confirm); !ok {
				return resp
			}
		}
	}
	res, err := c.Run(ctx, call)
	return result(c, call, res, err)
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
