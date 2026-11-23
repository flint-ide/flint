//go:build !darwin && !linux

package clisock

import (
	"errors"
	"net"
)

// peerPID is not available here: every caller is unknown, so commands that
// act are refused (and the server does not start without peer uids anyway).
func peerPID(*net.UnixConn) (int, error) {
	return 0, errors.New("this system does not say which process connected")
}
