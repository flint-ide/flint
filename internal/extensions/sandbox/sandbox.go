// Package sandbox confines an extension's process with what the operating
// system offers, without cgo and without a helper binary
// (docs/design/extensions.md, "Sandbox"):
//
//   - Linux: Landlock (files; TCP on kernels whose Landlock has network
//     rules; signals and abstract sockets on newer ones), a seccomp filter
//     that refuses new sockets, and no_new_privs. Go can't run code between
//     fork and exec, so Flint starts itself in a launcher mode (Main) that
//     applies the restrictions to itself and then execs the program.
//   - macOS: sandbox-exec with a generated profile. Apple marks sandbox-exec
//     deprecated; it is still what the system's own tools use.
//   - anywhere else, or when the kernel lacks the mechanism: nothing, and
//     Probe says so, so Flint can tell the user instead of pretending.
//
// The package is split so that everything that decides (Build, LinuxRules,
// DarwinProfile, the seccomp program) is a pure function tested on every
// system, and only applying the result is per system (sandbox_linux.go,
// sandbox_darwin.go, sandbox_other.go).
package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
)

// How Flint starts itself as the launcher: the argument, and the variable
// that carries what to apply and run (removed before the program starts).
const (
	LaunchArg = "--flint-sandbox-launch"
	launchEnv = "FLINT_SANDBOX_LAUNCH"
)

// MaxOpenFiles is the open-file limit of a sandboxed process. A security
// limit, not a setting.
const MaxOpenFiles = 4096

// Support is what this system can enforce.
type Support struct {
	// Available: a sandbox can be applied at all. When false, Reason says why.
	Available bool
	Reason    string
	// Mechanism names what enforces it, e.g. "Landlock (ABI 7) and seccomp".
	Mechanism string
	// Gaps are protections of the full profile this system lacks, as
	// sentences for the user (an older kernel).
	Gaps []string

	// landlockABI and seccomp are what the Linux launcher may use.
	landlockABI int
	seccomp     bool
}

// launchSpec is what the launcher applies and runs.
type launchSpec struct {
	// Argv is the program to exec and its arguments (on macOS: sandbox-exec
	// with the profile, then the program).
	Argv []string `json:"argv"`
	// Linux are the rules to apply first; nil on macOS.
	Linux *LinuxRules `json:"linux,omitempty"`
	// OpenFiles caps RLIMIT_NOFILE; core dumps are always turned off.
	OpenFiles uint64 `json:"openFiles"`
}

// Command returns how to start argv (an absolute program path and its
// arguments) confined by p: the command line to run instead and variables to
// add to its environment. launcher is Flint's own executable (or any program
// whose main calls Main first).
func Command(launcher string, p Policy, s Support, argv []string) (args []string, env []string, err error) {
	if !s.Available {
		return nil, nil, errors.New("no sandbox on this system: " + s.Reason)
	}
	if len(argv) == 0 || launcher == "" {
		return nil, nil, errors.New("sandbox: nothing to run")
	}
	spec, err := wrap(p, s, argv)
	if err != nil {
		return nil, nil, err
	}
	spec.OpenFiles = MaxOpenFiles
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, err
	}
	return []string{launcher, LaunchArg}, []string{launchEnv + "=" + string(data)}, nil
}

// Main turns the process into the sandbox launcher when it was started as
// one, and never returns then; otherwise it returns at once. It must be the
// first call of main, before anything starts threads that matter or reads
// files: the launcher restricts itself and execs the program in its place.
func Main() {
	if len(os.Args) != 2 || os.Args[1] != LaunchArg {
		return
	}
	// Landlock, seccomp and no_new_privs apply to the calling thread, and
	// exec keeps that thread's.
	runtime.LockOSThread()
	err := runLauncher()
	fmt.Fprintln(os.Stderr, "flint: sandbox launcher: "+err.Error())
	os.Exit(126)
}

func runLauncher() error {
	raw, ok := os.LookupEnv(launchEnv)
	if !ok {
		return errors.New(launchEnv + " is not set")
	}
	var spec launchSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return err
	}
	if len(spec.Argv) == 0 {
		return errors.New("nothing to run")
	}
	_ = os.Unsetenv(launchEnv)
	return launch(spec, os.Environ())
}

// Launcher is the program Command should start as the launcher when the
// caller has none of its own: this process's executable.
func Launcher() (string, error) { return launcherPath() }
