package settings

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Machine is a remote machine whose terminals and agents Flint runs over the
// user's own ssh (docs/design/agent-runtime.md, "Phase 4"). Every field ends
// up in the argument list of an ssh process, so each is checked against a
// strict pattern here and again by internal/remote before it builds a command
// line. Nothing secret is stored: ssh authenticates with the user's keys and
// agent, and BatchMode means it never asks for a password.
type Machine struct {
	// ID names the machine in tabs and routing ("<id>:<session>").
	ID string `json:"id"`
	// Name is shown in the UI; empty shows the ID.
	Name string `json:"name"`
	// Target is what ssh connects to: "host", "user@host" or an alias from
	// ~/.ssh/config.
	Target string `json:"target"`
	// Port is the ssh port; 0 leaves it to ssh (its config or 22).
	Port int `json:"port"`
	// Command is the Flint executable on the remote machine (default
	// "flint"): a name or a path, never with arguments.
	Command string `json:"command"`
}

// Limits and patterns of the machine registry. They are the design's
// validation rules.
const (
	MaxMachines        = 32
	MaxMachineNameLen  = 64
	DefaultMachineCmd  = "flint"
	machineIDHint      = "lowercase letters, digits and dashes, starting with a letter or digit, at most 32"
	machineTargetHint  = "host or user@host: letters, digits, '.', '_' and '-', not starting with '-'"
	machineCommandHint = "a file name or path made of letters, digits, '.', '_', '/', '~' and '-', not starting with '-'"
)

var (
	machineIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	machineTargetPattern  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,62}(@[A-Za-z0-9][A-Za-z0-9.-]{0,252})?$`)
	machineCommandPattern = regexp.MustCompile(`^[A-Za-z0-9_./~-]{1,256}$`)
)

// MachineIssue says why an entry of "machines" was dropped when settings.json
// was read, so the Settings UI can show it instead of the entry vanishing
// silently.
type MachineIssue struct {
	// Index is the entry's position in the file's list.
	Index int `json:"index"`
	// ID is the entry's id as written (it may be the invalid part).
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// CheckMachine reports why one machine would be refused. It is the only
// validation of a machine: internal/remote calls it again before building an
// ssh argument list.
func CheckMachine(m Machine) error {
	switch {
	case !machineIDPattern.MatchString(m.ID):
		return fmt.Errorf("id %q: use %s", clip(m.ID), machineIDHint)
	case !machineTargetPattern.MatchString(m.Target):
		return fmt.Errorf("target %q: use %s", clip(m.Target), machineTargetHint)
	case m.Port < 0 || m.Port > 65535:
		return fmt.Errorf("port %d out of range 1–65535 (0 leaves it to ssh)", m.Port)
	case !machineCommandPattern.MatchString(m.Command) || strings.HasPrefix(m.Command, "-"):
		return fmt.Errorf("command %q: use %s", clip(m.Command), machineCommandHint)
	}
	return checkMachineName(m.Name)
}

func checkMachineName(name string) error {
	switch {
	case !utf8.ValidString(name):
		return errors.New("name is not valid UTF-8")
	case utf8.RuneCountInString(name) > MaxMachineNameLen:
		return fmt.Errorf("name longer than %d characters", MaxMachineNameLen)
	case name != strings.TrimSpace(name) || strings.ContainsFunc(name, unicode.IsControl):
		return fmt.Errorf("invalid name %q", clip(name))
	}
	return nil
}

// clip bounds a rejected value echoed in an error message.
func clip(s string) string {
	if len(s) > 64 {
		return strings.ToValidUTF8(s[:64], "") + "…"
	}
	return s
}

// CheckMachines reports why a registry would be refused (as Validate does
// when saving): too many entries, an invalid one, or a repeated id.
func CheckMachines(list []Machine) error {
	if len(list) > MaxMachines {
		return fmt.Errorf("machines: too many (max %d)", MaxMachines)
	}
	seen := map[string]bool{}
	for _, m := range list {
		if err := CheckMachine(m); err != nil {
			return fmt.Errorf("machines: %w", err)
		}
		if seen[m.ID] {
			return fmt.Errorf("machines: id %q is used twice", m.ID)
		}
		seen[m.ID] = true
	}
	return nil
}

// NormalizeMachines keeps the valid entries and reports each dropped one. A
// broken entry is dropped whole: half a machine could connect somewhere else.
// An empty command is the default; the first entry of an id wins; entries
// past the limit are dropped.
func NormalizeMachines(list []Machine) ([]Machine, []MachineIssue) {
	out := []Machine{}
	var issues []MachineIssue
	seen := map[string]bool{}
	for i, m := range list {
		drop := func(reason string) {
			issues = append(issues, MachineIssue{Index: i, ID: clip(m.ID), Reason: reason})
		}
		if len(out) >= MaxMachines {
			drop(fmt.Sprintf("more than %d machines", MaxMachines))
			continue
		}
		if m.Command == "" {
			m.Command = DefaultMachineCmd
		}
		if err := CheckMachine(m); err != nil {
			drop(err.Error())
			continue
		}
		if seen[m.ID] {
			drop(fmt.Sprintf("id %q is used twice", clip(m.ID)))
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out, issues
}

// MachineIssues lists the entries of "machines" dropped when these settings
// were read. It is empty for settings that were not read from a file.
func (s Settings) MachineIssues() []MachineIssue {
	return append([]MachineIssue(nil), s.machineIssues...)
}

// Machine returns the registered machine with id.
func (s Settings) Machine(id string) (Machine, bool) {
	for _, m := range s.Machines {
		if m.ID == id {
			return m, true
		}
	}
	return Machine{}, false
}
