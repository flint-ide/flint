package main

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"flint/internal/desktop"
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

	toolsMu     sync.Mutex
	mu          sync.Mutex
	windowStart time.Time
	logged      int
}

// Report is the redacted diagnostics (versions, tools, end of the log).
func (d *DiagnosticsAPI) Report() string {
	s := d.app.currentSettings()
	return diag.Report(d.app.ctx, diag.Info{
		Version: diag.Version(version), Theme: s.Theme, Agent: s.DefaultAgent, Roots: len(d.app.projectRoots()),
		Tools: d.app.configuredTools(),
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
// Calls run one at a time, so a UI that keeps asking can't start a dozen
// version commands per call in parallel.
func (d *DiagnosticsAPI) Tools() []tools.Status {
	d.toolsMu.Lock()
	defer d.toolsMu.Unlock()
	return tools.Check(d.app.ctx, d.app.configuredTools())
}

// configuredTools are the agent and language-server binaries set in Settings,
// by tool id, so the tool check finds them where Flint will run them.
func (a *App) configuredTools() map[string]string {
	s := a.currentSettings()
	out := map[string]string{}
	for _, name := range []string{"claude", "codex"} {
		if p := s.Agents[name].Path; p != "" {
			out[name] = p
		}
	}
	for lang, id := range map[string]string{"go": "gopls", "typescript": "typescript-ls", "python": "python-ls", "rust": "rust-analyzer"} {
		if ls := s.LanguageServers[lang]; ls.Command != "" && !ls.Disabled {
			out[id] = ls.Command
		}
	}
	return out
}

// ToolCheckOnFirstRun is true once, on the first launch, so the tool check
// opens by itself only then.
func (d *DiagnosticsAPI) ToolCheckOnFirstRun() bool { return d.app.claimToolCheck() }

// LogPath is the log file, for the UI.
func (d *DiagnosticsAPI) LogPath() string { return diag.LogPath() }

// RevealLogs selects the log file in the file manager.
func (d *DiagnosticsAPI) RevealLogs() error { return desktop.Reveal(diag.LogPath()) }

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
