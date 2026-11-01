//go:build unix

package agent

import (
	"os/exec"
	"syscall"
)

// killTreeOnCancel runs the agent in its own process group and, when the run
// is cancelled, kills the whole group: the CLI and what it started (Claude
// Code's MCP servers, shells), which would otherwise outlive it.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
