//go:build !darwin

package terminal

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func foregroundGroup(ptyFd uintptr) (int, error) {
	return unix.IoctlGetInt(int(ptyFd), unix.TIOCGPGRP)
}

// processArgs returns the argv of pid (Linux: /proc/<pid>/cmdline).
func processArgs(pid int) ([]string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00"), nil
}
