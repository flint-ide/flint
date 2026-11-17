//go:build !linux && !darwin

package hostmode

import (
	"errors"
	"net"
)

// peerCredSupported is false here: the host refuses to start rather than
// serve peers it cannot identify.
const peerCredSupported = false

func peerUID(*net.UnixConn) (uint32, error) {
	return 0, errors.New("peer credentials are not supported on this platform")
}
