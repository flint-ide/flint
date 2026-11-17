package agenthost

import (
	"fmt"
	"regexp"
	"strconv"
)

// VersionLine is what "flint --version" prints: the build version and the
// host protocol version, on one line. The machine Check of a remote Flint
// (internal/remote) parses it with ParseVersionLine.
func VersionLine(version string) string {
	if version == "" {
		version = "unknown"
	}
	return fmt.Sprintf("flint %s host-protocol %d", version, Protocol)
}

var versionLinePattern = regexp.MustCompile(`^flint (\S{1,128}) host-protocol ([0-9]{1,6})$`)

// ParseVersionLine reads a VersionLine; ok is false for anything else.
func ParseVersionLine(line string) (version string, protocol int, ok bool) {
	m := versionLinePattern.FindStringSubmatch(line)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}
