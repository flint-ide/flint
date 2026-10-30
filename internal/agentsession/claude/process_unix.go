//go:build unix

package claude

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// isolateProcessTree puts the CLI in its own process group so the whole
// tree (the CLI, the shells and MCP servers it spawns) can be signalled at once.
func isolateProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateTree asks the CLI's process group to exit (SIGTERM).
func terminateTree(p *os.Process) error { return signalGroup(p, syscall.SIGTERM) }

// killTree force-kills the CLI's process group (SIGKILL).
func killTree(p *os.Process) error { return signalGroup(p, syscall.SIGKILL) }

func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
