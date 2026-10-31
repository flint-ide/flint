package main

import (
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"flint/internal/diag"
	"flint/internal/tools"
)

// version is set at release builds (-ldflags "-X main.version=v1.2.3").
var version = "dev"

// Frontend errors are logged at most this often (a render loop must not flood the log).
const (
	frontendLogBurst  = 30
	frontendLogWindow = time.Minute
	maxFrontendLog    = 4000
)

// DiagnosticsAPI is how people report problems: the log, a redacted report,
// and a prefilled GitHub issue. Nothing leaves the machine unless the user
// submits the issue themselves. It also runs the tool check (which external
// tools are installed).
type DiagnosticsAPI struct {
	app *App

	mu          sync.Mutex
	windowStart time.Time
	logged      int
}

// Report is the redacted diagnostics (versions, tools, end of the log).
func (d *DiagnosticsAPI) Report() string {
	s := d.app.currentSettings()
	return diag.Report(d.app.ctx, diag.Info{
		Version: diag.Version(version), Theme: s.Theme, Agent: s.DefaultAgent, Roots: len(d.app.projectRoots()),
	})
}

// OpenIssue copies the report to the clipboard and opens a prefilled GitHub
// issue in the browser, where the user reviews it before submitting.
func (d *DiagnosticsAPI) OpenIssue(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Bug: "
	}
	report := d.Report()
	d.app.copyText(report)
	return d.app.openURL(diag.IssueLink(title, report))
}

// Tools reports which external tools Flint drives are installed, with their
// versions. Only fixed version commands run, never in a project folder.
func (d *DiagnosticsAPI) Tools() []tools.Status { return tools.Check(d.app.ctx) }

// ToolCheckOnFirstRun is true once, on the first launch, so the tool check
// opens by itself only then.
func (d *DiagnosticsAPI) ToolCheckOnFirstRun() bool { return d.app.claimToolCheck() }

// LogPath is the log file, for the UI.
func (d *DiagnosticsAPI) LogPath() string { return diag.LogPath() }

// RevealLogs selects the log file in Finder.
func (d *DiagnosticsAPI) RevealLogs() error {
	return exec.Command("open", "-R", diag.LogPath()).Run()
}

// LogFrontend records an error of the web UI (uncaught exceptions, failed
// renders), rate-limited and size-capped.
func (d *DiagnosticsAPI) LogFrontend(source, message, stack string) {
	d.mu.Lock()
	now := time.Now()
	if now.Sub(d.windowStart) > frontendLogWindow {
		d.windowStart, d.logged = now, 0
	}
	d.logged++
	over := d.logged > frontendLogBurst
	d.mu.Unlock()
	if over {
		return
	}
	cut := func(s string) string {
		if len(s) > maxFrontendLog {
			return s[:maxFrontendLog] + "…"
		}
		return s
	}
	slog.Error("frontend", "source", cut(source), "message", cut(message), "stack", cut(stack))
}
