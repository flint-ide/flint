// Package desktop is the thin layer over what the operating system's desktop
// offers outside the window: showing a file in the file manager and reading
// the system's light or dark preference. Each platform has its own file
// (desktop_darwin.go, desktop_linux.go); desktop_other.go answers on the rest
// (Windows, for now) with honest "not supported" errors and defaults.
package desktop

import (
	"fmt"
	"os/exec"
	"strings"
)

// run runs a command and turns a failure into an error with its output.
func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: %s", name, msg)
	}
	return nil
}
