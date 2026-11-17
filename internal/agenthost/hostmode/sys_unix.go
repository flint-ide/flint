//go:build unix

package hostmode

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// hostLock is the held flock of host.lock. The kernel drops it when the
// process dies, so a crashed host never leaves a lock behind.
type hostLock struct{ f *os.File }

// acquireLock takes the exclusive lock without waiting. The lock file is
// created 0600 and never followed through a symlink. Losing returns
// ErrAlreadyRunning.
func acquireLock(path string) (*hostLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the lock file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("make the lock file private: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &hostLock{f}, nil
}

// release unlocks. The file stays: removing it would let a new host lock a
// new inode while an old one still holds the old.
func (l *hostLock) release() { _ = l.f.Close() }

// withUmask runs fn with the process umask set to mask. The umask is process
// wide, so this is for startup, before the host does anything concurrently.
func withUmask(mask int, fn func()) {
	old := syscall.Umask(mask)
	defer syscall.Umask(old)
	fn()
}

// ownerUID is the owner of a file, when the platform says.
func ownerUID(fi os.FileInfo) (uint32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// detach makes cmd a process that outlives its starter: a session of its own,
// so closing the starter's terminal sends it no SIGHUP.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
