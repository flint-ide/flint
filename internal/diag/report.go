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
	"sync"
	"time"

	"flint/internal/redact"
)

// IssuesURL is where bug reports go.
const IssuesURL = "https://github.com/ODudek/flint/issues/new"

// Limits: a GitHub new-issue link stays well under browser URL limits; the
// full report is also copied to the clipboard.
const (
	logTailLines = 150
	maxURLBody   = 6000
	toolTimeout  = 3 * time.Second
)

// tools whose versions explain most environment-specific bugs.
var tools = [][]string{
	{"git", "--version"}, {"rg", "--version"}, {"gh", "--version"}, {"docker", "--version"},
	{"kubectl", "version", "--client=true"}, {"aws", "--version"}, {"dlv", "version"},
	{"gopls", "version"}, {"node", "--version"}, {"claude", "--version"}, {"codex", "--version"},
}

// Info is what the app knows about itself.
type Info struct {
	Version string
	Theme   string
	Agent   string
	Roots   int // open project folders (their paths stay private)
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
	for _, line := range toolVersions(ctx) {
		b.WriteString("- " + line + "\n")
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
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sw_vers", "-productVersion").Output()
	if err != nil {
		return runtime.GOOS
	}
	return strings.TrimSpace(string(out))
}

// toolVersions asks every tool at once; missing ones say so.
func toolVersions(ctx context.Context) []string {
	out := make([]string, len(tools))
	var wg sync.WaitGroup
	for i, argv := range tools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = argv[0] + ": " + toolVersion(ctx, argv)
		}()
	}
	wg.Wait()
	return out
}

func toolVersion(ctx context.Context, argv []string) string {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return "not installed"
	}
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, argv[1:]...)
	cmd.Stdin = nil
	out, _ := cmd.CombinedOutput()
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if line == "" {
		return "installed (version unknown)"
	}
	return truncate(line, 120)
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

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
