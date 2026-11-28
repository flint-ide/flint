package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"flint/internal/cli"
	"flint/internal/redact"
)

// An agent the user runs outside Flint (Claude Code in their own terminal)
// may run a command of the flint command line that acts and lets agents ask
// (cli.AgentAsks), after the user's yes to that one call in the same native
// dialog Flint's acting MCP tools show (approveAction): "Allow this agent
// action?", Allow or Cancel (the default), which the web view cannot
// answer. There is no "always allow": like an acting tool call that came
// with no yes from the chat, every call is asked. Agents Flint started, and
// the user's decisions (approving a step, continuing past a failed one),
// never get here: cli.DispatchWith refuses them first.

// agentCommandTimeout is how long an agent's command waits for the user's
// answer: as long as an acting MCP tool call does (mcpserver's approval
// timeout).
const agentCommandTimeout = 5 * time.Minute

// errAgentHandedWork refuses an agent's command from inside handed-out work.
var errAgentHandedWork = errors.New("an agent that was handed work cannot act through the command line: " +
	"this command came from an agent task's worktree (an agent task's or a workflow step's work). Run it from a workspace folder")

// confirmAgentCLI is cli.Env.ConfirmAgent: it refuses a command sent
// from inside handed-out work or for a folder in Restricted Mode, then asks
// the user. nil is a yes.
func (a *App) confirmAgentCLI(ctx context.Context, r cli.AgentRequest) error {
	if err := a.agentCommandPlace(r); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, agentCommandTimeout)
	defer cancel()
	// As approveAction: a question still queued behind another one when the
	// time is up is never shown, and one on screen stays until answered,
	// with its answer ignored.
	answer := make(chan bool, 1)
	go func() {
		yes, _ := a.askUserCtx(ctx, "Allow this agent action?", agentCommandQuestion(r), "Allow")
		answer <- yes
	}()
	select {
	case ok := <-answer:
		switch {
		case ctx.Err() != nil:
			return errNotAnsweredInTime
		case ok:
			return nil
		}
		return errors.New(cli.NotApproved)
	case <-ctx.Done():
		return errNotAnsweredInTime
	}
}

// agentCommandPlace refuses a command an agent sent from an agent task's
// worktree (the work of an agent task or a workflow step: handed-out work
// never hands out or steers work), or for a folder in Restricted Mode.
func (a *App) agentCommandPlace(r cli.AgentRequest) error {
	for _, dir := range []string{r.Cwd, r.Folder} {
		if dir != "" && a.inTaskWorktree(dir) {
			return errAgentHandedWork
		}
	}
	if r.Folder != "" && !a.isTrusted(r.Folder) {
		return fmt.Errorf("%s is in Restricted Mode in Flint: agents cannot act there", r.Folder)
	}
	return nil
}

// inTaskWorktree reports whether dir is an agent task's worktree or inside
// one.
func (a *App) inTaskWorktree(dir string) bool {
	dir = cleanFolder(dir)
	for _, tasks := range a.taskStore() {
		for _, t := range tasks {
			if t.Worktree == "" {
				continue
			}
			wt := filepath.Clean(t.Worktree)
			if dir == wt || strings.HasPrefix(dir, wt+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// agentCommandQuestion is the dialog's text: which agent, where, the
// command, what it does and its arguments (secrets redacted, bounded).
func agentCommandQuestion(r cli.AgentRequest) string {
	where := r.Folder
	if where == "" {
		where = r.Cwd
	}
	who := cli.AgentLabel(r.Caller)
	who = strings.ToUpper(who[:1]) + who[1:]
	var b strings.Builder
	fmt.Fprintf(&b, "%s working in %s wants to run Flint's command \"flint %s\" (process %d, outside Flint).\n", who, where, r.Path, r.Caller.PID)
	if r.Summary != "" {
		b.WriteString("\n" + r.Summary + ".\n")
	}
	line := redact.Secrets(agentCLILine(r))
	if len(line) > maxDialogArgs {
		line = strings.ToValidUTF8(line[:maxDialogArgs], "") + "…"
	}
	fmt.Fprintf(&b, "\nCommand: %s\n\nAllow it this once?", line)
	return b.String()
}

// agentCLILine is the command as the agent wrote it, with the text of each
// --<flag>-file in place of the file, quoted where a word needs it.
func agentCLILine(r cli.AgentRequest) string {
	words := append([]string{"flint"}, strings.Fields(r.Path)...)
	for _, w := range r.Args {
		words = append(words, quoteWord(w))
	}
	names := make([]string, 0, len(r.Files))
	for n := range r.Files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		words = append(words, "--"+n, quoteWord(r.Files[n]))
	}
	return cli.Clean(strings.Join(words, " "))
}

func quoteWord(w string) string {
	if w != "" && utf8.ValidString(w) && !strings.ContainsFunc(w, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`"'\$`+"`", r)
	}) {
		return w
	}
	return strconv.Quote(w)
}
