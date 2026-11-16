package sandbox

import (
	"errors"
	"fmt"
	"strings"
)

// DarwinProfile is the sandbox-exec profile (SBPL) for p. Its shape:
//
//   - everything that is not a file, a socket or starting a program stays
//     allowed (Mach services, sysctl, …): closing those needs testing against
//     every macOS release, and the files and the network are what matters;
//   - no file can be written except the policy's Write paths;
//   - the contents of every user's home, other volumes and the shared
//     temporary folders can't be read, except the extension's folder and the
//     policy's paths (file names and attributes stay visible, so path
//     lookups work); the system itself stays readable;
//   - no socket unless the policy allows the network or local sockets (the
//     two are not told apart here: Support.Gaps says so);
//   - only the extension's own files (and its interpreters) can be run,
//     unless the policy allows other programs;
//   - signals only reach the sandboxed processes themselves.
//
// Later rules win over earlier ones. Paths must be real paths: the kernel
// checks a file's resolved path.
func (p Policy) DarwinProfile() (string, error) {
	var b strings.Builder
	rule := func(action, operation, filter string, paths []string) error {
		if len(paths) == 0 {
			return nil
		}
		fmt.Fprintf(&b, "(%s %s", action, operation)
		for _, path := range paths {
			q, err := sbplString(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "\n  (%s %s)", filter, q)
		}
		b.WriteString(")\n")
		return nil
	}
	b.WriteString("(version 1)\n(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	if err := rule("allow", "file-write*", "subpath", p.Write); err != nil {
		return "", err
	}
	b.WriteString("(allow file-write-data\n  (literal \"/dev/null\")\n  (literal \"/dev/zero\")\n  (literal \"/dev/dtracehelper\"))\n")
	if err := rule("deny", "file-read-data", "subpath", p.Protected); err != nil {
		return "", err
	}
	readable := append(append([]string{p.ExtensionDir}, p.Read...), p.Write...)
	if err := rule("allow", "file-read-data", "subpath", readable); err != nil {
		return "", err
	}
	if !p.Network && !p.LocalSockets {
		b.WriteString("(deny network*)\n")
	}
	if !p.ExecAny {
		b.WriteString("(deny process-exec*)\n")
		if err := rule("allow", "process-exec*", "subpath", []string{p.ExtensionDir}); err != nil {
			return "", err
		}
		if err := rule("allow", "process-exec*", "literal", p.ExecFiles); err != nil {
			return "", err
		}
	}
	b.WriteString("(deny signal)\n(allow signal (target self))\n(allow signal (target same-sandbox))\n")
	return b.String(), nil
}

// sbplString quotes a path for a profile.
func sbplString(s string) (string, error) {
	if !strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("sandbox profile: %q is not an absolute path", s)
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			return "", errors.New("sandbox profile: a path contains control characters")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}
