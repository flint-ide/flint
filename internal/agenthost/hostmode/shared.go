package hostmode

import "net"

// The run directory serves a second socket: the command line's
// (internal/cli/clisock), which the window listens on. These are the host's
// own rules for the directory, the lock, the token and the socket, exported
// so both sockets keep to the same ones.

// PeerCredSupported is whether this platform can tell the uid of a socket's
// peer.
const PeerCredSupported = peerCredSupported

// PrepareRunDir creates dir as 0700 and refuses one that is a symlink or
// another user's.
func PrepareRunDir(dir string) error { return prepareDir(dir) }

// AcquireLock takes the exclusive flock of path without waiting
// (ErrAlreadyRunning when another process holds it); release drops it.
func AcquireLock(path string) (release func(), err error) {
	l, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	return l.release, nil
}

// WriteToken replaces path with a fresh 256-bit random token (0600, O_EXCL)
// and returns it. The caller holds the lock.
func WriteToken(path string) (string, error) { return writeToken(path) }

// ReadToken reads a token file after checking it is a private regular file
// of ours; a missing file is ErrNotRunning.
func ReadToken(path string) (string, error) { return readToken(path) }

// Listen replaces a stale socket at path (the caller holds the lock) and
// listens there with mode 0600.
func Listen(path string) (*net.UnixListener, error) { return listen(path) }

// PeerUID is the uid of the process at the other end of c.
func PeerUID(c *net.UnixConn) (uint32, error) { return peerUID(c) }
