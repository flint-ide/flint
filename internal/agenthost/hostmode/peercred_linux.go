package hostmode

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerCredSupported is whether this platform can tell who is on the other
// end of a Unix socket. Without it the host refuses to start: the token alone
// would then be the only protection against another user.
const peerCredSupported = true

// peerUID is the uid of the process that connected, from SO_PEERCRED: the
// kernel records the credentials of the peer at connect time, so the peer
// cannot claim to be someone else.
func peerUID(c *net.UnixConn) (uint32, error) {
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
	return cred.Uid, nil
}
