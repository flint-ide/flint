//go:build linux

package proctree

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Parent describes pid: its parent's pid (/proc/<pid>/stat) and the uid
// that owns /proc/<pid> (its real uid).
func Parent(pid int) (Proc, error) {
	if pid <= 0 {
		return Proc{}, fmt.Errorf("no process %d", pid)
	}
	dir := "/proc/" + strconv.Itoa(pid)
	raw, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return Proc{}, fmt.Errorf("process %d: %w", pid, err)
	}
	st, err := parseStat(string(raw))
	if err != nil {
		return Proc{}, fmt.Errorf("process %d: %w", pid, err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return Proc{}, fmt.Errorf("process %d: %w", pid, err)
	}
	owner, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Proc{}, fmt.Errorf("process %d: no owner", pid)
	}
	return Proc{PID: pid, PPID: st.ppid, UID: owner.Uid, SID: st.sid, TTY: st.tty != 0}, nil
}

// stat is what Parent reads of /proc/<pid>/stat.
type stat struct{ ppid, sid, tty int }

// parseStat reads the parent's pid, the session and the controlling
// terminal's device (0: none) from /proc/<pid>/stat.
func parseStat(s string) (stat, error) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return stat{}, fmt.Errorf("malformed stat")
	}
	f := strings.Fields(s[i+1:]) // state, ppid, pgrp, session, tty_nr, …
	if len(f) < 5 {
		return stat{}, fmt.Errorf("malformed stat")
	}
	var out stat
	var err error
	for j, dst := range map[int]*int{1: &out.ppid, 3: &out.sid, 4: &out.tty} {
		if *dst, err = strconv.Atoi(f[j]); err != nil {
			return stat{}, fmt.Errorf("malformed stat")
		}
	}
	return out, nil
}

// parseStatPPID reads the parent's pid from /proc/<pid>/stat: the fields
// after the command, which is in parentheses and may contain anything,
// spaces and parentheses included, so the last ")" ends it.
func parseStatPPID(stat string) (int, error) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, fmt.Errorf("malformed stat")
	}
	f := strings.Fields(stat[i+1:]) // state, ppid, …
	if len(f) < 2 {
		return 0, fmt.Errorf("malformed stat")
	}
	return strconv.Atoi(f[1])
}
