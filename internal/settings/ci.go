package settings

import "fmt"

// CI configures how Flint follows the checks of your pull requests (the
// status-bar CI badge, Fix with agent, background notifications).
type CI struct {
	// PollSeconds is how often a pull request's checks are re-read while some
	// are still running. Nothing is read while no check is running.
	PollSeconds int `json:"pollSeconds"`
	// LogTailLines is how many lines from the end of a failed step go into
	// the agent's draft.
	LogTailLines int `json:"logTailLines"`
	// LogErrorLines caps the error lines (test failures, compiler errors,
	// file:line references) picked from the rest of the failed step.
	LogErrorLines int `json:"logErrorLines"`
	// Notify shows a notification when CI fails or passes on your pull
	// request while Flint is in the background (needs NotifyInBackground).
	Notify bool `json:"notify"`
}

// Bounds of the CI settings: polls far enough apart not to spend the GitHub
// API rate limit, excerpts small enough for an agent's context.
const (
	MinCIPollSeconds   = 10
	MaxCIPollSeconds   = 3600
	MinCILogTailLines  = 5
	MaxCILogTailLines  = 1000
	MaxCILogErrorLines = 500
)

func defaultCI() CI {
	return CI{PollSeconds: 30, LogTailLines: 60, LogErrorLines: 40, Notify: true}
}

// CheckCI reports the first value out of range.
func CheckCI(c CI) error {
	switch {
	case c.PollSeconds < MinCIPollSeconds || c.PollSeconds > MaxCIPollSeconds:
		return fmt.Errorf("CI poll interval %d s out of range %d–%d", c.PollSeconds, MinCIPollSeconds, MaxCIPollSeconds)
	case c.LogTailLines < MinCILogTailLines || c.LogTailLines > MaxCILogTailLines:
		return fmt.Errorf("CI log tail %d lines out of range %d–%d", c.LogTailLines, MinCILogTailLines, MaxCILogTailLines)
	case c.LogErrorLines < 0 || c.LogErrorLines > MaxCILogErrorLines:
		return fmt.Errorf("CI error lines %d out of range 0–%d", c.LogErrorLines, MaxCILogErrorLines)
	}
	return nil
}

// normalizeCI clamps each number into its range (0 or missing poll and tail
// values are the defaults; 0 error lines is a valid choice).
func normalizeCI(c CI) CI {
	def := defaultCI()
	clamp := func(v, lo, hi, d int) int {
		if v == 0 {
			return d
		}
		return max(lo, min(hi, v))
	}
	return CI{
		PollSeconds:   clamp(c.PollSeconds, MinCIPollSeconds, MaxCIPollSeconds, def.PollSeconds),
		LogTailLines:  clamp(c.LogTailLines, MinCILogTailLines, MaxCILogTailLines, def.LogTailLines),
		LogErrorLines: max(0, min(MaxCILogErrorLines, c.LogErrorLines)),
		Notify:        c.Notify,
	}
}
