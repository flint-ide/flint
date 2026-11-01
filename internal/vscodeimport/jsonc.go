package vscodeimport

import (
	"bytes"
	"errors"
)

// StripJSONC turns VS Code's "JSON with comments" into plain JSON: it removes
// // and /* */ comments and commas before a closing } or ], outside strings.
// Newlines inside comments are kept so error offsets stay on the right line.
func StripJSONC(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	out := make([]byte, 0, len(data))
	// pendingComma is the index in out of a comma that is dropped if the next
	// significant character closes an object or array.
	pendingComma := -1
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '"':
			end, err := stringEnd(data, i)
			if err != nil {
				return nil, err
			}
			out = append(out, data[i:end]...)
			i = end - 1
			pendingComma = -1
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i-- // the newline is copied by the next iteration
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			end := bytes.Index(data[i+2:], []byte("*/"))
			if end < 0 {
				return nil, errors.New("unterminated /* comment")
			}
			for _, b := range data[i+2 : i+2+end] {
				if b == '\n' {
					out = append(out, '\n')
				}
			}
			i += end + 3
		case c == ',':
			pendingComma = len(out)
			out = append(out, c)
		case c == '}' || c == ']':
			if pendingComma >= 0 {
				out = append(out[:pendingComma], out[pendingComma+1:]...)
			}
			pendingComma = -1
			out = append(out, c)
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				pendingComma = -1
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// stringEnd returns the index just past the string literal starting at data[start].
func stringEnd(data []byte, start int) (int, error) {
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		}
	}
	return 0, errors.New("unterminated string")
}
