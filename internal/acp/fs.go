package acp

import (
	"errors"
	"strings"
)

// ReadTextFileRequest is the params of `fs/read_text_file`. Line is 1-based;
// nil Line/Limit mean "from the start" / "to the end".
type ReadTextFileRequest struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

// WriteTextFileRequest is the params of `fs/write_text_file`.
type WriteTextFileRequest struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

type readTextFileResponse struct {
	Content string `json:"content"`
}

// writeTextFileResponse marshals to the schema's empty object.
type writeTextFileResponse struct{}

func (r ReadTextFileRequest) validate() error {
	switch {
	case r.SessionID == "":
		return errors.New("sessionId is required")
	case r.Path == "":
		return errors.New("path is required")
	case r.Line != nil && *r.Line < 0, r.Limit != nil && *r.Limit < 0:
		return errors.New("line and limit must be non-negative")
	}
	return nil
}

func (r WriteTextFileRequest) validate() error {
	switch {
	case r.SessionID == "":
		return errors.New("sessionId is required")
	case r.Path == "":
		return errors.New("path is required")
	}
	return nil
}

// ApplyLineWindow returns the slice of content selected by req.Line
// (1-based start line) and req.Limit (maximum number of lines), keeping
// line terminators intact. Handlers can use it to honour fs/read_text_file
// windows after loading the full text (including unsaved buffers).
func ApplyLineWindow(content string, req ReadTextFileRequest) string {
	if req.Line == nil && req.Limit == nil {
		return content
	}
	lines := strings.SplitAfter(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // SplitAfter yields a trailing "" after a final newline
	}
	start := 0
	if req.Line != nil && *req.Line > 1 {
		start = *req.Line - 1
	}
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if req.Limit != nil && start+*req.Limit < end {
		end = start + *req.Limit
	}
	return strings.Join(lines[start:end], "")
}
