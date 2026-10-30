//go:build !unix

package claude

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

// isolateProcessTree is a no-op outside unix; trees are killed via taskkill.
func isolateProcessTree(*exec.Cmd) {}

// terminateTree kills the CLI tree; there is no portable graceful signal.
func terminateTree(p *os.Process) error { return killTree(p) }

// killTree force-kills the CLI and, on Windows, its descendants.
func killTree(p *os.Process) error {
	if runtime.GOOS == "windows" {
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run(); err == nil {
			return nil
		}
	}
	err := p.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
