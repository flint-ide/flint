package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"flint/internal/agentsession/acpdriver"
	"flint/internal/settings"
)

// probeTimeout bounds the "Check" of a custom agent: a node-based CLI can
// take several seconds to start. An agent that misses it is killed at once,
// so the call returns within moments of it, also below the 30 s write
// timeout of the server build's HTTP bindings.
const probeTimeout = 25 * time.Second

// ProbeCustomAgent starts a saved custom agent, runs the ACP `initialize`
// handshake and stops it, so Settings can show whether the command speaks
// ACP (and the agent's name and version). Only agents already in settings
// can be checked, and a command line not confirmed yet this session runs
// only after the user confirms it in a native dialog (confirmAgentCommand).
// The agent starts in an empty temporary folder, not in a project, so no
// project configuration is read; no session is opened.
func (s *SettingsAPI) ProbeCustomAgent(id string) (acpdriver.AgentInfo, error) {
	c, ok := s.app.currentSettings().CustomAgent(id)
	if !ok {
		return acpdriver.AgentInfo{}, fmt.Errorf("no custom agent %q in settings (save it first)", id)
	}
	if err := s.app.confirmAgentCommand(c); err != nil {
		return acpdriver.AgentInfo{}, err
	}
	dir, err := os.MkdirTemp("", "flint-acp-check-")
	if err != nil {
		return acpdriver.AgentInfo{}, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return acpdriver.Probe(ctx, customAgentOptions(c, nil), dir)
}

// errNotConfirmed: the user declined to run a custom agent's command.
var errNotConfirmed = errors.New("check cancelled: the command was not confirmed")

// confirmAgentCommand asks the user, in a native dialog that shows the exact
// command line, before a Check runs it for the first time this session.
// Settings are saved by the web view, which renders untrusted content, so
// an agent being in settings proves nothing: without this, injected code
// could save {"command": "/bin/sh", "args": ["-c", ...]} and run it with a
// Check, in no project and with no trust needed.
func (a *App) confirmAgentCommand(c settings.CustomAgent) error {
	key := strings.Join(append([]string{c.Command}, c.Args...), "\x00")
	a.confirmMu.Lock()
	defer a.confirmMu.Unlock()
	if a.confirmed[key] {
		return nil
	}
	ask := a.ask
	if ask == nil {
		ask = a.askNative
	}
	msg := "Flint will run this command to check that it speaks the Agent Client Protocol:\n\n" +
		commandLine(c.Command, c.Args) +
		"\n\nIt starts in an empty temporary folder and is stopped after the handshake. Continue only if you added this agent yourself."
	if !ask(fmt.Sprintf("Run the custom agent %q?", c.ID), msg, "Run Check") {
		return errNotConfirmed
	}
	if a.confirmed == nil {
		a.confirmed = map[string]bool{}
	}
	a.confirmed[key] = true
	return nil
}

// commandLine writes a command and its arguments the way a shell would
// read them, so the dialog shows exactly what runs: arguments with spaces
// or shell characters are single-quoted, anything not plain printable
// ASCII is shown escaped.
func commandLine(command string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	for _, a := range append([]string{command}, args...) {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	plain := func(r rune) bool {
		return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_./=:,+@%", r))
	}
	switch {
	case s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !plain(r) }):
		return s
	case strings.ContainsFunc(s, func(r rune) bool { return r >= utf8.RuneSelf || !unicode.IsPrint(r) }):
		return strconv.QuoteToASCII(s)
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
