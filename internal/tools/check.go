package tools

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"flint/internal/shellenv"
)

// States of a checked tool.
const (
	StateOK      = "ok"
	StateMissing = "missing"
	StateError   = "error" // on PATH, but its version command failed (e.g. a rustup proxy without the component)
)

const (
	versionTimeout = 5 * time.Second // node-based CLIs can be slow on a cold start
	maxOutput      = 64 << 10        // a version is one line; don't buffer a misbehaving binary's flood
	maxDetail      = 120
)

// Status is a tool as found on this machine.
type Status struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Group    string   `json:"group"`
	Feature  string   `json:"feature"`
	Required bool     `json:"required"`
	Install  string   `json:"install"`
	DocsURL  string   `json:"docsUrl"`
	Binaries []string `json:"binaries"` // what was looked for, in order
	State    string   `json:"state"`
	Binary   string   `json:"binary"`  // the binary found; "" when missing
	Version  string   `json:"version"` // "2.43.0"; "" when unknown
	Detail   string   `json:"detail"`  // the version line as printed, or why the check failed
}

type (
	lookPathFunc func(bin string) (string, error)
	runFunc      func(ctx context.Context, path string, args []string) (string, error)
)

// Check looks for every tool in parallel. It waits for the login-shell PATH
// first: before that, a GUI app sees only launchd's minimal PATH and would
// report Homebrew tools as missing.
// overrides maps a tool id to the binary configured in Settings (an absolute
// path or a command name), which is looked for before the usual names.
func Check(ctx context.Context, overrides map[string]string) []Status {
	shellenv.Wait()
	list := All()
	for i, t := range list {
		if bin := overrides[t.ID]; bin != "" {
			list[i] = t.withBinary(bin)
		}
	}
	return check(ctx, list, exec.LookPath, runVersion)
}

func check(ctx context.Context, list []Tool, lookPath lookPathFunc, run runFunc) []Status {
	out := make([]Status, len(list))
	var wg sync.WaitGroup
	for i, t := range list {
		wg.Go(func() { out[i] = checkOne(ctx, t, lookPath, run) })
	}
	wg.Wait()
	return out
}

func checkOne(ctx context.Context, t Tool, lookPath lookPathFunc, run runFunc) Status {
	s := Status{
		ID: t.ID, Name: t.Name, Group: t.Group, Feature: t.Feature, Required: t.Required,
		Install: t.Install, DocsURL: DocsBase + docsHTML(t.Docs), Binaries: t.Binaries(), State: StateMissing,
	}
	for _, c := range t.candidates {
		// LookPath also refuses binaries found through a relative PATH entry
		// (exec.ErrDot), so nothing is picked up from the current directory.
		path, err := lookPath(c.bin)
		if err != nil {
			continue
		}
		s.Binary, s.State = c.bin, StateOK
		if len(c.args) == 0 {
			return s
		}
		out, err := run(ctx, path, c.args)
		s.Version, s.Detail = parseVersion(out)
		if err != nil && s.Version == "" {
			s.State = StateError
			if s.Detail == "" {
				s.Detail = truncate(err.Error(), maxDetail)
			}
		}
		return s
	}
	return s
}

// docsHTML turns a site page ("guides/aws.md#eks") into its published URL path.
func docsHTML(page string) string {
	file, anchor, found := strings.Cut(page, "#")
	file = strings.TrimSuffix(file, ".md") + ".html"
	if found {
		return file + "#" + anchor
	}
	return file
}

// runVersion runs a version command outside any project, with no stdin.
func runVersion(ctx context.Context, path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	// Never the project folder (Flint may have been started by `flint .` inside
	// one): version managers read per-directory files (.tool-versions,
	// rust-toolchain.toml, .nvmrc) and an untrusted repository must not choose
	// what runs. The home folder gives the user's global versions.
	cmd.Dir = neutralDir()
	var buf cappedBuffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	cmd.WaitDelay = time.Second // a child holding the pipes open must not stall the check
	err := cmd.Run()
	return buf.String(), err
}

func neutralDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/"
}

// cappedBuffer keeps the first maxOutput bytes and drops the rest.
type cappedBuffer struct{ strings.Builder }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - b.Len(); room > 0 {
		b.Builder.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// versionPattern finds "2.43.0", "v1.31.0" or "1.0.0-beta.1" after a separator,
// so "aws-cli/2.15.0" and "Client Version: v1.31.0" both yield the number.
var versionPattern = regexp.MustCompile(`(?:^|[\s/(:@,])v?(\d+\.\d+(?:\.\d+)?(?:[-+][0-9A-Za-z.]+)?)`)

// parseVersion returns the version number and the line it was on (some tools,
// like dlv, print a banner first); without a number, the first line.
func parseVersion(out string) (version, line string) {
	first := ""
	for l := range strings.Lines(out) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if first == "" {
			first = l
		}
		if m := versionPattern.FindStringSubmatch(l); m != nil {
			return m[1], truncate(l, maxDetail)
		}
	}
	return "", truncate(first, maxDetail)
}

// ReportLine is a tool's line in the bug report.
func ReportLine(s Status) string {
	name := s.ID
	if s.Binary != "" && s.Binary != s.ID {
		name += " (" + s.Binary + ")"
	}
	switch {
	case s.State == StateMissing:
		return name + ": not installed"
	case s.State == StateError:
		return name + ": version check failed: " + s.Detail
	case s.Detail == "":
		return name + ": installed (version unknown)"
	default:
		return name + ": " + s.Detail
	}
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
