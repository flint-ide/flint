package mcptools

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// PlainText renders terminal output as plain lines: escape sequences
// (colors, cursor moves, titles, hyperlinks) are dropped, a carriage return
// starts its line over (progress bars keep their last state), backspace
// erases, and other control characters go. Trailing blanks are trimmed.
func PlainText(b []byte) string {
	var out strings.Builder
	var line []byte
	flush := func() {
		out.WriteString(strings.TrimRight(string(line), " \t"))
		out.WriteByte('\n')
		line = line[:0]
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == 0x1b:
			i = skipEscape(b, i)
		case c == '\n':
			flush()
		case c == '\r':
			if i+1 < len(b) && b[i+1] == '\n' {
				continue // CRLF: the \n ends the line
			}
			line = line[:0]
		case c == '\b':
			if len(line) > 0 {
				_, size := utf8.DecodeLastRune(line)
				line = line[:len(line)-size]
			}
		case c == '\t' || c >= 0x20 && c != 0x7f:
			line = append(line, c)
		}
	}
	if len(line) > 0 {
		flush()
	}
	return strings.TrimRight(out.String(), "\n")
}

// skipEscape returns the index of the last byte of the escape sequence that
// starts at b[i] (an ESC).
func skipEscape(b []byte, i int) int {
	if i+1 >= len(b) {
		return i
	}
	switch b[i+1] {
	case '[': // CSI: parameters, then one final byte in 0x40–0x7e
		for j := i + 2; j < len(b); j++ {
			if b[j] >= 0x40 && b[j] <= 0x7e {
				return j
			}
		}
		return len(b) - 1
	case ']', 'P', 'X', '^', '_': // OSC / DCS / SOS / PM / APC: until BEL or ESC \
		for j := i + 2; j < len(b); j++ {
			if b[j] == 0x07 {
				return j
			}
			if b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\' {
				return j + 1
			}
		}
		return len(b) - 1
	case '(', ')', '*', '+', '-', '.', '/', '#', '%': // charset and similar: one more byte
		return min(i+2, len(b)-1)
	}
	return i + 1
}

// lastLines returns the last n lines of text.
func lastLines(text string, n int) string {
	if n <= 0 {
		return ""
	}
	end := len(text)
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == '\n' {
			n--
			if n == 0 {
				return text[i+1 : end]
			}
		}
	}
	return text
}

// within reports whether path is root or below it (both absolute and clean).
func within(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// rel shows path relative to root when it lies inside it.
func rel(path, root string) string {
	if r, err := filepath.Rel(root, path); err == nil && within(path, root) {
		return r
	}
	return path
}

// cut bounds s to n bytes without splitting a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// tailBytes keeps the end of s within n bytes, starting at a whole line.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
