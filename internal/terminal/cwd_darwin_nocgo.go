//go:build darwin && !cgo

package terminal

import "errors"

// processCwd without cgo (the headless host, cmd/flint-host, is built with
// CGO_ENABLED=0): macOS exposes a process's directory only through libproc,
// which needs cgo, so the live directory is unknown and callers fall back to
// the one the shell last reported.
func processCwd(int) (string, error) {
	return "", errors.New("the working directory of a process needs cgo on macOS")
}
