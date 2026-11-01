package clickhouse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Error is an exception ClickHouse reported, parsed from "Code: N. DB::Exception: …".
type Error struct {
	Code int
	// Name is the symbolic error name, e.g. SYNTAX_ERROR; empty when the server didn't send one.
	Name    string
	Message string
}

// Error keeps the "code: N, message: …" shape the previous driver produced, so the
// console shows the same text as before.
func (e *Error) Error() string {
	return fmt.Sprintf("code: %d, message: %s", e.Code, e.Message)
}

// codeSessionLocked is SESSION_IS_LOCKED: another request still uses the session.
const codeSessionLocked = 373

var (
	exceptionStart = regexp.MustCompile(`Code: (\d+)\. DB::Exception: `)
	errorName      = regexp.MustCompile(` \(([A-Z][A-Z0-9_]+)\)$`)
)

// versionMarker ends every exception text the server writes.
const versionMarker = " (version "

// parseError extracts the first exception from text (a response body or the tail of a
// broken stream). headerCode, from X-ClickHouse-Exception-Code, backs up the code when the
// text can't be parsed. It returns nil when there is no sign of an exception.
func parseError(text, headerCode string) *Error {
	loc := exceptionStart.FindStringSubmatchIndex(text)
	if loc == nil {
		code, err := strconv.Atoi(strings.TrimSpace(headerCode))
		if err != nil || code == 0 {
			return nil
		}
		return &Error{Code: code, Message: strings.TrimSpace(clip(text, 500))}
	}
	code, _ := strconv.Atoi(text[loc[2]:loc[3]])
	msg := text[loc[1]:]
	// The first exception ends at its version marker; anything after belongs to the next.
	if i := strings.LastIndex(msg, versionMarker); i >= 0 {
		msg = msg[:i]
	}
	if i := strings.Index(msg, "\nStack trace"); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.TrimSpace(msg)
	e := &Error{Code: code}
	if m := errorName.FindStringSubmatchIndex(msg); m != nil {
		e.Name = msg[m[2]:m[3]]
		msg = strings.TrimSpace(msg[:m[0]])
	}
	e.Message = msg
	return e
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// looksLikeException reports whether a single string cell is the exception the server
// writes into the output format when a query fails after the first rows were sent.
func looksLikeException(s string) bool {
	return strings.HasPrefix(s, "Code: ") && strings.Contains(s, ". DB::Exception: ") && strings.Contains(s, versionMarker)
}
