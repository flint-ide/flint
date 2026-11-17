package terminal

import (
	"fmt"
	"regexp"
	"strings"
)

var envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// maxEnvValue bounds one variable's value.
const maxEnvValue = 1024

// CheckEnvVar says whether a caller may add name=value to a terminal's
// environment: names are plain upper-case identifiers and values single-line.
// It is the one rule for every way into a terminal (the web view's call and
// the background host's wire), so they cannot drift apart.
func CheckEnvVar(name, value string) error {
	if !envNameRe.MatchString(name) || strings.ContainsAny(value, "\x00\n\r") || len(value) > maxEnvValue {
		return fmt.Errorf("invalid terminal variable %q", name)
	}
	return nil
}

// CheckEnvEntry is CheckEnvVar for an entry as Options.Env holds it
// ("NAME=value").
func CheckEnvEntry(entry string) error {
	name, value, ok := strings.Cut(entry, "=")
	if !ok {
		return fmt.Errorf("invalid terminal variable %.64q: no =", entry)
	}
	return CheckEnvVar(name, value)
}
