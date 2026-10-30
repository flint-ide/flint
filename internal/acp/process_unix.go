//go:build unix

package acp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// isolateProcessTree puts the agent in its own process group so the whole
// tree (node, the CLI it spawns, MCP servers, ...) can be signalled at once.
func isolateProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateTree asks the agent's process group to exit (SIGTERM).
func terminateTree(p *os.Process) error { return signalGroup(p, syscall.SIGTERM) }

// killTree force-kills the agent's process group (SIGKILL).
func killTree(p *os.Process) error { return signalGroup(p, syscall.SIGKILL) }

func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
