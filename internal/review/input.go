package review

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"flint/internal/redact"
)

// Change is one changed file as the review sees it.
type Change struct {
	Path     string // repo-relative, slash-separated
	OrigPath string // the source of a rename or copy
	Kind     string // git.FileChange kinds: modified, added, deleted, renamed, copied, typechange, untracked
	Diff     string // git's unified diff against HEAD (tracked files)
	Content  string // the new version (untracked files)
	Binary   bool   // an untracked file is binary
}

// Input is what the agent gets on stdin: every reviewed file's diff with the
// line numbers of the new version spelled out, bounded in size.
type Input struct {
	Text string
	// Files are the paths sent; files that hold credentials are left out.
	Files []string
	// Notes tell the user what was left out or shortened.
	Notes []string
	// Shortened is set when a file's diff was cut or summarized.
	Shortened bool
}

// minBody is the smallest share of the limit worth spending on a diff; a
// file that gets less is summarized in one line.
const minBody = 256

// BuildInput renders changes for the agent within maxBytes: small diffs go in
// whole, and the rest of the room is shared evenly by the larger ones, which
// keep their first hunks (or one line of stats) and say what was left out.
// Secrets are redacted, and files that hold credentials are not sent at all.
func BuildInput(changes []Change, maxBytes int) Input {
	var in Input
	var secret []string
	var files []rendered
	for _, c := range changes {
		if redact.SecretFile(c.Path) || (c.OrigPath != "" && redact.SecretFile(c.OrigPath)) {
			secret = append(secret, c.Path)
			continue
		}
		files = append(files, renderFile(c))
		in.Files = append(in.Files, c.Path)
	}
	headers := 0
	sizes := make([]int, len(files))
	for i, f := range files {
		headers += len(f.header) + 1
		sizes[i] = len(f.full())
	}
	budgets := share(sizes, max(0, maxBytes-headers))
	var b strings.Builder
	cut := 0
	for i, f := range files {
		body := f.body(budgets[i])
		if len(body) != sizes[i] {
			cut++
		}
		b.WriteString(f.header)
		b.WriteString(body)
		b.WriteString("\n")
	}
	in.Text = b.String()
	if len(secret) > 0 {
		in.Notes = append(in.Notes, fmt.Sprintf("Not sent to the agent (they hold credentials): %s.", listNames(secret)))
	}
	if cut > 0 {
		in.Shortened = true
		in.Notes = append(in.Notes, fmt.Sprintf("The diff was shortened to fit the %d KB limit: %d of %d files cut or summarized (the agent can read them itself).", maxBytes>>10, cut, len(files)))
	}
	return in
}

// share splits room between sizes: in order of size, each gets what it needs
// or an even share of what is left, whichever is smaller.
func share(sizes []int, room int) []int {
	order := make([]int, len(sizes))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return sizes[a] - sizes[b] })
	out := make([]int, len(sizes))
	for k, i := range order {
		out[i] = min(sizes[i], room/(len(order)-k))
		room -= out[i]
	}
	return out
}

func listNames(paths []string) string {
	if len(paths) <= 5 {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(paths[:5], ", "), len(paths)-5)
}

// rendered is one file's part of the input.
type rendered struct {
	header string
	// special replaces the hunks of binary, deleted and unchanged files.
	special        string
	hunks          []string
	added, removed int
}

func (r rendered) full() string {
	if r.special != "" {
		return r.special
	}
	return strings.Join(r.hunks, "")
}

// body is the file's diff in at most about budget bytes: whole hunks first,
// else the first lines of the first one, else (too little room) a one-line
// summary.
func (r rendered) body(budget int) string {
	full := r.full()
	if len(full) <= budget || r.special != "" {
		return full
	}
	var b strings.Builder
	kept := 0
	for _, h := range r.hunks {
		if b.Len()+len(h) > budget {
			break
		}
		b.WriteString(h)
		kept++
	}
	if kept == 0 && budget < minBody {
		return fmt.Sprintf("(diff left out to fit the size limit: +%d/-%d lines in %d hunks; read the file if you need it)\n", r.added, r.removed, len(r.hunks))
	}
	if kept == 0 {
		for line := range strings.SplitAfterSeq(r.hunks[0], "\n") {
			if b.Len()+len(line) > budget {
				break
			}
			b.WriteString(line)
		}
		kept = 1
	}
	fmt.Fprintf(&b, "… diff shortened to fit the size limit: %d of %d hunks left out (read the file for the rest)\n", len(r.hunks)-kept, len(r.hunks))
	return b.String()
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// renderFile numbers the new version's lines of c's diff (or of its whole
// content when it is untracked) and redacts secrets.
func renderFile(c Change) rendered {
	r := rendered{header: "## " + c.Path + " (" + kindLabel(c) + ")\n"}
	switch {
	case c.Kind == "deleted":
		r.special = "(file deleted; there is no new version to comment on)\n"
		return r
	case c.Binary:
		r.special = "(binary file, not shown)\n"
		return r
	case c.Kind == "untracked":
		var b strings.Builder
		b.WriteString("@@ new file @@\n")
		for i, line := range Lines(c.Content) {
			fmt.Fprintf(&b, "%5d +%s\n", i+1, line)
		}
		r.hunks = []string{redact.String(b.String())}
		r.added = len(Lines(c.Content))
		return r
	}
	var cur *strings.Builder
	n := 0
	flush := func() {
		if cur != nil {
			r.hunks = append(r.hunks, redact.String(cur.String()))
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(c.Diff, "\r\n", "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = &strings.Builder{}
			n, _ = strconv.Atoi(m[1])
			cur.WriteString(line + "\n")
			continue
		}
		if cur == nil {
			if strings.HasPrefix(line, "Binary files ") {
				r.special = "(binary file, not shown)\n"
				return r
			}
			continue // the diff's header
		}
		switch {
		case strings.HasPrefix(line, "+"):
			fmt.Fprintf(cur, "%5d +%s\n", n, line[1:])
			n++
			r.added++
		case strings.HasPrefix(line, "-"):
			fmt.Fprintf(cur, "%5s -%s\n", "", line[1:])
			r.removed++
		case strings.HasPrefix(line, " "):
			fmt.Fprintf(cur, "%5d  %s\n", n, line[1:])
			n++
		}
	}
	flush()
	if len(r.hunks) == 0 {
		if c.Kind == "renamed" || c.Kind == "copied" {
			r.special = "(" + c.Kind + " without changes)\n"
		} else {
			r.special = "(no changes to the content)\n"
		}
	}
	return r
}

func kindLabel(c Change) string {
	switch c.Kind {
	case "untracked", "added":
		return "new file"
	case "renamed", "copied":
		if c.OrigPath != "" {
			return c.Kind + " from " + c.OrigPath
		}
	}
	return c.Kind
}

// Lines splits content into lines without their line breaks; a final line
// break does not start another line.
func Lines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), "\n")
	return lines
}
