package hostmode

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
)

var (
	// ErrNotRunning says there is no host to talk to: no run directory, no
	// token or nobody listening. The caller may start one (Spawn).
	ErrNotRunning = errors.New("no background host is running")
	// ErrAlreadyRunning is what a host that lost the lock returns; the
	// process exits with ExitAlreadyRunning.
	ErrAlreadyRunning = errors.New("another background host is already running for this configuration")
)

const (
	// tokenBytes is the token's size: 256 bits.
	tokenBytes = 32
	// maxTokenFile bounds what Dial reads from host.token, which it does not
	// control: a legitimate one is 64 bytes.
	maxTokenFile = 256
)

// prepareDir creates the run directory as 0700 and insists that it is ours: a
// directory in a shared place (the /tmp fallback) that somebody else made
// first, or a symlink to somewhere, is refused instead of used.
func prepareDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create the run directory: %w", err)
	}
	fi, err := checkRunDir(dir)
	if err != nil {
		return err
	}
	if fi.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("make the run directory private: %w", err)
		}
	}
	return nil
}

// checkRunDir returns the run directory's info after checking that it is a
// real directory (not a symlink) that belongs to us. A missing one is
// ErrNotRunning.
func checkRunDir(dir string) (fs.FileInfo, error) {
	fi, err := os.Lstat(dir) // Lstat: a symlink is not a directory of ours
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w (no %s)", ErrNotRunning, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("check the run directory: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	if uid, ok := ownerUID(fi); ok && uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("%s belongs to user %d, not to us", dir, uid)
	}
	return fi, nil
}

// writeToken creates host.token with a fresh random token, mode 0600, and
// returns the token. The caller holds the lock, so a file already there is a
// dead host's: it is removed (a symlink is removed itself, never followed) and
// the new one is created with O_EXCL, which refuses to open anything that
// appeared in between, a planted symlink included.
func writeToken(path string) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("remove the old token: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create the token file: %w", err)
	}
	_, err = f.WriteString(token)
	if err == nil {
		err = f.Chmod(0o600) // whatever the umask said
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write the token: %w", err)
	}
	return token, nil
}

// readToken reads the token of the running host, after checking that the file
// is the private regular file the host made: a token that others can read, or
// one that is not ours, is not a secret.
func readToken(path string) (string, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w (no %s)", ErrNotRunning, tokenName)
	}
	if err != nil {
		return "", fmt.Errorf("check the token file: %w", err)
	}
	switch {
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("%s is not a regular file", path)
	case fi.Mode().Perm()&0o077 != 0:
		return "", fmt.Errorf("%s is accessible to others (mode %v)", path, fi.Mode().Perm())
	case fi.Size() > maxTokenFile:
		return "", fmt.Errorf("%s is %d bytes, over %d", path, fi.Size(), maxTokenFile)
	}
	if uid, ok := ownerUID(fi); ok && uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("%s belongs to user %d, not to us", path, uid)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read the token: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTokenFile+1))
	if err != nil || len(data) > maxTokenFile {
		return "", fmt.Errorf("read the token: %v", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// removeStaleSocket deletes a socket left by a host that died. Only the lock
// holder may call it: with the lock held nobody else is listening, so the
// socket is dead. Anything that is not a socket is an error, not deleted.
func removeStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket", path)
	}
	return os.Remove(path)
}

// listen makes the host's socket, readable and writable by us only. The umask
// is tightened around the bind so the socket never exists with wider rights,
// not even for an instant; the run directory is 0700 as well.
func listen(path string) (*net.UnixListener, error) {
	if err := removeStaleSocket(path); err != nil {
		return nil, fmt.Errorf("remove the stale socket: %w", err)
	}
	var ln *net.UnixListener
	var err error
	withUmask(0o177, func() {
		ln, err = net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	})
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("make the socket private: %w", err)
	}
	return ln, nil
}
