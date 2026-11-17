package backlog

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/prompts"
)

// SetStatus returns data with its status set: only the "status:" line of the
// front matter changes (or one is added before the closing "---", or front
// matter is added when the file has none); every other byte stays, with the
// file's line endings. A file it cannot change safely (no closing "---", a
// line that is not "key: value", two status lines) is refused.
func SetStatus(data []byte, status string) ([]byte, error) {
	if !slices.Contains(Statuses, status) {
		return nil, fmt.Errorf("unknown status %q", status)
	}
	if !utf8.Valid(data) {
		return nil, errors.New("not UTF-8 text")
	}
	before, body, err := prompts.FrontMatter(data)
	if err != nil {
		return nil, err
	}
	out, err := setStatus(data, status)
	if err != nil {
		return nil, err
	}
	// What was meant is what was done: the same fields, with the new status.
	after, afterBody, err := prompts.FrontMatter(out)
	if err != nil || afterBody != body || !sameFieldsBut(before, after, status) {
		return nil, errors.New("the file's front matter cannot be changed safely: set the status in the editor")
	}
	return out, nil
}

func setStatus(data []byte, status string) ([]byte, error) {
	line := "status: " + status
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	var open string
	switch {
	case bytes.HasPrefix(data, []byte("---\n")):
		open, eol = "---\n", "\n"
	case bytes.HasPrefix(data, []byte("---\r\n")):
		open, eol = "---\r\n", "\r\n"
	default:
		// No front matter: add one.
		return slices.Concat([]byte("---"+eol+line+eol+"---"+eol), data), nil
	}
	found := -1 // offset of the status line
	foundEnd := 0
	for pos := len(open); pos < len(data); {
		end := bytes.IndexByte(data[pos:], '\n')
		next := len(data)
		if end >= 0 {
			next = pos + end + 1
		}
		raw := data[pos:next]
		content := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
		if content == "---" {
			// The added line ends like the line before it.
			if found < 0 {
				return slices.Concat(data[:pos], []byte(line+eol), data[pos:]), nil
			}
			return replaceValue(data, found, foundEnd, status), nil
		}
		trimmed := strings.TrimSpace(content)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			key, _, ok := strings.Cut(trimmed, ":")
			if !ok {
				return nil, fmt.Errorf("front matter line %q is not key: value", trimmed)
			}
			if strings.EqualFold(strings.TrimSpace(key), "status") {
				if found >= 0 {
					return nil, errors.New("the front matter has two status lines")
				}
				found, foundEnd = pos, pos+len(content)
			}
		}
		if bytes.HasSuffix(raw, []byte("\r\n")) {
			eol = "\r\n"
		} else if bytes.HasSuffix(raw, []byte("\n")) {
			eol = "\n"
		}
		pos = next
	}
	return nil, errors.New("front matter has no closing ---")
}

// replaceValue replaces the value of the "key: value" line data[start:end]
// (without its line ending) with value, keeping the key and the spaces after
// the colon.
func replaceValue(data []byte, start, end int, value string) []byte {
	text := string(data[start:end])
	keep := strings.IndexByte(text, ':') + 1
	for keep < len(text) && (text[keep] == ' ' || text[keep] == '\t') {
		keep++
	}
	if keep == strings.IndexByte(text, ':')+1 {
		value = " " + value // "status:doing" becomes "status: review"
	}
	return slices.Concat(data[:start+keep], []byte(value), data[end:])
}

// sameFieldsBut reports whether after is before with the status set (or
// added when before had none).
func sameFieldsBut(before, after []prompts.Field, status string) bool {
	drop := func(fs []prompts.Field) []prompts.Field {
		return slices.DeleteFunc(slices.Clone(fs), func(f prompts.Field) bool { return f.Key == "status" })
	}
	n := 0
	for _, f := range after {
		if f.Key == "status" {
			if f.Value != status {
				return false
			}
			n++
		}
	}
	return n == 1 && slices.Equal(drop(before), drop(after))
}
