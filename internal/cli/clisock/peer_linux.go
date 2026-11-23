//go:build linux

package clisock

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerPID is the pid of the process that connected (SO_PEERCRED, recorded by
// the kernel at connect time).
func peerPID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, fmt.Errorf("SO_PEERCRED: %w", serr)
	}
	return int(cred.Pid), nil
}
