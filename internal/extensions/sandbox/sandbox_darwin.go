package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// sandboxExec is the system's sandbox launcher. Apple has marked it
// deprecated since macOS 10.12 without removing it or offering a
// replacement for programs that aren't app bundles.
const sandboxExec = "/usr/bin/sandbox-exec"

var probed = sync.OnceValue(probe)

// Probe reports whether sandbox-exec is there, works from this process (it
// doesn't from inside another sandbox) and accepts a profile of the shape
// Flint generates: it runs /usr/bin/true once under a strict profile. A
// macOS release that refuses the profile thus means "no sandbox", said to
// the user, rather than extensions that can't start.
func Probe() Support { return probed() }

func probe() Support {
	if info, err := os.Stat(sandboxExec); err != nil || !info.Mode().IsRegular() {
		return Support{Reason: sandboxExec + " is missing"}
	}
	const probeProgram = "/usr/bin/true"
	home, _ := os.UserHomeDir()
	profile, err := Policy{
		GOOS: "darwin", ExtensionDir: "/var/empty", Write: []string{"/var/empty"},
		ExecFiles: []string{probeProgram}, Protected: darwinClosed(home),
	}.DarwinProfile()
	if err != nil {
		return Support{Reason: err.Error()}
	}
	out, err := exec.Command(sandboxExec, "-p", profile, probeProgram).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return Support{Reason: "sandbox-exec doesn't work here: " + msg}
	}
	return Support{Available: true, Mechanism: "sandbox-exec", Gaps: []string{
		"Internet access and local sockets are allowed or blocked together: an extension that declares one gets both.",
	}}
}

// wrap: the launcher sets the limits, then sandbox-exec applies the profile
// and runs argv.
func wrap(p Policy, _ Support, argv []string) (launchSpec, error) {
	profile, err := p.DarwinProfile()
	if err != nil {
		return launchSpec{}, err
	}
	return launchSpec{Argv: append([]string{sandboxExec, "-p", profile}, argv...)}, nil
}

func launch(spec launchSpec, env []string) error {
	if spec.Argv[0] != sandboxExec {
		return errors.New("not a sandbox-exec command")
	}
	if err := setLimits(spec.OpenFiles); err != nil {
		return fmt.Errorf("resource limits: %w", err)
	}
	return fmt.Errorf("%s: %w", spec.Argv[0], syscall.Exec(spec.Argv[0], spec.Argv, env))
}

func launcherPath() (string, error) { return os.Executable() }
