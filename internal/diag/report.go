package diag

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"flint/internal/redact"
	"flint/internal/tools"
)

// IssuesURL is where bug reports go.
const IssuesURL = "https://github.com/flint-ide/flint/issues/new"

// Limits: a GitHub new-issue link stays well under browser URL limits; the
// full report is also copied to the clipboard.
const (
	logTailLines  = 150
	maxURLBody    = 6000
	swVersTimeout = 3 * time.Second
)

// Info is what the app knows about itself.
type Info struct {
	Version string
	Theme   string
	Agent   string
	Roots   int // open project folders (their paths stay private)
	// Tools are the binaries configured in Settings, by tool id (see tools.Check).
	Tools map[string]string
}

// Report is a Markdown bug-report body: versions, environment and the end of
// the log, all redacted. Nothing is sent anywhere by building it.
func Report(ctx context.Context, info Info) string {
	var b strings.Builder
	b.WriteString("### Environment\n\n")
	fmt.Fprintf(&b, "- Flint: %s\n", info.Version)
	fmt.Fprintf(&b, "- macOS: %s (%s)\n", macOSVersion(ctx), runtime.GOARCH)
	fmt.Fprintf(&b, "- Go: %s, Wails: %s\n", runtime.Version(), moduleVersion("github.com/wailsapp/wails/v3"))
	fmt.Fprintf(&b, "- Theme: %s, default agent: %s, open folders: %d\n", info.Theme, firstNonEmpty(info.Agent, "auto"), info.Roots)
	b.WriteString("\n### Tools\n\n")
	// The same table as the tool check: their versions explain most
	// environment-specific bugs.
	for _, s := range tools.Check(ctx, info.Tools) {
		b.WriteString("- " + tools.ReportLine(s) + "\n")
	}
	b.WriteString("\n### Log (last lines)\n\n```\n")
	b.WriteString(LogTail(logTailLines))
	b.WriteString("```\n")
	return redact.String(b.String())
}

// IssueLink is a prefilled GitHub issue; a report too long for a URL is cut
// (the whole report is meant to be pasted from the clipboard instead).
func IssueLink(title, report string) string {
	body := "<!-- Describe what happened and what you expected. -->\n\n\n" + report
	if len(body) > maxURLBody {
		body = body[:maxURLBody] + "\n…\n(diagnostics cut: the full report is on your clipboard — paste it here)\n"
	}
	q := url.Values{"title": {title}, "body": {body}, "labels": {"bug"}}
	return IssuesURL + "?" + q.Encode()
}

// LogTail returns the last n lines of the log.
func LogTail(n int) string {
	f, err := os.Open(LogPath())
	if err != nil {
		return "(no log yet)\n"
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if len(lines) == 0 {
		return "(log is empty)\n"
	}
	return redact.String(strings.Join(lines, "\n")) + "\n"
}

func macOSVersion(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, swVersTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sw_vers", "-productVersion").Output()
	if err != nil {
		return runtime.GOOS
	}
	return strings.TrimSpace(string(out))
}

func moduleVersion(path string) string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		if i := slices.IndexFunc(bi.Deps, func(m *debug.Module) bool { return m.Path == path }); i >= 0 {
			return bi.Deps[i].Version
		}
	}
	return "unknown"
}

// Version is Flint's version: the -ldflags one, else the VCS revision of the build.
func Version(ldflags string) string {
	if ldflags != "" && ldflags != "dev" {
		return ldflags
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		var rev, dirty string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "+dirty"
				}
			}
		}
		if rev != "" {
			return "dev-" + rev[:min(12, len(rev))] + dirty
		}
	}
	return "dev"
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
