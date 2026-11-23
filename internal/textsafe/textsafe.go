// Package textsafe holds the text rules that features share: which characters
// a reader does not see, how to keep text inside a fenced block from closing
// it, and the form of a name. Pure, no dependencies.
package textsafe

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// HiddenRune finds a character that a reader does not see or that breaks a
// line where the panel shows one: control characters other than tab and
// newline, the line and paragraph separators, Unicode tags, bidirectional
// controls and zero-width characters (the rules of project items).
//
// Zero-width joiner and non-joiner (U+200D, U+200C) are allowed: emoji and
// several scripts need them.
//
// The line and paragraph separators (U+2028, U+2029) are refused too: many
// readers (an agent, a dialog) break the line there while the panel shows one
// line, so a title could imitate a line of its own. NEL (U+0085) is a control
// character.
func HiddenRune(s string) (rune, bool) {
	for _, r := range s {
		if hidden(r) {
			return r, true
		}
	}
	return 0, false
}

func hidden(r rune) bool {
	switch {
	case r == '\t' || r == '\n':
		return false
	case unicode.IsControl(r),
		r == 0x2028, r == 0x2029,
		r >= 0xE0000 && r <= 0xE007F,
		r >= 0x202A && r <= 0x202E,
		r >= 0x2066 && r <= 0x2069,
		r == 0x200B, r == 0x2060, r == 0xFEFF:
		return true
	}
	return false
}

// RuneKind names a character HiddenRune found, for a problem's reason: "a
// control character (U+0007)", "a line or paragraph separator (U+2028)" or
// "invisible characters (U+200B)".
func RuneKind(r rune) string {
	switch {
	case unicode.IsControl(r):
		return fmt.Sprintf("a control character (U+%04X)", r)
	case r == 0x2028 || r == 0x2029:
		return fmt.Sprintf("a line or paragraph separator (U+%04X)", r)
	}
	return fmt.Sprintf("invisible characters (U+%04X)", r)
}

// Plain replaces every character HiddenRune would refuse with a space (a
// carriage return that ends a line as "\r\n" is dropped), so text that is
// stored or sent on (a script's output, a reviewer's findings) keeps its tabs
// and newlines and nothing else that hides or breaks lines.
func Plain(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if hidden(r) {
			return ' '
		}
		return r
	}, s)
}

// fenceRun is how many "=" (or full-width "＝") start a fence line.
const fenceRun = 5

var lineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u0085", "\n", " ", "\n", " ", "\n")

// FenceSafe puts "> " in front of every line that starts with a run of five
// "=" so no text can close or imitate one of Flint's fenced sections. What
// counts as a line and as its start is what a reader sees: "\r", NEL (U+0085)
// and the line and paragraph separators (U+2028, U+2029) break lines (and are
// written as "\n" when any line is quoted); all Unicode white space (NBSP
// among it) and invisible format characters (zero-width spaces, joiners,
// bidirectional controls, tags) in front of a line are skipped; and the
// full-width "＝" (U+FF1D) counts as "=".
func FenceSafe(s string) string {
	if !strings.ContainsAny(s, "=＝") {
		return s
	}
	lines := strings.Split(lineBreaks.Replace(s), "\n")
	quoted := false
	for i, l := range lines {
		if fenceLine(l) {
			lines[i] = "> " + l
			quoted = true
		}
	}
	if !quoted {
		return s
	}
	return strings.Join(lines, "\n")
}

// fenceLine reports whether l, past its invisible start, begins with a run of
// fenceRun equals signs of either width.
func fenceLine(l string) bool {
	l = strings.TrimLeftFunc(l, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	n := 0
	for _, r := range l {
		if r != '=' && r != '＝' {
			break
		}
		if n++; n == fenceRun {
			return true
		}
	}
	return false
}

// slugName is the form of a workflow's name, a schedule's name and a project
// item's workflow key.
var slugName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidSlugName reports whether name is made of lowercase letters, digits and
// dashes, starts with a letter or digit and has at most 64 characters.
func ValidSlugName(name string) bool { return slugName.MatchString(name) }
