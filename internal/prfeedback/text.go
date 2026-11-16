package prfeedback

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"flint/internal/redact"
)

// stripControl drops control and format characters (bidi overrides and
// zero-width characters hide or reorder text), as internal/review does for an
// agent's findings. Multi-line text keeps its line breaks and tabs.
func stripControl(s string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			if multiline {
				return r
			}
			return ' '
		case r == '\r' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError || r == ' ' || r == ' ':
			return -1
		}
		return r
	}, s)
}

// oneLine makes a name safe to state in the draft: one line, no control or
// format characters, collapsed spaces, at most limit runes.
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(stripControl(s, false)), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

// login is a comment's author; GitHub sends none for a deleted account.
func login(s string) string {
	if s = oneLine(s, maxNameRunes); s == "" {
		return "ghost"
	}
	return s
}

func path(p string) string { return oneLine(p, maxPathRunes) }

// cleanBody prepares what a reviewer wrote: no control or format characters,
// secrets redacted, trimmed and cut to limit bytes.
func cleanBody(s string, limit int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = capLen(strings.TrimSpace(redactCapped(stripControl(s, true), limit)), limit)
	if s == "" {
		return "(no text)"
	}
	return s
}

// cleanCode prepares lines of code (the working tree's, or a diff's): long
// lines cut, control characters dropped, secrets redacted.
func cleanCode(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		l = stripControl(l, true)
		if r := []rune(l); len(r) > maxLineRunes {
			l = string(r[:maxLineRunes]) + "…"
		}
		lines[i] = l
	}
	return redact.String(strings.Join(lines, "\n"))
}

// redactCapped redacts s cut to a little more than limit first: redaction
// costs time with the text's length, and the margin keeps a secret that the
// final cut splits from being left half visible.
func redactCapped(s string, limit int) string {
	if n := limit + redactMargin; len(s) > n {
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n]
	}
	return redact.String(s)
}

func capLen(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// quote marks every line of s as copied text.
func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

var backticks = regexp.MustCompile("`+")

// fenced wraps text in a code fence longer than any run of backticks in it.
func fenced(info, text string) string {
	n := 3
	for _, run := range backticks.FindAllString(text, -1) {
		n = max(n, len(run)+1)
	}
	fence := strings.Repeat("`", n)
	return fence + info + "\n" + text + "\n" + fence
}

// suggestionOpen matches the opening fence of a GitHub suggestion block:
// ```suggestion (three or more backticks or tildes, up to three spaces in).
var suggestionOpen = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*suggestion(?:[^A-Za-z0-9_].*)?$")

// suggestionMark stands where a suggestion was in the comment.
const suggestionMark = "[suggested change, shown below]"

// splitSuggestions takes the suggestion blocks out of a comment: body is
// the comment with a mark in each one's place, suggestions their code (""
// suggests deleting the lines). A block the comment never closes runs to its
// end, as GitHub renders it.
func splitSuggestions(text string) (body string, suggestions []string) {
	var out, code []string
	fence := ""
	flush := func() {
		suggestions = append(suggestions, capLen(strings.Join(code, "\n"), maxSuggestionBytes))
		out = append(out, suggestionMark)
		code, fence = nil, ""
	}
	for _, l := range strings.Split(text, "\n") {
		if fence == "" {
			if m := suggestionOpen.FindStringSubmatch(l); m != nil {
				fence = m[1]
			} else {
				out = append(out, l)
			}
			continue
		}
		if t := strings.TrimSpace(l); strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
			flush()
			continue
		}
		code = append(code, l)
	}
	if fence != "" {
		flush()
	}
	return strings.TrimSpace(strings.Join(out, "\n")), suggestions
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// hunkAround cuts the lines from..to of one side (left: the base version)
// out of a file's patch, with context lines of the diff around them; ""
// when the patch has no such lines.
func hunkAround(patch string, left bool, from, to, context int) string {
	lines := strings.Split(patch, "\n")
	oldN, newN := 0, 0
	start, first, last := -1, -1, -1 // the hunk's header, the first and last matching line
	end := len(lines)
	for i, l := range lines {
		if m := hunkHeader.FindStringSubmatch(l); m != nil {
			if first >= 0 {
				end = i
				break
			}
			oldN, _ = strconv.Atoi(m[1])
			newN, _ = strconv.Atoi(m[2])
			start = i
			continue
		}
		if start < 0 || strings.HasPrefix(l, `\`) {
			continue
		}
		n, onSide := newN, true
		switch {
		case strings.HasPrefix(l, "+"):
			onSide = !left
			newN++
		case strings.HasPrefix(l, "-"):
			n, onSide = oldN, left
			oldN++
		default:
			if left {
				n = oldN
			}
			oldN++
			newN++
		}
		if onSide && n >= from && n <= to {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return ""
	}
	a, b := max(start+1, first-context), min(end, last+context+1)
	header := lines[start]
	if a > start+1 {
		header = "@@ … @@"
	}
	return strings.Join(append([]string{header}, lines[a:b]...), "\n")
}
