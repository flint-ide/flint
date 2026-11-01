//go:build unix

// Package proctree runs a command in its own process group so the whole tree
// it starts (a CLI, the shells and MCP servers it spawns) can be signalled at
// once.
package proctree

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Isolate puts cmd (not started yet) in its own process group.
func Isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Kill force-kills p's process group (SIGKILL); a group that is already gone
// is not an error, nor is a process that never started.
func Kill(p *os.Process) error { return signal(p, syscall.SIGKILL) }

// Terminate asks p's process group to exit (SIGTERM).
func Terminate(p *os.Process) error { return signal(p, syscall.SIGTERM) }

func signal(p *os.Process, sig syscall.Signal) error {
	if p == nil {
		return nil
	}
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
