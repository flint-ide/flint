package review

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"flint/internal/redact"
)

// Change is one changed file as the review sees it.
type Change struct {
	Path     string // repo-relative, slash-separated
	OrigPath string // the source of a rename or copy
	Kind     string // git.FileChange kinds: modified, added, deleted, renamed, copied, typechange, untracked
	// Diff is git's diff of this path against HEAD (tracked files): one
	// "diff --git" section.
	Diff string
	// Size and Read give an untracked file's content: Read returns at most
	// limit bytes, whether they look binary, and false when the file can't
	// be read. The file is read only as far as its share of the limit.
	Size int
	Read func(limit int) (content string, binary, ok bool)
}

// Input is what the agent gets on stdin: every reviewed file's diff with the
// line numbers of the new version spelled out, at most maxBytes long.
type Input struct {
	Text string
	// Files are the paths sent; files that hold credentials, and files there
	// was no room for, are left out.
	Files []string
	// Notes tell the user what was left out or shortened.
	Notes []string
	// Shortened is set when a file's diff was cut, summarized or left out.
	Shortened bool
}

// Room a file's diff needs before it is worth more than its summary line,
// and what is kept back for the line that says more files were left out.
const (
	minHunkRoom   = 192
	trailerRoom   = 96
	redactMargin  = 512 // read past a cut so a secret it splits is still redacted
	shortenedNote = "… shortened to fit the size limit (read the file for the rest)\n"
)

// file is one change on its way into the input.
type file struct {
	c      Change
	header string
	// special replaces the diff of binary, deleted and unchanged files.
	special        string
	hunks          []string // not redacted yet
	added, removed int
	full           int // the body's size in whole (estimated for untracked files)
}

// BuildInput renders changes for the agent within maxBytes. Every file sent
// gets its header and at least a one-line summary; small diffs go in whole
// and the larger ones share the rest of the room, keeping their first hunks.
// Files there is no room for at all are left out with a note. Text is cut
// before it is redacted (with a margin, so a cut never splits a secret out of
// reach of the redaction), and files that hold credentials are not sent.
func BuildInput(changes []Change, maxBytes int) Input {
	var in Input
	var secret []string
	var files []*file
	for _, c := range changes {
		if redact.SecretFile(c.Path) || (c.OrigPath != "" && redact.SecretFile(c.OrigPath)) {
			secret = append(secret, c.Path)
			continue
		}
		files = append(files, newFile(c))
	}
	room := max(0, maxBytes-trailerRoom)
	var sent []*file
	for _, f := range files {
		need := len(f.header) + len(f.minBody()) + 1
		if need > room {
			break // the files after it don't fit either: order is kept
		}
		room -= need
		sent = append(sent, f)
	}
	want := make([]int, len(sent))
	for i, f := range sent {
		want[i] = max(0, f.full-len(f.minBody()))
	}
	extra := share(want, room)
	var b strings.Builder
	cut := 0
	for i, f := range sent {
		body, whole := f.body(len(f.minBody()) + extra[i])
		if !whole {
			cut++
		}
		b.WriteString(f.header)
		b.WriteString(body)
		b.WriteString("\n")
		in.Files = append(in.Files, f.c.Path)
	}
	left := files[len(sent):]
	if len(left) > 0 {
		fmt.Fprintf(&b, "(%d more changed files left out to fit the size limit)\n", len(left))
	}
	in.Text = capBytes(b.String(), maxBytes)
	if len(secret) > 0 {
		in.Notes = append(in.Notes, fmt.Sprintf("Not sent to the agent (they hold credentials): %s.", listNames(secret)))
	}
	if cut > 0 || len(left) > 0 {
		in.Shortened = true
		msg := fmt.Sprintf("The diff was shortened to fit the %d KB limit: %d of %d files cut or summarized", maxBytes>>10, cut, len(sent))
		if len(left) > 0 {
			names := make([]string, len(left))
			for i, f := range left {
				names[i] = f.c.Path
			}
			msg += fmt.Sprintf(", and %d left out (%s)", len(left), listNames(names))
		}
		in.Notes = append(in.Notes, msg+". The agent can read files itself.")
	}
	return in
}

// share splits room between wants: in order of size, each gets what it wants
// or an even share of what is left, whichever is smaller.
func share(wants []int, room int) []int {
	order := make([]int, len(wants))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return wants[a] - wants[b] })
	out := make([]int, len(wants))
	for k, i := range order {
		out[i] = min(wants[i], room/(len(order)-k))
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

func newFile(c Change) *file {
	f := &file{c: c, header: "## " + oneLine(c.Path) + " (" + oneLine(kindLabel(c)) + ")\n"}
	switch {
	case c.Kind == "deleted":
		f.special = "(file deleted; there is no new version to comment on)\n"
	case c.Kind == "untracked":
		// Numbering adds 7 bytes a line: at most as much again for lines
		// of 7 bytes or more. The read decides what fits.
		f.full = 2*c.Size + len(newFileHunk)
	default:
		f.parseDiff()
	}
	if f.special != "" {
		f.full = len(f.special)
	} else if c.Kind != "untracked" {
		f.full = 0
		for _, h := range f.hunks {
			f.full += len(h)
		}
	}
	return f
}

// minBody is what the file gets at the least: its special line or a summary.
func (f *file) minBody() string {
	switch {
	case f.special != "":
		return f.special
	case f.c.Kind == "untracked":
		return fmt.Sprintf("(new file, %d bytes, left out to fit the size limit; read it if you need it)\n", f.c.Size)
	}
	return fmt.Sprintf("(diff left out to fit the size limit: +%d/-%d lines in %d hunks; read the file if you need it)\n", f.added, f.removed, len(f.hunks))
}

const newFileHunk = "@@ new file @@\n"

// body is the file's part of the input in at most budget bytes, redacted, and
// whether it is whole.
func (f *file) body(budget int) (string, bool) {
	if f.special != "" {
		return f.special, true
	}
	if f.c.Kind == "untracked" {
		if f.c.Read == nil || (budget < 2*f.c.Size+len(newFileHunk) && budget-len(shortenedNote) < minHunkRoom) {
			return f.minBody(), false // too little room for more than the summary
		}
		content, binary, ok := f.c.Read(min(f.c.Size, budget))
		switch {
		case !ok:
			return "(not a regular file, or not readable; not shown)\n", true
		case binary:
			return "(binary file, not shown)\n", true
		}
		complete := len(content) >= f.c.Size
		if !complete { // the last line may be cut: drop it
			if i := strings.LastIndexByte(content, '\n'); i >= 0 {
				content = content[:i+1]
			}
		}
		var b strings.Builder
		b.WriteString(newFileHunk)
		for i, line := range Lines(content) {
			fmt.Fprintf(&b, "%5d +%s\n", i+1, line)
		}
		f.hunks = []string{b.String()}
		body, whole := cutHunks(f.hunks, budget, f.minBody())
		return body, whole && complete
	}
	return cutHunks(f.hunks, budget, f.minBody())
}

// cutHunks joins hunks into at most budget bytes, redacted: all of them when
// they fit, else the whole hunks that fit (or the first lines of the first
// one) and a note, else the summary.
func cutHunks(hunks []string, budget int, summary string) (string, bool) {
	full := strings.Join(hunks, "")
	if len(full) <= budget {
		if red := clean(full); len(red) <= budget {
			return red, true
		}
	}
	limit := budget - len(shortenedNote)
	kept := 0
	for _, h := range hunks {
		if kept+len(h) > limit {
			break
		}
		kept += len(h)
	}
	switch {
	case kept > 0:
		limit = kept
	case limit < minHunkRoom:
		return summary, false
	}
	// Redact a little past the cut, then cut at the last line that fits.
	red := clean(full[:min(len(full), limit+redactMargin)])
	if len(red) > limit {
		red = red[:limit]
		if i := strings.LastIndexByte(red, '\n'); i >= 0 {
			red = red[:i+1]
		} else {
			red = capBytes(red, limit)
		}
	}
	return red + shortenedNote, false
}

// clean redacts secrets and drops NUL bytes.
func clean(s string) string {
	return strings.ReplaceAll(redact.String(s), "\x00", "")
}

// capBytes cuts s to at most n bytes on a rune boundary.
func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == 0 {
			return ' '
		}
		return r
	}, s)
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parseDiff numbers the new version's lines of the file's diff section. A
// second "diff --git" section (another path) ends it.
func (f *file) parseDiff() {
	var cur *strings.Builder
	n := 0
	flush := func() {
		if cur != nil {
			f.hunks = append(f.hunks, cur.String())
		}
	}
	sections := 0
	for _, line := range strings.Split(strings.ReplaceAll(f.c.Diff, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			if sections++; sections > 1 {
				break
			}
			continue
		}
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = &strings.Builder{}
			n, _ = strconv.Atoi(m[1])
			cur.WriteString(line + "\n")
			continue
		}
		if cur == nil {
			if strings.HasPrefix(line, "Binary files ") {
				f.special = "(binary file, not shown)\n"
				return
			}
			continue // the section's header
		}
		switch {
		case strings.HasPrefix(line, "+"):
			fmt.Fprintf(cur, "%5d +%s\n", n, line[1:])
			n++
			f.added++
		case strings.HasPrefix(line, "-"):
			fmt.Fprintf(cur, "%5s -%s\n", "", line[1:])
			f.removed++
		case strings.HasPrefix(line, " "):
			fmt.Fprintf(cur, "%5d  %s\n", n, line[1:])
			n++
		}
	}
	flush()
	if len(f.hunks) == 0 {
		if f.c.Kind == "renamed" || f.c.Kind == "copied" {
			f.special = "(" + f.c.Kind + " without changes)\n"
		} else {
			f.special = "(no changes to the content)\n"
		}
	}
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
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), "\n")
}
