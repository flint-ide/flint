package workflows

import (
	"errors"
	"flint/internal/agentdefs"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/agenttask"
	"flint/internal/settings"
)

// Settings is the "workflows" key of settings.json:
//
//	"workflows": { "maxAttempts": 3, "fallbackAgent": "codex", "resumeAfterMinutes": 60,
//	  "sandbox": "on", "sandboxNetwork": false, "sandboxWritable": ["~/Library/Caches/go-build"],
//	  "schedules": [{ "name": "nightly-deps", "workflow": "feature", "folder": "/abs/repo",
//	    "task": "Update the dependencies.", "agent": "", "at": "02:30", "days": ["mon", "fri"] }] }
type Settings struct {
	// MaxAttempts bounds how many times one step is taken before a failing
	// check stops sending the work back by itself and the run waits for the
	// user (Run.Failed's maxAttempts).
	MaxAttempts int `json:"maxAttempts"`
	// FallbackAgent takes a step again at once when the step's agent
	// reached its own usage limit ("" = the run is held instead).
	FallbackAgent string `json:"fallbackAgent"`
	// ResumeAfterMinutes resumes a run held by a limit after this many
	// minutes (0 = it waits for the user).
	ResumeAfterMinutes int `json:"resumeAfterMinutes"`
	// Sandbox is "on" (the default, also for a missing or unknown value) to
	// run script steps in the operating system's sandbox; "off" runs them
	// as the user's own runs, after the user's OK once per folder.
	Sandbox string `json:"sandbox"`
	// SandboxNetwork lets a sandboxed script use the network.
	SandboxNetwork bool `json:"sandboxNetwork"`
	// SandboxWritable are more folders a sandboxed script may write
	// (absolute, or starting with ~/), e.g. a build cache.
	SandboxWritable []string `json:"sandboxWritable"`
	// Schedules start runs at a time of day (schedule.go).
	Schedules []Schedule `json:"schedules"`
}

// The values of Settings.Sandbox.
const (
	SandboxOff = "off"
	SandboxOn  = "on"
)

// Bounds of the section.
const (
	defaultMaxAttempts = 3
	// maxMaxAttempts keeps a review that never passes from running agents
	// (and the usage budget) for long.
	maxMaxAttempts = 10
	// maxResumeAfter is 12 hours: a provider's limit resets within a day.
	maxResumeAfter = 720
	// maxWritable bounds sandboxWritable.
	maxWritable = 10
)

// Section is the "workflows" key.
var Section = &settings.Section[Settings]{
	Key:       "workflows",
	Default:   defaultSettings,
	Normalize: normalizeSettings,
	Check:     checkSettings,
	Clone:     cloneSettings,
}

func defaultSettings() Settings {
	return Settings{MaxAttempts: defaultMaxAttempts, Sandbox: SandboxOn, SandboxWritable: []string{}, Schedules: []Schedule{}}
}

func cloneSettings(s Settings) Settings {
	s.SandboxWritable = slices.Clone(s.SandboxWritable)
	s.Schedules = slices.Clone(s.Schedules)
	for i := range s.Schedules {
		s.Schedules[i].Days = slices.Clone(s.Schedules[i].Days)
	}
	return s
}

// normalizeSettings replaces each value that is out of range with its
// default, as for the built-in keys, and drops the entries of a list that
// break its rules (the extra ones past its bound too). Whether a writable
// folder exists is checked when it is saved and when a script runs, not
// here: a folder on a disk that is not mounted now stays in the settings.
func normalizeSettings(s Settings) Settings {
	s = cloneSettings(s)
	if checkMaxAttempts(s.MaxAttempts) != nil {
		s.MaxAttempts = defaultMaxAttempts
	}
	if checkFallback(s.FallbackAgent) != nil {
		s.FallbackAgent = ""
	}
	if checkResumeAfter(s.ResumeAfterMinutes) != nil {
		s.ResumeAfterMinutes = 0
	}
	if checkSandbox(s.Sandbox) != nil {
		s.Sandbox = SandboxOn
	}
	writable := []string{}
	for _, dir := range s.SandboxWritable {
		if len(writable) < maxWritable && writablePath(dir, "") == nil && !slices.Contains(writable, dir) {
			writable = append(writable, dir)
		}
	}
	s.SandboxWritable = writable
	s.Schedules = normalizeSchedules(s.Schedules)
	return s
}

func checkSettings(s Settings) error {
	if err := checkMaxAttempts(s.MaxAttempts); err != nil {
		return err
	}
	if err := checkFallback(s.FallbackAgent); err != nil {
		return err
	}
	if err := checkResumeAfter(s.ResumeAfterMinutes); err != nil {
		return err
	}
	if err := checkSandbox(s.Sandbox); err != nil {
		return err
	}
	if err := checkWritable(s.SandboxWritable, userHome()); err != nil {
		return err
	}
	return checkSchedules(s.Schedules)
}

func checkMaxAttempts(n int) error {
	if n < 1 || n > maxMaxAttempts {
		return fmt.Errorf("workflows.maxAttempts must be between 1 and %d", maxMaxAttempts)
	}
	return nil
}

func checkFallback(agent string) error {
	if agent != "" && !agentdefs.ValidName(agent) {
		return fmt.Errorf("workflows.fallbackAgent %q is not an agent's name (lowercase letters, digits and dashes)", agent)
	}
	return nil
}

func checkResumeAfter(n int) error {
	if n < 0 || n > maxResumeAfter {
		return fmt.Errorf("workflows.resumeAfterMinutes must be between 0 (wait for you) and %d", maxResumeAfter)
	}
	return nil
}

func checkSandbox(v string) error {
	if v != SandboxOff && v != SandboxOn {
		return fmt.Errorf("workflows.sandbox must be %q or %q", SandboxOff, SandboxOn)
	}
	return nil
}

// checkWritable checks sandboxWritable as it is saved: at most maxWritable
// folders, each absolute (or ~/…) and clean, and existing (with home).
func checkWritable(list []string, home string) error {
	if len(list) > maxWritable {
		return fmt.Errorf("workflows.sandboxWritable lists at most %d folders", maxWritable)
	}
	for _, dir := range list {
		if err := writablePath(dir, home); err != nil {
			return fmt.Errorf("workflows.sandboxWritable: %w", err)
		}
	}
	return nil
}

// writablePath checks one sandboxWritable folder: absolute, or ~/ and a
// path, clean and without control characters; with home set, it must also
// be a folder that exists.
func writablePath(dir, home string) error {
	rest, tilde := strings.CutPrefix(dir, "~/")
	abs := dir
	if tilde {
		abs = "/" + rest // checked for cleanliness as an absolute path
	}
	switch {
	case dir == "" || len(dir) > maxPath:
		return errors.New("a folder must be an absolute path")
	case strings.IndexFunc(dir, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		return fmt.Errorf("%q contains control characters", dir)
	case !filepath.IsAbs(abs) || filepath.Clean(abs) != abs || abs == "/" && tilde:
		return fmt.Errorf("%q is not an absolute, clean path (like /Users/me/cache or ~/cache)", dir)
	case home == "":
		return nil
	}
	if info, err := os.Stat(expandHome(dir, home)); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	return nil
}

// expandHome replaces a leading ~/ with home.
func expandHome(dir, home string) string {
	if home == "" {
		return dir
	}
	return agenttask.ExpandHome(dir, home)
}

// userHome is the user's home folder ("" when unknown).
func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}
