//go:build !linux && !darwin

package sandbox

import (
	"errors"
	"os"
	"runtime"
)

// Probe: no sandbox on this system.
func Probe() Support {
	return Support{Reason: "Flint has no sandbox for " + runtime.GOOS}
}

func wrap(Policy, Support, []string) (launchSpec, error) {
	return launchSpec{}, errors.New("no sandbox on " + runtime.GOOS)
}

func launch(launchSpec, []string) error { return errors.New("no sandbox on " + runtime.GOOS) }

func launcherPath() (string, error) { return os.Executable() }
