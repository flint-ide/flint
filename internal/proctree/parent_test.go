//go:build darwin || linux

package proctree

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParentOfThisProcess(t *testing.T) {
	p, err := Parent(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if p.PID != os.Getpid() || p.PPID != os.Getppid() || p.UID != uint32(os.Getuid()) {
		t.Errorf("Parent = %+v, want ppid %d uid %d", p, os.Getppid(), os.Getuid())
	}
	if sid, err := unix.Getsid(0); err != nil || p.SID != sid {
		t.Errorf("SID = %d, want %d (%v)", p.SID, sid, err)
	}
	if _, err := Parent(0); err == nil {
		t.Error("Parent(0) succeeded")
	}
}
