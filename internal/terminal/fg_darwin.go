//go:build darwin

package terminal

import "golang.org/x/sys/unix"

// foregroundGroup returns the process group in the foreground of the PTY: the
// shell's own while it waits at the prompt, a command's while one runs.
func foregroundGroup(ptyFd uintptr) (int, error) {
	return unix.IoctlGetInt(int(ptyFd), unix.TIOCGPGRP)
}

// processArgs returns the argv of pid (kern.procargs2: argc, the executable
// path, padding, then argv and the environment, all NUL-separated).
func processArgs(pid int) ([]string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	return parseProcArgs(raw), nil
}
