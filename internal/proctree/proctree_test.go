//go:build unix

package proctree

import (
	"os/exec"
	"testing"
)

func TestKillGoneOrUnstarted(t *testing.T) {
	if err := Kill(nil); err != nil {
		t.Errorf("Kill(nil) = %v", err)
	}
	cmd := exec.Command("/usr/bin/true")
	Isolate(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// The group is gone (ESRCH): not an error.
	if err := Kill(cmd.Process); err != nil {
		t.Errorf("Kill of an exited group = %v", err)
	}
	if err := Terminate(cmd.Process); err != nil {
		t.Errorf("Terminate of an exited group = %v", err)
	}
}
