// Package httpfile parses .http / .rest request files (the format of IntelliJ's HTTP
// Client) and sends their requests with net/http, one at a time and only when asked.
//
//	@host = http://localhost:8080
//
//	### Create user
//	# @name createUser
//	POST {{host}}/users
//	Content-Type: application/json
//
//	{"id": "{{$uuid}}"}
package httpfile

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Header is one request or response header, in file (or wire) order.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Line is the 1-based line in the .http file (0 for response headers).
	Line int `json:"line,omitempty"`
}

// Var is an in-file variable ("@name = value").
type Var struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

// Request is one request of a file, before variables are substituted.
type Request struct {
	// Name comes from "### name" or "# @name name"; "" when unnamed.
	Name    string   `json:"name"`
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Version string   `json:"version,omitempty"`
	Headers []Header `json:"headers"`
	// Body is the inline body; BodyFile a "< path" reference relative to the file
	// ("<@ path" also substitutes variables in it: BodyFileVars).
	Body         string `json:"body,omitempty"`
	BodyFile     string `json:"bodyFile,omitempty"`
	BodyFileVars bool   `json:"bodyFileVars,omitempty"`
	// Directives: "# @no-redirect", "# @no-cookie-jar", "# @timeout 60" (seconds or 1m).
	NoRedirect  bool          `json:"noRedirect,omitempty"`
	NoCookieJar bool          `json:"noCookieJar,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	// Line is the request line; BodyLine the first body line (0 without a body).
	Line     int `json:"line"`
	BodyLine int `json:"bodyLine,omitempty"`
	// Start and End delimit the request's block (from its "###" to the next one).
	Start int `json:"start"`
	End   int `json:"end"`
}

// Error is a problem at a 1-based line of the file.
type Error struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func (e Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// File is a parsed .http file. Parsing never stops at an error: a broken request
// doesn't keep the others from being sent.
type File struct {
	Vars     []Var     `json:"vars"`
	Requests []Request `json:"requests"`
	Errors   []Error   `json:"errors"`
	// blocks are the "###"-delimited ranges, including those without a request.
	blocks []span
}

type span struct{ start, end int }

var (
	methods = map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
		"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
	}
	upperWord   = regexp.MustCompile(`^[A-Z]+$`)
	varName     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	headerName  = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	httpVersion = regexp.MustCompile(`^HTTP/\d(\.\d)?$`)
	// bodyFileRef is "< path" or "<@ path"; the space keeps XML bodies ("<root>") inline.
	bodyFileRef = regexp.MustCompile(`^<(@?)\s+(\S.*)$`)
)

type state int

const (
	beforeRequest state = iota
	inHeaders
	inBody
)

// parser holds the block being read.
type parser struct {
	file    File
	state   state
	start   int // first line of the current block
	name    string
	req     *Request
	body    []string
	sawHead bool // a header was read (URL continuation lines end there)
	// directives seen before the request line
	noRedirect, noCookieJar bool
	timeout                 time.Duration
}

// Parse reads a .http file. Line numbers in the result are 1-based.
func Parse(src string) File {
	p := &parser{start: 1}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i, line := range lines {
		p.line(i+1, line)
	}
	p.finish(len(lines))
	if p.file.Vars == nil {
		p.file.Vars = []Var{}
	}
	if p.file.Requests == nil {
		p.file.Requests = []Request{}
	}
	if p.file.Errors == nil {
		p.file.Errors = []Error{}
	}
	return p.file
}

func (p *parser) fail(n int, format string, args ...any) {
	p.file.Errors = append(p.file.Errors, Error{Line: n, Message: fmt.Sprintf(format, args...)})
}

func (p *parser) line(n int, line string) {
	if strings.HasPrefix(line, "###") {
		p.finish(n - 1)
		*p = parser{file: p.file, start: n, name: strings.TrimSpace(strings.TrimLeft(line, "#"))}
		return
	}
	trimmed := strings.TrimSpace(line)
	switch p.state {
	case beforeRequest:
		switch {
		case trimmed == "":
		case isComment(trimmed):
			p.directive(n, trimmed)
		case strings.HasPrefix(trimmed, "@"):
			p.variable(n, trimmed)
		default:
			p.requestLine(n, trimmed)
		}
	case inHeaders:
		switch {
		case trimmed == "":
			p.state = inBody
		case isComment(trimmed):
		case !p.sawHead && line != trimmed && (trimmed[0] == '?' || trimmed[0] == '&'):
			// Long query strings may continue on indented lines.
			p.req.URL += trimmed
		default:
			p.header(n, trimmed)
		}
	case inBody:
		if len(p.body) == 0 && trimmed == "" {
			return
		}
		if len(p.body) == 0 {
			p.req.BodyLine = n
		}
		p.body = append(p.body, line)
	}
}

func isComment(s string) bool { return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") }

// directive handles "# @name x", "# @no-redirect", "# @no-cookie-jar" and "# @timeout 30".
func (p *parser) directive(n int, comment string) {
	text := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(comment, "//"), "#"))
	if !strings.HasPrefix(text, "@") {
		return // a plain comment
	}
	key, value, _ := strings.Cut(text[1:], " ")
	if k, v, ok := strings.Cut(key, "="); ok { // "@name=value"
		key, value = k, v
	}
	value = strings.TrimSpace(value)
	switch key {
	case "name":
		if value == "" {
			p.fail(n, "@name needs a value")
			return
		}
		p.name = value
	case "no-redirect":
		p.noRedirect = true
	case "no-cookie-jar":
		p.noCookieJar = true
	case "timeout":
		d, err := parseTimeout(value)
		if err != nil {
			p.fail(n, "%v", err)
			return
		}
		p.timeout = d
	}
	// Other directives (IntelliJ has more) are comments here.
}

// parseTimeout accepts seconds ("60") or a Go duration ("1m30s").
func parseTimeout(s string) (time.Duration, error) {
	if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second, nil
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d, nil
	}
	return 0, fmt.Errorf("@timeout needs a positive number of seconds or a duration like 1m, got %q", s)
}

func (p *parser) variable(n int, line string) {
	name, value, ok := strings.Cut(line[1:], "=")
	name = strings.TrimSpace(name)
	if !ok || !varName.MatchString(name) {
		p.fail(n, `expected a variable "@name = value"`)
		return
	}
	p.file.Vars = append(p.file.Vars, Var{Name: name, Value: strings.TrimSpace(value), Line: n})
}

func (p *parser) requestLine(n int, line string) {
	fields := strings.Fields(line)
	method, rest := "GET", fields
	if upperWord.MatchString(fields[0]) && len(fields) > 1 || methods[fields[0]] {
		if !methods[fields[0]] {
			p.fail(n, "unknown HTTP method %q", fields[0])
		}
		method, rest = fields[0], fields[1:]
	}
	req := &Request{Name: p.name, Method: method, Line: n, Start: p.start, Headers: []Header{},
		NoRedirect: p.noRedirect, NoCookieJar: p.noCookieJar, Timeout: p.timeout}
	switch {
	case len(rest) == 0:
		p.fail(n, "%s needs a URL", method)
	case len(rest) > 2:
		p.fail(n, "unexpected %q after the URL (spaces in a URL must be encoded as %%20)", strings.Join(rest[1:], " "))
	case len(rest) == 2 && !httpVersion.MatchString(rest[1]):
		p.fail(n, "expected an HTTP version like HTTP/1.1 after the URL, got %q", rest[1])
	}
	if len(rest) > 0 {
		req.URL = rest[0]
	}
	if len(rest) == 2 {
		req.Version = rest[1]
	}
	p.req = req
	p.state = inHeaders
}

func (p *parser) header(n int, line string) {
	name, value, ok := strings.Cut(line, ":")
	name = strings.TrimSpace(name)
	if !ok || !headerName.MatchString(name) {
		p.fail(n, `expected a header "Name: value" (or a blank line before the body)`)
		return
	}
	p.sawHead = true
	p.req.Headers = append(p.req.Headers, Header{Name: name, Value: strings.TrimSpace(value), Line: n})
}

// finish closes the current block at line end.
func (p *parser) finish(end int) {
	if end < p.start {
		return // "###" on the first line: nothing before it
	}
	p.file.blocks = append(p.file.blocks, span{p.start, end})
	if p.req == nil {
		return
	}
	req := *p.req
	req.End = end
	body := trimTrailingBlank(p.body)
	if len(body) > 0 {
		if m := bodyFileRef.FindStringSubmatch(strings.TrimSpace(body[0])); m != nil {
			if len(body) > 1 {
				p.fail(req.BodyLine+1, "a body file reference (< path) must be the whole body")
			}
			req.BodyFile, req.BodyFileVars = strings.TrimSpace(m[2]), m[1] == "@"
		} else {
			req.Body = strings.Join(body, "\n")
		}
	}
	p.file.Requests = append(p.file.Requests, req)
}

func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// RequestAt returns the request whose block contains line, failing with the block's
// first parse error when it has one.
func (f File) RequestAt(line int) (Request, error) {
	block, ok := span{}, false
	for _, b := range f.blocks {
		if line >= b.start && line <= b.end {
			block, ok = b, true
			break
		}
	}
	if !ok {
		return Request{}, fmt.Errorf("no request at line %d", line)
	}
	for _, e := range f.Errors {
		if e.Line >= block.start && e.Line <= block.end {
			return Request{}, e
		}
	}
	for _, r := range f.Requests {
		if r.Start == block.start {
			return r, nil
		}
	}
	return Request{}, fmt.Errorf("no request at line %d", line)
}
