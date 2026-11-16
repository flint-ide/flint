package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"flint/internal/lsp"
	"flint/internal/testrun"
)

// Bounds of the answers (the server cuts anything longer anyway).
const (
	maxDiagLines   = 300
	listLines      = 30 // lines per console when all are listed
	maxFailures    = 30
	failureOutput  = 4 << 10
	failureLines   = 60
	maxEditorFiles = 100
)

var severityNames = map[string]int{"error": 1, "warning": 2, "information": 3, "hint": 4}

func severityName(n int) string {
	for name, v := range severityNames {
		if v == n {
			return name
		}
	}
	return "error" // LSP: a missing severity is up to the client; Flint's editor shows it as an error
}

func (s *Sources) diagnostics(_ context.Context, root string, args json.RawMessage) (string, error) {
	var in struct {
		Path     string `json:"path"`
		Severity string `json:"severity"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	scope, err := scopePath(root, in.Path)
	if err != nil {
		return "", err
	}
	limit := 4
	if in.Severity != "" {
		v, ok := severityNames[in.Severity]
		if !ok {
			return "", fmt.Errorf("severity must be error, warning, information or hint")
		}
		limit = v
	}
	type item struct {
		path string
		d    lsp.Diagnostic
	}
	var items []item
	counts := map[string]int{}
	files := map[string]bool{}
	byPath := s.Diagnostics.In(root)
	for _, path := range slices.Sorted(maps.Keys(byPath)) {
		if !within(path, scope) {
			continue
		}
		diags := slices.Clone(byPath[path])
		slices.SortStableFunc(diags, func(a, b lsp.Diagnostic) int {
			if a.Range.Start.Line != b.Range.Start.Line {
				return a.Range.Start.Line - b.Range.Start.Line
			}
			return a.Range.Start.Character - b.Range.Start.Character
		})
		for _, d := range diags {
			sev := d.Severity
			if sev < 1 || sev > 4 {
				sev = 1
			}
			if sev > limit {
				continue
			}
			counts[severityName(sev)]++
			files[path] = true
			items = append(items, item{path, d})
		}
	}
	where := "this project"
	if scope != root {
		where = rel(scope, root)
	}
	note := "Only files a language server has checked are included (usually the open files and their packages)."
	if len(items) == 0 {
		return fmt.Sprintf("No problems reported for %s. %s", where, note), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d problems in %d files of %s (errors: %d, warnings: %d, information: %d, hints: %d). %s\n",
		len(items), len(files), where, counts["error"], counts["warning"], counts["information"], counts["hint"], note)
	for _, it := range items[:min(len(items), maxDiagLines)] {
		sev := it.d.Severity
		if sev < 1 || sev > 4 {
			sev = 1
		}
		fmt.Fprintf(&b, "%s:%d:%d: %s: %s", rel(it.path, root), it.d.Range.Start.Line+1, it.d.Range.Start.Character+1,
			severityName(sev), strings.ReplaceAll(strings.TrimSpace(it.d.Message), "\n", " "))
		if it.d.Source != "" {
			fmt.Fprintf(&b, " (%s)", it.d.Source)
		}
		b.WriteByte('\n')
	}
	if len(items) > maxDiagLines {
		fmt.Fprintf(&b, "… and %d more; pass a path or a severity to narrow it down.\n", len(items)-maxDiagLines)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (s *Sources) editor(_ context.Context, root string, args json.RawMessage) (string, error) {
	if err := decode(args, &struct{}{}); err != nil {
		return "", err
	}
	snap := s.Editor.In(root)
	if len(snap.Files) == 0 && snap.Active == "" {
		return "No files of this project are open in Flint's editor.", nil
	}
	var b strings.Builder
	if snap.Active == "" {
		b.WriteString("Active tab: not a file of this project.\n")
	} else {
		fmt.Fprintf(&b, "Active file: %s%s\n", rel(snap.Active, root), unsaved(snap.Files, snap.Active))
		if sel := snap.Selection; sel != nil {
			if sel.Text == "" {
				fmt.Fprintf(&b, "Cursor: line %d, column %d\n", sel.StartLine, sel.StartColumn)
			} else {
				fmt.Fprintf(&b, "Selection: line %d, column %d to line %d, column %d:\n````\n%s\n````\n",
					sel.StartLine, sel.StartColumn, sel.EndLine, sel.EndColumn, sel.Text)
			}
		}
	}
	fmt.Fprintf(&b, "Open files (%d):\n", len(snap.Files))
	for _, f := range snap.Files[:min(len(snap.Files), maxEditorFiles)] {
		mark := ""
		if f.Dirty {
			mark = " (unsaved changes)"
		}
		fmt.Fprintf(&b, "- %s%s\n", rel(f.Path, root), mark)
	}
	if len(snap.Files) > maxEditorFiles {
		fmt.Fprintf(&b, "- … and %d more\n", len(snap.Files)-maxEditorFiles)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func unsaved(files []OpenFile, path string) string {
	if slices.ContainsFunc(files, func(f OpenFile) bool { return f.Path == path && f.Dirty }) {
		return " (unsaved changes: the file on disk is older)"
	}
	return ""
}

func (s *Sources) terminals(_ context.Context, root string, args json.RawMessage, maxLines int) (string, error) {
	var in struct {
		Terminal string `json:"terminal"`
		Lines    int    `json:"lines"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	if in.Lines < 0 {
		return "", fmt.Errorf("lines must be positive")
	}
	list := s.Terminals.List(root)
	if len(list) == 0 {
		return "No terminals or run consoles of this project are open in Flint.", nil
	}
	return s.consoleTails(root, list, "console", in.Terminal, in.Lines, maxLines)
}

// consoleTails renders the last lines of the consoles in list, or of the one
// called id (what: "console" or "run", for the message when there is none).
func (s *Sources) consoleTails(root string, list []TerminalInfo, what, id string, want, maxLines int) (string, error) {
	pick := list
	lines := min(maxLines, listLines)
	if id != "" {
		i := slices.IndexFunc(list, func(t TerminalInfo) bool { return t.ID == id })
		if i < 0 {
			ids := make([]string, len(list))
			for j, t := range list {
				ids[j] = t.ID
			}
			return "", fmt.Errorf("no %s %q in this project (%ss: %s)", what, cut(id, maxNameLen), what, strings.Join(ids, ", "))
		}
		pick, lines = list[i:i+1], maxLines
	}
	if want > 0 {
		lines = min(want, maxLines)
	}
	var b strings.Builder
	for _, t := range pick {
		tail, ok := s.Terminals.Tail(t.ID, root, lines)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "## %s %s: %s\n", t.Kind, t.ID, consoleState(t, root))
		if tail == "" {
			b.WriteString("(no output)\n\n")
			continue
		}
		b.WriteString("````\n" + tail + "\n````\n\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func consoleState(t TerminalInfo, root string) string {
	title := t.Title
	if title == "" {
		title = "shell at the prompt"
	}
	state := "running"
	if t.Exited {
		state = fmt.Sprintf("exited with code %d", t.Code)
	}
	return fmt.Sprintf("%s (in %s, %s)", title, rel(t.Cwd, root), state)
}

func (s *Sources) tests(_ context.Context, root string, args json.RawMessage) (string, error) {
	if err := decode(args, &struct{}{}); err != nil {
		return "", err
	}
	run, ok := s.Tests.Last(root)
	if !ok {
		return "No test run of this project in Flint yet.", nil
	}
	return s.describeRun("Last test run", run, root), nil
}

// describeRun renders a test run: how it ended, the totals and each failed
// test with its location and output.
func (s *Sources) describeRun(label string, run TestRun, root string) string {
	var b strings.Builder
	ago := s.now().Sub(run.Started).Round(time.Second)
	fmt.Fprintf(&b, "%s: %s (in %s, started %s ago)\n", label, run.Title, rel(run.Dir, root), ago)
	switch {
	case !run.Done:
		b.WriteString("Status: still running; results so far.\n")
	case run.Error != "":
		fmt.Fprintf(&b, "Status: could not start: %s\n", run.Error)
	case run.Stopped:
		b.WriteString("Status: stopped by the user.\n")
	default:
		fmt.Fprintf(&b, "Status: finished with exit code %d in %s.\n", run.ExitCode, (time.Duration(run.ElapsedMs) * time.Millisecond).Round(time.Millisecond))
	}
	byID := map[string]testrun.Node{}
	counts := map[testrun.Status]int{}
	var failed []testrun.Node
	for _, n := range run.Nodes {
		byID[n.ID] = n
		if n.Kind != testrun.KindTest {
			continue
		}
		counts[n.Status]++
		if n.Status == testrun.Failed {
			failed = append(failed, n)
		}
	}
	// A package can fail without a failing test (a build error).
	for _, n := range run.Nodes {
		if n.Kind == testrun.KindPackage && n.Status == testrun.Failed &&
			!slices.ContainsFunc(failed, func(f testrun.Node) bool { return packageOf(f, byID) == n.Name }) {
			failed = append(failed, n)
		}
	}
	fmt.Fprintf(&b, "Tests: %d passed, %d failed, %d skipped, %d running, %d cancelled\n",
		counts[testrun.Passed], counts[testrun.Failed], counts[testrun.Skipped], counts[testrun.Running], counts[testrun.Cancelled])
	if len(failed) == 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	fmt.Fprintf(&b, "\nFailed (%d):\n", len(failed))
	for _, n := range failed[:min(len(failed), maxFailures)] {
		name := n.Test
		if name == "" {
			name = n.Name
		}
		if pkg := packageOf(n, byID); pkg != "" && pkg != name {
			name = pkg + " " + name
		}
		if n.Location != nil {
			name += fmt.Sprintf(" (%s:%d)", rel(n.Location.Path, root), n.Location.Line)
		}
		fmt.Fprintf(&b, "- %s\n", name)
		if out := strings.TrimSpace(PlainText([]byte(n.Output))); out != "" {
			fmt.Fprintf(&b, "````\n%s\n````\n", tailBytes(lastLines(out, failureLines), failureOutput))
		}
	}
	if len(failed) > maxFailures {
		fmt.Fprintf(&b, "- … and %d more\n", len(failed)-maxFailures)
	}
	return strings.TrimRight(b.String(), "\n")
}

// packageOf names the package a node belongs to ("" when none).
func packageOf(n testrun.Node, byID map[string]testrun.Node) string {
	for range 64 { // parents form a tree; the bound only guards against a cycle
		if n.Kind == testrun.KindPackage {
			return n.Name
		}
		p, ok := byID[n.Parent]
		if !ok {
			return ""
		}
		n = p
	}
	return ""
}
