package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// run.sandbox: what the program needs from the machine beyond the strict
// profile Flint confines it to (internal/extensions/sandbox): its own folder
// and the system's programs and libraries to read, its data folder and a
// private temporary folder to write, no network, no other programs.
//
//	"run": { "command": "bin/ext", "sandbox": { "read": ["~/.kube"], "network": true, "exec": true } }
//
// network opens the Internet, localSockets the Unix sockets of services on
// this machine, exec lets it start other programs (confined like itself).
//
// A manifest without the object runs unconfined, as before sandboxing
// existed, and Flint says so wherever it shows the extension; "sandbox": {}
// asks for the strict profile; "mode": "none" declares that the program
// can't work confined.

// Sandbox modes.
const (
	SandboxStrict = "strict" // the default of a declared sandbox
	SandboxNone   = "none"   // the extension declares it runs unconfined
)

// Limits of the sandbox section.
const (
	maxSandboxPaths   = 32
	maxSandboxPathLen = 512
)

// Sandbox is run.sandbox.
type Sandbox struct {
	// Mode is "strict" (also when empty) or "none".
	Mode string `json:"mode,omitempty"`
	// Read are more files and folders the program may read; Write the ones
	// it may also change. See checkSandboxPath for how they are written.
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
	// Network: the program talks to the Internet (IPv4 and IPv6 sockets).
	// Declaring a network: permission implies it.
	Network bool `json:"network,omitempty"`
	// LocalSockets: the program connects to services on this machine through
	// Unix sockets (the Docker socket, the desktop's message bus).
	LocalSockets bool `json:"localSockets,omitempty"`
	// Exec: the program starts other programs than its own.
	Exec bool `json:"exec,omitempty"`
}

// UnmarshalJSON refuses fields this Flint doesn't know: an extension must
// never count on a restriction, or be granted an access, that Flint skipped.
func (s *Sandbox) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.New("run.sandbox must be an object")
	}
	if raw == nil {
		return errors.New("run.sandbox must be an object")
	}
	known := []string{"exec", "localSockets", "mode", "network", "read", "write"}
	for _, key := range sortedKeys(raw) {
		if !slices.Contains(known, key) {
			return fmt.Errorf("run.sandbox: unknown field %q (known: %s)", truncate(key, 40), strings.Join(known, ", "))
		}
	}
	type plain Sandbox
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("run.sandbox: %w", err)
	}
	*s = Sandbox(p)
	return nil
}

// Unconfined: the extension declares that its program runs without a sandbox.
func (s *Sandbox) Unconfined() bool { return s != nil && s.Mode == SandboxNone }

// sandboxVars are the variables a sandbox path may start with; each has a
// fixed meaning (internal/extensions/sandbox expands them).
var sandboxVars = []string{"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "XDG_STATE_HOME"}

var sandboxEnvRef = regexp.MustCompile(`^\$\{env:([A-Z][A-Z0-9_]{0,63})\}$`)

func (m *Manifest) checkSandbox() error {
	s := m.Run.Sandbox
	if s == nil {
		return nil
	}
	switch s.Mode {
	case "", SandboxStrict:
	case SandboxNone:
		if len(s.Read)+len(s.Write) > 0 || s.Network || s.LocalSockets || s.Exec {
			return errors.New(`run.sandbox: "mode": "none" runs the program unconfined; remove read, write, network, localSockets and exec`)
		}
		return nil
	default:
		return fmt.Errorf(`run.sandbox: mode %q is not "strict" or "none"`, truncate(s.Mode, 40))
	}
	for _, list := range []struct {
		name  string
		paths []string
	}{{"read", s.Read}, {"write", s.Write}} {
		if len(list.paths) > maxSandboxPaths {
			return fmt.Errorf("run.sandbox.%s: at most %d paths", list.name, maxSandboxPaths)
		}
		seen := map[string]bool{}
		for _, p := range list.paths {
			if err := checkSandboxPath(p, m.Run.Env); err != nil {
				return fmt.Errorf("run.sandbox.%s: %w", list.name, err)
			}
			if seen[p] {
				return fmt.Errorf("run.sandbox.%s: %q is listed twice", list.name, p)
			}
			seen[p] = true
		}
	}
	return nil
}

// checkSandboxPath: one of
//
//	/abs/path          an absolute path
//	~/path             inside the user's home folder
//	${XDG_…}/path      inside an XDG base folder (sandboxVars), or the folder itself
//	${env:NAME}        the files a run.env variable names (a path, or a list
//	                   separated like PATH), e.g. ${env:KUBECONFIG}
//
// written clean: no "..", ".", doubled or trailing slashes, no wildcards. The
// root and the home folder themselves can't be named.
func checkSandboxPath(p string, runEnv []string) error {
	switch {
	case p == "":
		return errors.New("an empty path")
	case len(p) > maxSandboxPathLen || strings.ContainsFunc(p, unicode.IsControl):
		// Paths are shown in the install dialog: no newlines to fake text there.
		return fmt.Errorf("%q: too long or contains control characters", truncate(p, 40))
	case strings.ContainsAny(p, `*?[\`):
		return fmt.Errorf("%q: wildcards and backslashes are not supported; name each file or folder", p)
	}
	if ref := sandboxEnvRef.FindStringSubmatch(p); ref != nil {
		if !slices.Contains(runEnv, ref[1]) {
			return fmt.Errorf("%q: %s is not listed in run.env", p, ref[1])
		}
		return nil
	}
	rest := p
	switch {
	case strings.HasPrefix(p, "/"):
	case strings.HasPrefix(p, "~/"):
		rest = p[1:]
	case strings.HasPrefix(p, "${"):
		name, after, ok := strings.Cut(p[2:], "}")
		if !ok || !slices.Contains(sandboxVars, name) {
			return fmt.Errorf("%q: unknown variable (known: ~, %s, and ${env:NAME} for a run.env variable)", p, "${"+strings.Join(sandboxVars, "}, ${")+"}")
		}
		if after == "" {
			return nil // the base folder itself
		}
		if !strings.HasPrefix(after, "/") {
			return fmt.Errorf("%q: write ${%s}/…", p, name)
		}
		rest = after
	default:
		return fmt.Errorf("%q: start with /, ~/ or a variable such as ${XDG_CONFIG_HOME}", p)
	}
	if strings.Contains(rest, "$") {
		return fmt.Errorf("%q: variables are only expanded at the start", p)
	}
	if rest == "/" || path.Clean(rest) != rest {
		return fmt.Errorf("%q: use a clean path below the root or the home folder (no .., no trailing slash)", p)
	}
	return nil
}

// SandboxState says how the manifest wants its program confined.
type SandboxState int

// The states of a manifest's sandbox section.
const (
	// SandboxNoProgram: nothing runs.
	SandboxNoProgram SandboxState = iota
	// SandboxUndeclared: no run.sandbox; the program runs unconfined as
	// extensions did before sandboxing, and Flint warns.
	SandboxUndeclared
	// SandboxDeclaredNone: "mode": "none".
	SandboxDeclaredNone
	// SandboxDeclared: the strict profile plus what the section adds.
	SandboxDeclared
)

// SandboxState reports which of the states the manifest is in.
func (m *Manifest) SandboxState() SandboxState {
	switch {
	case !m.HasProgram():
		return SandboxNoProgram
	case m.Run.Sandbox == nil:
		return SandboxUndeclared
	case m.Run.Sandbox.Unconfined():
		return SandboxDeclaredNone
	}
	return SandboxDeclared
}
