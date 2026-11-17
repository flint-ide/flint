// Package remote runs terminals and agents on another machine by driving the
// user's own ssh (docs/design/agent-runtime.md, "Phase 4"). No SSH library is
// embedded: ssh's configuration, keys, agent, known_hosts and jump hosts all
// keep working, and Flint stores no credential.
//
// The scope is remote terminals and agents only. Files, editor, git, language
// servers and search stay local.
//
// This file builds the ssh command lines. Everything that came from the user
// (target, port, command) is checked again here, whatever checked it before,
// and goes after the "--" that ends ssh's options, so none of it can be taken
// for an option; ssh is started directly, never through a local shell.
package remote

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"flint/internal/settings"
)

// sshProgram is looked up on PATH each time a command is made (not when the
// package is built or loaded), so a newly installed ssh is found.
const sshProgram = "ssh"

// ErrNoSSH is returned when no ssh executable is on PATH.
var ErrNoSSH = errors.New("ssh was not found on PATH: install an OpenSSH client")

// The remote's own flags. The host relay runs "<command> --agent-host --stdio",
// the check "<command> --version".
const (
	agentHostFlag = "--agent-host"
	stdioFlag     = "--stdio"
	versionFlag   = "--version"
)

// sshOptions are fixed. -T: no pseudo-terminal, the stream carries bytes.
// -e none: no escape character, so no byte of the stream is special to ssh.
// BatchMode: never prompt (a password or a host-key question would otherwise
// wait on a terminal nobody sees); it fails with ssh's own message instead.
// The keep-alive options notice a dead network in about 45 s.
var sshOptions = []string{
	"-T", "-e", "none",
	"-o", "BatchMode=yes",
	"-o", "ConnectTimeout=10",
	"-o", "ServerAliveInterval=15",
	"-o", "ServerAliveCountMax=3",
}

// HostArgv is the argument vector (argv[0] is "ssh") that connects to the
// host of machine m:
//
//	ssh -T -e none -o BatchMode=yes … [-p <port>] -- <target> <command> --agent-host --stdio
//
// It returns an error for a machine that does not pass settings.CheckMachine;
// there is no way to get an argument list for an unchecked one.
func HostArgv(m settings.Machine) ([]string, error) {
	return argv(m, agentHostFlag, stdioFlag)
}

// CheckArgv is the argument vector of the Check command, which asks the remote
// Flint for its version over the same ssh options:
//
//	ssh … [-p <port>] -- <target> <command> --version
func CheckArgv(m settings.Machine) ([]string, error) {
	return argv(m, versionFlag)
}

func argv(m settings.Machine, remoteFlags ...string) ([]string, error) {
	if err := settings.CheckMachine(m); err != nil {
		return nil, fmt.Errorf("refusing to build an ssh command: %w", err)
	}
	out := make([]string, 0, len(sshOptions)+8+len(remoteFlags))
	out = append(out, sshProgram)
	out = append(out, sshOptions...)
	if m.Port != 0 {
		out = append(out, "-p", strconv.Itoa(m.Port))
	}
	// Everything the user supplied comes after "--".
	out = append(out, "--", m.Target, m.Command)
	return append(out, remoteFlags...), nil
}

// newCommand turns an argument vector from HostArgv or CheckArgv into a
// command that runs the ssh found on PATH now. The command is not started and
// has no shell: Args are passed to ssh as they are.
func newCommand(argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 || argv[0] != sshProgram {
		return nil, errors.New("not an ssh argument vector")
	}
	path, err := exec.LookPath(sshProgram)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNoSSH, err)
	}
	return exec.Command(path, argv[1:]...), nil
}
