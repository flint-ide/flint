//go:build !unix

package hostmode

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("the background host is not supported on this platform")

type hostLock struct{}

func acquireLock(string) (*hostLock, error) { return nil, errUnsupported }

func (*hostLock) release() {}

func withUmask(_ int, fn func()) { fn() }

func ownerUID(os.FileInfo) (uint32, bool) { return 0, false }

func detach(*exec.Cmd) {}
