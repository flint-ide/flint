package main

// Pull request bodies are untrusted text. What comes out of them goes into
// the middle of the release body, so it is reshaped until it can't change the
// structure around it: headings are demoted under the entry's own, fences are
// closed, raw HTML and comments are removed, @mentions become code (no
// notification), and list items use "+" (see listMarker).

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	atxRe       = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	closingRe   = regexp.MustCompile(`(?:^|[ \t]+)#+$`)
	underlineRe = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	listItemRe  = regexp.MustCompile(`^[ \t]*([*+-]|[0-9]{1,9}[.)])([ \t]|$)`)
	bulletRe    = regexp.MustCompile(`^([ \t]*)[*-]([ \t]+)(.*)$`)
	autolinkRe  = regexp.MustCompile(`<((?:https?|mailto):[^\s<>]*)>`)
	// A GitHub @mention: a user or org/team, not after a word character (an
	// e-mail address) or a slash (a path, a URL).
	mentionRe = regexp.MustCompile("(^|[^A-Za-z0-9_`/@.-])@([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9][A-Za-z0-9._-]*)?)")
)

// fences follows fenced code blocks line by line.
type fences struct {
	ch byte // '`' or '~'
	n  int  // length of the opening run; 0 outside a fence
}

// step reads one line and reports whether it is code: inside a fence or one
// of its fence lines. Any indentation is accepted, for fences in list items.
func (f *fences) step(line string) bool {
	t := strings.TrimLeft(line, " \t")
	ch, n := fenceRun(t)
	if f.n == 0 {
		// A backtick fence's info string can't hold a backtick.
		if n >= 3 && (ch == '~' || !strings.Contains(t[n:], "`")) {
			f.ch, f.n = ch, n
			return true
		}
		return false
	}
	if ch == f.ch && n >= f.n && strings.TrimSpace(t[n:]) == "" {
		f.n = 0
	}
	return true
}

// closing is the line that closes the open fence, or "".
func (f *fences) closing() string {
	if f.n == 0 {
		return ""
	}
	return strings.Repeat(string(f.ch), f.n)
}

func fenceRun(t string) (byte, int) {
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return 0, 0
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	return t[0], n
}

// heading reads an ATX heading: its level (0 when the line is none) and text.
func heading(line string) (int, string) {
	m := atxRe.FindStringSubmatch(line)
	if m == nil {
		return 0, ""
	}
	return len(m[1]), strings.TrimSpace(closingRe.ReplaceAllString(m[2], ""))
}

// normalize makes one form of a body: "\n" line ends, no control characters
// but tabs, at most limit bytes.
func normalize(s string, limit int) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == utf8.RuneError || isInvisible(r) {
			return -1
		}
		return r
	}, s)
	return strings.Split(cutBytes(s, limit), "\n")
}

// isInvisible: bidirectional controls and the like, which could make the text
// read differently from what it is.
func isInvisible(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F || r == 0xFEFF
}

// stripComments removes HTML comments outside fenced code. An unclosed one
// runs to the end, as on GitHub. A line that was only comment goes.
func stripComments(lines []string) []string {
	var out []string
	var f fences
	in := false
	for _, l := range lines {
		if !in && f.step(l) {
			out = append(out, l)
			continue
		}
		startedIn := in
		var b strings.Builder
		rest := l
		for rest != "" {
			if in {
				i := strings.Index(rest, "-->")
				if i < 0 {
					rest = ""
					break
				}
				rest, in = rest[i+3:], false
				continue
			}
			i := strings.Index(rest, "<!--")
			if i < 0 {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:i])
			rest, in = rest[i+4:], true
		}
		kept := b.String()
		if strings.TrimSpace(kept) == "" && (startedIn || strings.Contains(l, "<!--")) {
			continue
		}
		out = append(out, kept)
	}
	return out
}

// section finds the first heading outside code whose text matches and returns
// the lines under it, up to the next heading of the same or a higher level,
// and every other line (the heading itself left out).
func section(lines []string, match func(level int, text string) bool) (inside, outside []string, found bool) {
	var f fences
	level := 0 // the matched heading's level while inside its section
	for _, l := range lines {
		if f.step(l) {
			if level > 0 {
				inside = append(inside, l)
			} else {
				outside = append(outside, l)
			}
			continue
		}
		if n, text := heading(l); n > 0 {
			if level > 0 && n <= level {
				level = 0
			} else if level == 0 && !found && match(n, text) {
				level, found = n, true
				continue
			}
		}
		if level > 0 {
			inside = append(inside, l)
		} else {
			outside = append(outside, l)
		}
	}
	return inside, outside, found
}

// isNone reports whether the notes say there are none.
func isNone(lines []string) bool {
	s := strings.ToLower(strings.TrimSpace(strings.Join(lines, " ")))
	s = strings.TrimRight(strings.Trim(s, "*_` "), ".")
	switch s {
	case "", "none", "n/a", "na", "no", "nothing", "-", "—", "no release notes", "no user-visible changes":
		return true
	}
	return false
}

// clean turns the lines of a pull request's notes into Markdown that can go
// under a heading of level top-1: the shallowest heading becomes level top.
// The result is at most limit bytes; "" when nothing is left.
func clean(lines []string, top, limit int) string {
	lines = setext(lines)

	// Move the headings so that the shallowest is at level top.
	minLevel := 7
	var f fences
	for _, l := range lines {
		if !f.step(l) {
			if n, _ := heading(l); n > 0 {
				minLevel = min(minLevel, n)
			}
		}
	}
	shift := top - minLevel

	var out []string
	f = fences{}
	blank := true // collapse runs of blank lines; none at the start
	size := 0
	for _, l := range lines {
		if size > limit {
			break // it is cut below anyway
		}
		size += len(l) + 1
		if f.step(l) {
			out = append(out, l)
			blank = false
			continue
		}
		if strings.TrimSpace(l) == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		if n, text := heading(l); n > 0 {
			out = append(out, strings.Repeat("#", min(6, n+shift))+" "+inline(text)) // n+shift ≥ top
			continue
		}
		out = append(out, listMarker(inline(l)))
	}
	s := strings.TrimSpace(strings.Join(out, "\n"))
	if s == "" {
		return ""
	}
	if len(s) > limit {
		return closeFence(strings.TrimSpace(cutBytes(s, limit))) + "\n\n*The rest is in the pull request.*"
	}
	return closeFence(s)
}

// setext rewrites setext headings ("Title" over "===" or "---") as ATX ones,
// so that clean can demote them. An underline below a paragraph of several
// lines gets a blank line before it: a rule, or the text "===".
func setext(lines []string) []string {
	out := make([]string, 0, len(lines))
	var f fences
	prevCode := false
	for _, l := range lines {
		code := f.step(l)
		if !code && underlineRe.MatchString(l) && len(out) > 0 && !prevCode && plain(out[len(out)-1]) {
			level := "## "
			if strings.Contains(l, "=") {
				level = "# "
			}
			if len(out) == 1 || strings.TrimSpace(out[len(out)-2]) == "" {
				out[len(out)-1] = level + strings.TrimSpace(out[len(out)-1])
				prevCode = false
				continue
			}
			out = append(out, "")
		}
		out = append(out, l)
		prevCode = code
	}
	return out
}

// plain: a line of paragraph text (one a setext underline would turn into a
// heading).
func plain(l string) bool {
	t := strings.TrimSpace(l)
	if t == "" || strings.HasPrefix(t, ">") || listItemRe.MatchString(l) || underlineRe.MatchString(l) {
		return false
	}
	n, _ := heading(l)
	return n == 0
}

// listMarker writes a "*" or "-" list item with "+", which renders the same.
// Flint's "What's New" page (internal/update.ParseNotes) lists the "*" and
// "-" items of a release as its changes: the items of a pull request's notes
// are details, not changes of their own.
func listMarker(l string) string {
	m := bulletRe.FindStringSubmatch(l)
	if m == nil || !strings.ContainsFunc(m[3], func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return l // not an item, or a rule such as "* * *"
	}
	return m[1] + "+" + m[2] + m[3]
}

// inline makes the text of one line safe outside code spans: no raw HTML
// (it is shown as text) and no @mentions (they become code).
func inline(s string) string {
	return outsideCode(s, func(t string) string {
		t = autolinkRe.ReplaceAllString(t, "$1")
		t = strings.ReplaceAll(t, "<", "&lt;")
		return mentionRe.ReplaceAllString(t, "$1`@$2`")
	})
}

// outsideCode applies f to the parts of s that are not code spans.
func outsideCode(s string, f func(string) string) string {
	var b strings.Builder
	start, i := 0, 0
	for i < len(s) {
		if s[i] != '`' {
			i++
			continue
		}
		n := runLen(s, i)
		end := -1
		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			m := runLen(s, j)
			if m == n {
				end = j
				break
			}
			j += m
		}
		if end < 0 {
			i += n // no closing run: the backticks are text
			continue
		}
		b.WriteString(f(s[start:i]))
		b.WriteString(s[i : end+n])
		i = end + n
		start = i
	}
	b.WriteString(f(s[start:]))
	return b.String()
}

func runLen(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == s[i] {
		n++
	}
	return n
}

// closeFence closes a fence left open at the end of s, which would otherwise
// make the rest of the release code.
func closeFence(s string) string {
	var f fences
	for l := range strings.SplitSeq(s, "\n") {
		f.step(l)
	}
	if c := f.closing(); c != "" {
		return s + "\n" + c
	}
	return s
}

// cutBytes cuts s to at most n bytes, at a line end in its second half when
// there is one, else at a character boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, '\n'); i > n/2 {
		return s[:i]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
