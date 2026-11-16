//go:build linux || darwin

package sandbox

import "syscall"

// setLimits applies the resource limits of a sandboxed process to this one
// (they survive exec and are inherited by what it starts): no core dumps,
// which would write the process's memory to disk, and a bounded number of
// open files. Limits are only ever lowered.
//
// There is no memory limit: RLIMIT_AS counts address space, not memory, and
// stops runtimes that reserve large ranges (Go with the race detector, V8,
// the JVM) long before they use any; macOS doesn't enforce RLIMIT_RSS; and
// RLIMIT_NPROC counts all of the user's processes and threads, so a value
// low enough to stop a fork bomb would stop the extension's first thread on
// a busy desktop.
func setLimits(openFiles uint64) error {
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{}); err != nil {
		return err
	}
	if openFiles == 0 {
		return nil
	}
	var cur syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &cur); err != nil {
		return err
	}
	lim := syscall.Rlimit{Cur: min(uint64(cur.Cur), openFiles), Max: min(uint64(cur.Max), openFiles)}
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
