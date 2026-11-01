package git

import (
	"context"
	"strconv"
	"strings"
)

// Bounds of one `git diff` call of WorkingDiffs: argument lists stay far
// below the system's limit however many files are reviewed.
const (
	diffBatchPaths = 400
	diffBatchBytes = 96 << 10
)

// WorkingDiffs is the diff between HEAD (the empty tree before the first
// commit) and the working tree of each path in groups, what the Commit panel
// shows, split by path: one "diff --git" section each, keyed by the path of
// its new version (a deletion by its old path). A group holds one change's
// paths (a rename and its source) and is never split across git calls. Diff
// drivers stay off.
func (r *Repo) WorkingDiffs(ctx context.Context, groups [][]string) (map[string]string, error) {
	out := map[string]string{}
	if len(groups) == 0 {
		return out, nil
	}
	base := "HEAD"
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		base = emptyTree
	}
	for _, batch := range batchGroups(groups) {
		args := append([]string{"diff", base, "--no-color", "--no-ext-diff", "--no-textconv", "--find-renames", "--"}, batch...)
		diff, err := r.git(ctx, args...)
		if err != nil {
			return nil, err
		}
		for path, section := range splitDiff(string(diff)) {
			out[path] += section
		}
	}
	return out, nil
}

// batchGroups flattens groups into argument batches within the bounds.
func batchGroups(groups [][]string) [][]string {
	var batches [][]string
	var cur []string
	size := 0
	for _, g := range groups {
		n := 0
		for _, p := range g {
			n += len(p) + 1
		}
		if len(cur) > 0 && (len(cur)+len(g) > diffBatchPaths || size+n > diffBatchBytes) {
			batches = append(batches, cur)
			cur, size = nil, 0
		}
		cur = append(cur, g...)
		size += n
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// splitDiff splits a multi-file diff into its "diff --git" sections by path.
func splitDiff(diff string) map[string]string {
	out := map[string]string{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			s := cur.String()
			if p := sectionPath(s); p != "" {
				out[p] += s
			}
			cur.Reset()
		}
	}
	for line := range strings.SplitAfterSeq(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
		}
		cur.WriteString(line)
	}
	flush()
	return out
}

// sectionPath is the path a diff section is about: its new version's
// (+++ b/, rename to), else its old one's (a deletion), else the one in the
// header when both sides name the same file (binary changes).
func sectionPath(section string) string {
	var plus, minus, renamed, header string
scan:
	for line := range strings.SplitSeq(section, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header = strings.TrimPrefix(line, "diff --git ")
		case strings.HasPrefix(line, "+++ "):
			plus = diffPath(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "--- "):
			minus = diffPath(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "rename to "), strings.HasPrefix(line, "copy to "):
			renamed = unquotePath(line[strings.Index(line, " to ")+4:])
		case strings.HasPrefix(line, "@@"):
			break scan // the header is over
		}
	}
	switch {
	case plus != "":
		return plus
	case renamed != "":
		return renamed
	case minus != "":
		return minus
	}
	// "a/P b/P": both halves are the same path.
	if n := len(header); n > 5 && (n-5)%2 == 0 && !strings.HasPrefix(header, `"`) {
		p := header[2 : 2+(n-5)/2]
		if header == "a/"+p+" b/"+p {
			return p
		}
	}
	return ""
}

// diffPath is the path of a ---/+++ line without its a/ or b/ prefix, or ""
// for /dev/null.
func diffPath(s, prefix string) string {
	s = unquotePath(strings.TrimSuffix(s, "\t"))
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// unquotePath undoes git's C-style quoting of unusual paths.
func unquotePath(s string) string {
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}
