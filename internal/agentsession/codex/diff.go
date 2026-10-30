package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"flint/internal/agentsession"
)

// maxDiffFileBytes bounds files read from disk to reconstruct edits.
const maxDiffFileBytes = 8 << 20

const noNewlineMarker = `\ No newline at end of file`

var errHunkMismatch = errors.New("codex: hunk does not apply")

// hunkLine is one body line of a unified diff hunk.
type hunkLine struct {
	op   byte   // ' ', '-' or '+'
	text string // without terminator
	eol  bool   // false when followed by "\ No newline at end of file"
}

type hunk struct {
	oldStart int // 1-based line numbers from the header
	newStart int
	lines    []hunkLine
}

// parseHunks parses the hunks of a unified diff. Header lines before the
// first "@@" (diff --git, ---, +++) are skipped.
func parseHunks(diff string) ([]hunk, error) {
	var hunks []hunk
	for _, raw := range splitLines(diff) {
		line := strings.TrimSuffix(raw, "\n")
		switch {
		case strings.HasPrefix(line, "@@"):
			oldStart, newStart, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, hunk{oldStart: oldStart, newStart: newStart})
		case len(hunks) == 0:
			continue
		case line == noNewlineMarker:
			h := &hunks[len(hunks)-1]
			if n := len(h.lines); n > 0 {
				h.lines[n-1].eol = false
			}
		case line == "":
			// Some generators drop the leading space of empty context lines.
			appendLine(&hunks[len(hunks)-1], ' ', "")
		case line[0] == ' ' || line[0] == '-' || line[0] == '+':
			appendLine(&hunks[len(hunks)-1], line[0], line[1:])
		default:
			return nil, errHunkMismatch
		}
	}
	if len(hunks) == 0 {
		return nil, errHunkMismatch
	}
	return hunks, nil
}

func appendLine(h *hunk, op byte, text string) {
	h.lines = append(h.lines, hunkLine{op: op, text: text, eol: true})
}

// parseHunkHeader returns the starts a and c of "@@ -a,b +c,d @@".
func parseHunkHeader(line string) (oldStart, newStart int, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[1], "-") || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, errHunkMismatch
	}
	if oldStart, err = rangeStart(fields[1][1:]); err != nil {
		return 0, 0, err
	}
	if newStart, err = rangeStart(fields[2][1:]); err != nil {
		return 0, 0, err
	}
	return oldStart, newStart, nil
}

func rangeStart(r string) (int, error) {
	text, _, _ := strings.Cut(r, ",")
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 {
		return 0, errHunkMismatch
	}
	return n, nil
}

// splitLines splits s into lines that keep their "\n" terminator.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// applyHunks applies hunks to src. With reverse set, '+' and '-' swap roles
// (src is the new text and the result is the old one).
func applyHunks(src string, hunks []hunk, reverse bool) (string, error) {
	lines := splitLines(src)
	var out strings.Builder
	cursor := 0
	for _, h := range hunks {
		pos, ok := locateHunk(lines, h, cursor, reverse)
		if !ok {
			return "", errHunkMismatch
		}
		for _, l := range lines[cursor:pos] {
			out.WriteString(l)
		}
		cursor = writeHunk(&out, lines, pos, h, reverse)
	}
	for _, l := range lines[cursor:] {
		out.WriteString(l)
	}
	return out.String(), nil
}

// sides returns the ops present only in the source / target text.
func sides(reverse bool) (source, target byte) {
	if reverse {
		return '+', '-'
	}
	return '-', '+'
}

// locateHunk finds where h's source lines match, preferring the position the
// header names and otherwise scanning forward from cursor.
func locateHunk(lines []string, h hunk, cursor int, reverse bool) (int, bool) {
	start := h.oldStart
	if reverse {
		start = h.newStart
	}
	expected := start - 1
	if sourceLen(h, reverse) == 0 {
		if start > len(lines) {
			return 0, false
		}
		expected = start // a pure insertion goes after line `start`
	}
	expected = min(max(expected, cursor), len(lines))
	if matchesAt(lines, h, expected, reverse) {
		return expected, true
	}
	if sourceLen(h, reverse) == 0 {
		return 0, false
	}
	for pos := cursor; pos <= len(lines); pos++ {
		if matchesAt(lines, h, pos, reverse) {
			return pos, true
		}
	}
	return 0, false
}

// sourceLen counts the lines h expects to find in the source text.
func sourceLen(h hunk, reverse bool) int {
	_, target := sides(reverse)
	n := 0
	for _, hl := range h.lines {
		if hl.op != target {
			n++
		}
	}
	return n
}

func matchesAt(lines []string, h hunk, pos int, reverse bool) bool {
	_, target := sides(reverse)
	i := pos
	for _, hl := range h.lines {
		if hl.op == target {
			continue
		}
		if i >= len(lines) || strings.TrimSuffix(strings.TrimSuffix(lines[i], "\n"), "\r") != strings.TrimSuffix(hl.text, "\r") {
			return false
		}
		i++
	}
	return true
}

// writeHunk emits the target side of h and returns the new cursor.
func writeHunk(out *strings.Builder, lines []string, pos int, h hunk, reverse bool) int {
	source, target := sides(reverse)
	i := pos
	for _, hl := range h.lines {
		switch hl.op {
		case target:
			out.WriteString(hl.text)
			if hl.eol {
				out.WriteByte('\n')
			}
		case source:
			i++
		default:
			out.WriteString(lines[i]) // context keeps its original terminator
			i++
		}
	}
	return i
}

// readSmallFile reads path if it is a regular file of bounded size.
func readSmallFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDiffFileBytes {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// absPath resolves p against cwd.
func absPath(cwd, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(cwd, p)
}

// buildDiff turns one FileUpdateChange into old/new text. The protocol only
// carries a unified diff (or raw content for add/delete), so update hunks
// are applied to the file on disk: forward while the patch is pending,
// in reverse once it has been applied. applied says which to try first, as
// some patches (pure appends) apply both ways.
func buildDiff(cwd string, ch fileUpdateChange, applied bool) (agentsession.Diff, bool) {
	path := absPath(cwd, ch.Path)
	switch ch.Kind.Type {
	case "add":
		text, ok := contentOf(ch.Diff, "")
		return agentsession.Diff{Path: path, NewText: text}, ok
	case "delete":
		if ch.Diff == "" {
			old, ok := readSmallFile(path)
			return agentsession.Diff{Path: path, OldText: old}, ok
		}
		return agentsession.Diff{Path: path, OldText: ch.Diff}, true
	}
	target := path
	if ch.Kind.MovePath != "" {
		target = absPath(cwd, ch.Kind.MovePath)
	}
	hunks, err := parseHunks(ch.Diff)
	if err != nil {
		return agentsession.Diff{}, false
	}
	for _, candidate := range []string{path, target} {
		cur, ok := readSmallFile(candidate)
		if !ok {
			continue
		}
		for _, reverse := range []bool{applied, !applied} {
			other, err := applyHunks(cur, hunks, reverse)
			if err != nil {
				continue
			}
			if reverse {
				return agentsession.Diff{Path: target, OldText: other, NewText: cur}, true
			}
			return agentsession.Diff{Path: target, OldText: cur, NewText: other}, true
		}
	}
	return agentsession.Diff{}, false
}

// contentOf returns raw added content, or applies hunks to base when the
// server sent a unified diff instead.
func contentOf(diff, base string) (string, bool) {
	if !strings.HasPrefix(diff, "@@") && !strings.HasPrefix(diff, "--- ") && !strings.HasPrefix(diff, "diff --git") {
		return diff, true
	}
	hunks, err := parseHunks(diff)
	if err != nil {
		return "", false
	}
	out, err := applyHunks(base, hunks, false)
	return out, err == nil
}

// unifiedText renders a change for the Output fallback.
func unifiedText(cwd string, ch fileUpdateChange) string {
	path := absPath(cwd, ch.Path)
	return "--- " + path + "\n+++ " + path + "\n" + ch.Diff
}
