//go:build darwin

package proctree

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Parent describes pid: its parent's pid, its real uid and whether it has
// a controlling terminal (kern.proc.pid, readable for every process of the
// system), and its session (getsid).
func Parent(pid int) (Proc, error) {
	if pid <= 0 {
		return Proc{}, fmt.Errorf("no process %d", pid)
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Proc{}, fmt.Errorf("process %d: %w", pid, err)
	}
	if int(kp.Proc.P_pid) != pid {
		return Proc{}, fmt.Errorf("no process %d", pid)
	}
	// Tdev is NODEV (-1) without a controlling terminal; getsid works for
	// any process.
	sid, _ := unix.Getsid(pid)
	return Proc{PID: pid, PPID: int(kp.Eproc.Ppid), UID: kp.Eproc.Pcred.P_ruid, SID: sid, TTY: kp.Eproc.Tdev != -1 && kp.Eproc.Tdev != 0}, nil
}
