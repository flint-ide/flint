package prompts

import (
	"regexp"
	"slices"
	"strings"
)

// placeholder matches {{name}}, with optional spaces inside the braces.
var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_-]*)\s*\}\}`)

var blankRuns = regexp.MustCompile(`\n{3,}`)

// Placeholders lists the names a body uses, once each, in order.
func Placeholders(body string) []string {
	out := []string{}
	for _, m := range placeholder.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// Expand fills the placeholders of body from vars in one pass: text that a
// value brings in is never expanded again, so a selection holding "{{diff}}"
// stays as it is. Unknown placeholders are left in place for the user to see.
// Runs of blank lines left by empty values shrink to one.
func Expand(body string, vars map[string]string) string {
	out := placeholder.ReplaceAllStringFunc(body, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
	return strings.TrimSpace(blankRuns.ReplaceAllString(out, "\n\n"))
}

// Fenced puts text in a Markdown code block whose fence is longer than any
// backtick run inside it; empty text stays empty.
func Fenced(text, lang string) string {
	if text == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + lang + "\n" + strings.TrimRight(text, "\n") + "\n" + fence
}
