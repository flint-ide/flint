//go:build darwin

package clisock

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerPID is the pid of the process that connected (LOCAL_PEERPID, recorded
// by the kernel at connect time).
func peerPID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var serr error
	if err := raw.Control(func(fd uintptr) {
		pid, serr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, fmt.Errorf("LOCAL_PEERPID: %w", serr)
	}
	return pid, nil
}
