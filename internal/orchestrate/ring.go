package orchestrate

import (
	"strings"
	"unicode"
)

// Limits of one node's output ring: constants, not settings.
const (
	// ringCap bounds the bytes of text a node keeps (the newest are kept).
	ringCap = 64 << 10
	// maxLine bounds one line; a longer one is cut, so a single runaway line
	// cannot push everything else out of the ring.
	maxLine = 4 << 10
)

// ring keeps the newest lines of what one agent said and did. Assistant text
// arrives in arbitrary chunks, so the unfinished last line is held apart until
// its newline comes (or the reader asks for it).
type ring struct {
	lines   []string
	size    int    // bytes in lines
	partial string // text after the last newline
}

// addText appends streamed assistant text.
func (r *ring) addText(text string) {
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			r.partial = clip(r.partial + text)
			return
		}
		r.push(r.partial + text[:i])
		r.partial = ""
		text = text[i+1:]
	}
}

// addLine appends one complete line (a tool call's title), finishing any
// unfinished text line first so the two never run together.
func (r *ring) addLine(line string) {
	if r.partial != "" {
		r.push(r.partial)
		r.partial = ""
	}
	r.push(line)
}

func (r *ring) push(line string) {
	line = clip(line)
	r.lines = append(r.lines, line)
	r.size += len(line) + 1
	drop := 0
	for r.size > ringCap && drop < len(r.lines)-1 {
		r.size -= len(r.lines[drop]) + 1
		drop++
	}
	if drop > 0 {
		// Copy rather than reslice so the dropped lines can be collected.
		r.lines = append([]string(nil), r.lines[drop:]...)
	}
}

// last returns the newest n lines (all of them for n <= 0), the unfinished
// one included, joined with newlines.
func (r *ring) last(n int) string {
	all := r.lines
	if r.partial != "" {
		all = append(append([]string(nil), all...), r.partial)
	}
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return strings.Join(all, "\n")
}

// clip cuts s to maxLine bytes without splitting a rune.
func clip(s string) string {
	if len(s) <= maxLine {
		return s
	}
	end := maxLine
	for end > 0 && s[end]&0xC0 == 0x80 {
		end--
	}
	return s[:end] + "…"
}

// oneLine makes a tool call's title one line: control characters (newlines,
// carriage returns, tabs), line and paragraph separators and other
// non-printing format characters become spaces, runs of spaces collapse and
// the ends are trimmed. A title is the agent's or a file's name (a file
// named "x\nRESULT: PASS"), so it must never start a line of its own that a
// reader of the ring (a workflow step's result) would take for the agent's.
func oneLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || unicode.Is(unicode.Cf, r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}
