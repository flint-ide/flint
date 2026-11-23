package cli

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Table aligns rows into columns two spaces apart (the last column is not
// padded); header, when set, is the first row. Cells are cleaned (Clean, and
// line ends become spaces), so a title from a file cannot break the layout.
func Table(header []string, rows [][]string) string {
	all := rows
	if header != nil {
		all = append([][]string{header}, rows...)
	}
	var widths []int
	clean := make([][]string, len(all))
	for i, r := range all {
		clean[i] = make([]string, len(r))
		for j, cell := range r {
			cell = strings.Join(strings.Fields(Clean(cell)), " ")
			if j == 0 {
				cell = leading(r[0]) + cell // keep an indent
			}
			clean[i][j] = cell
			if j >= len(widths) {
				widths = append(widths, 0)
			}
			widths[j] = max(widths[j], utf8.RuneCountInString(cell))
		}
	}
	var b strings.Builder
	for _, r := range clean {
		for j, cell := range r {
			if j == len(r)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(cell)+2))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func leading(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " "))] }

// Clean removes what a terminal would act on: control characters other than
// tab and newline (escape sequences start with one), C1 controls, and
// invisible formatting characters such as bidi overrides. Text from
// repositories and agents passes through it before it is printed.
func Clean(s string) string {
	if !needsClean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == utf8.RuneError, unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsClean(s string) bool {
	for _, r := range s {
		if (r != '\n' && r != '\t' && unicode.IsControl(r)) || r == utf8.RuneError || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// Cut shortens s to n characters at most, with an ellipsis.
func Cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:max(n-1, 0)]) + "…"
}
