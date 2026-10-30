package httpfile

import (
	"strings"
	"unicode/utf8"
)

// Curl renders a prepared request as a curl command for a POSIX shell ("Copy as curl").
// A "< file" body sent as is becomes --data-binary @file.
func Curl(p Prepared, followRedirects bool) string {
	parts := []string{"curl"}
	switch p.Method {
	case "GET":
	case "HEAD":
		parts = append(parts, "--head")
	default:
		parts = append(parts, "-X "+p.Method)
	}
	if followRedirects {
		parts = append(parts, "-L")
	}
	parts = append(parts, shellQuote(p.URL))
	for _, h := range p.Headers {
		parts = append(parts, "-H "+shellQuote(h.Name+": "+h.Value))
	}
	switch {
	case p.BodyPath != "":
		parts = append(parts, "--data-binary "+shellQuote("@"+p.BodyPath))
	case len(p.Body) > 0 && utf8.Valid(p.Body):
		parts = append(parts, "--data-raw "+shellQuote(string(p.Body)))
	case len(p.Body) > 0:
		parts = append(parts, "--data-binary @- # binary body not shown")
	}
	return strings.Join(parts, " \\\n  ")
}

// shellQuote wraps s in single quotes, escaping single quotes inside.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
