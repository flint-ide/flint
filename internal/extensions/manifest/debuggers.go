package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"flint/internal/extensions/vars"
)

// Debugger is a debug adapter an extension adds for some languages: a
// program speaking the Debug Adapter Protocol on its standard input and
// output, which Flint's debugger drives like Delve. It starts only in trusted
// folders, as it runs the project's code.
type Debugger struct {
	// Type names the debugger ("ruby-rdbg"); it is the adapterID Flint sends.
	Type  string `json:"type"`
	Label string `json:"label"`
	// Languages are language ids (the extension's own or another's) whose
	// files it debugs.
	Languages []string `json:"languages"`
	// Adapter is the program: the first candidate found runs.
	Adapter *Server `json:"adapter"`
	// Launch is the DAP launch request's arguments, with vars.Names
	// substituted in its string values ("${file}").
	Launch json.RawMessage `json:"launch,omitempty"`
}

// TestRunner runs the tests of a file in some languages: Command is an
// argument list (no shell) with vars.Names and vars.Tests substituted.
type TestRunner struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Languages []string `json:"languages"`
	Command   []string `json:"command"`
	// Format is how Flint reads its output: "tap", or "plain" (only the exit
	// code counts; also when empty).
	Format string `json:"format,omitempty"`
}

// Test runner output formats (testrun.FormatTAP, testrun.FormatPlain).
const (
	TestFormatTAP   = "tap"
	TestFormatPlain = "plain"
)

// Limits of the debuggers and test runners of one extension.
const (
	maxDebuggers     = 10
	maxTestRunners   = 10
	maxToolLanguages = 20
	maxLaunch        = 16 << 10
	maxLaunchDepth   = 8
)

func (m *Manifest) checkDebuggers() error {
	if err := checkCount("debuggers", len(m.Contributes.Debuggers), maxDebuggers); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range m.Contributes.Debuggers {
		if !languagePattern.MatchString(d.Type) {
			return fmt.Errorf("debugger type %q: use lower-case letters, digits, +, #, _ or -, at most 40", d.Type)
		}
		if seen[d.Type] {
			return fmt.Errorf("debugger %q is listed twice", d.Type)
		}
		seen[d.Type] = true
		if err := d.check(); err != nil {
			return fmt.Errorf("debugger %s: %w", d.Type, err)
		}
	}
	return nil
}

func (d Debugger) check() error {
	if err := checkText("label", d.Label, 40, true); err != nil {
		return err
	}
	if err := checkToolLanguages(d.Languages); err != nil {
		return err
	}
	if d.Adapter == nil {
		return fmt.Errorf("adapter is missing")
	}
	if err := d.Adapter.checkAs("adapter"); err != nil {
		return fmt.Errorf("adapter: %w", err)
	}
	for _, c := range d.Adapter.Candidates {
		for _, a := range c[1:] {
			if err := vars.Check(a, false); err != nil {
				return fmt.Errorf("adapter: %w", err)
			}
		}
	}
	return checkLaunch(d.Launch)
}

// checkLaunch: a JSON object of bounded size and depth whose strings use only
// known variables.
func checkLaunch(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxLaunch {
		return fmt.Errorf("launch is larger than %d KiB", maxLaunch>>10)
	}
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '{' {
		return fmt.Errorf("launch must be a JSON object")
	}
	x, err := vars.Decode(raw)
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	_, err = vars.Walk(x, maxLaunchDepth, func(s string) (string, error) { return s, vars.Check(s, false) })
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	return nil
}

func (m *Manifest) checkTestRunners() error {
	if err := checkCount("testRunners", len(m.Contributes.TestRunners), maxTestRunners); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range m.Contributes.TestRunners {
		if !languagePattern.MatchString(r.ID) {
			return fmt.Errorf("test runner id %q: use lower-case letters, digits, +, #, _ or -, at most 40", r.ID)
		}
		if seen[r.ID] {
			return fmt.Errorf("test runner %q is listed twice", r.ID)
		}
		seen[r.ID] = true
		if err := r.check(); err != nil {
			return fmt.Errorf("test runner %s: %w", r.ID, err)
		}
	}
	return nil
}

func (r TestRunner) check() error {
	if err := checkText("label", r.Label, 40, true); err != nil {
		return err
	}
	if err := checkToolLanguages(r.Languages); err != nil {
		return err
	}
	if len(r.Command) == 0 {
		return fmt.Errorf("command is missing")
	}
	if err := checkProgram("test runner", r.Command[0]); err != nil {
		return err
	}
	if err := checkArgs(r.Command[1:]); err != nil {
		return err
	}
	for _, a := range r.Command[1:] {
		if err := vars.Check(a, true); err != nil {
			return err
		}
	}
	switch r.Format {
	case "", TestFormatTAP, TestFormatPlain:
		return nil
	}
	return fmt.Errorf(`format %q: use "tap" or "plain"`, r.Format)
}

// checkToolLanguages: at least one language id, each like a language's.
func checkToolLanguages(langs []string) error {
	if len(langs) == 0 {
		return fmt.Errorf("no languages")
	}
	if len(langs) > maxToolLanguages {
		return fmt.Errorf("at most %d languages", maxToolLanguages)
	}
	for _, l := range langs {
		if !languagePattern.MatchString(l) {
			return fmt.Errorf("language %q: use lower-case letters, digits, +, #, _ or -", l)
		}
	}
	return nil
}

// CommandLine shows a test runner's command in the install dialog.
func (r TestRunner) CommandLine() string { return strings.Join(r.Command, " ") }
