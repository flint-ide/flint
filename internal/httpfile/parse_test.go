package httpfile

import (
	"strings"
	"testing"
	"time"
)

const sample = `@host = http://localhost:8080
// a comment

### List users
GET {{host}}/users?page=1
    &size=20
Accept: application/json
# comment between headers

###
# @name create
# @no-redirect
# @timeout 5
POST {{host}}/users HTTP/1.1
Content-Type: application/json

{
  "name": "Ann"
}


### Upload
PUT {{host}}/files/1
Content-Type: application/octet-stream

< ./payload.bin

### Plain URL
https://example.test/ping
`

func TestParseSeveralRequests(t *testing.T) {
	f := Parse(sample)
	if len(f.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", f.Errors)
	}
	if len(f.Vars) != 1 || f.Vars[0] != (Var{Name: "host", Value: "http://localhost:8080", Line: 1}) {
		t.Errorf("vars = %+v", f.Vars)
	}
	if len(f.Requests) != 4 {
		t.Fatalf("got %d requests", len(f.Requests))
	}
	list, create, upload, plain := f.Requests[0], f.Requests[1], f.Requests[2], f.Requests[3]

	if list.Name != "List users" || list.Method != "GET" || list.URL != "{{host}}/users?page=1&size=20" || list.Line != 5 {
		t.Errorf("list = %+v", list)
	}
	if len(list.Headers) != 1 || list.Headers[0] != (Header{Name: "Accept", Value: "application/json", Line: 7}) {
		t.Errorf("list headers = %+v", list.Headers)
	}
	if list.Body != "" || list.Start != 4 || list.End != 9 {
		t.Errorf("list block = %d-%d body %q", list.Start, list.End, list.Body)
	}

	if create.Name != "create" || create.Method != "POST" || create.Version != "HTTP/1.1" {
		t.Errorf("create = %+v", create)
	}
	if !create.NoRedirect || create.Timeout != 5*time.Second || create.NoCookieJar {
		t.Errorf("create directives = %+v", create)
	}
	if create.Body != "{\n  \"name\": \"Ann\"\n}" || create.BodyLine != 17 {
		t.Errorf("create body = %q at %d", create.Body, create.BodyLine)
	}

	if upload.BodyFile != "./payload.bin" || upload.Body != "" || upload.BodyFileVars {
		t.Errorf("upload = %+v", upload)
	}
	if plain.Method != "GET" || plain.URL != "https://example.test/ping" || plain.Name != "Plain URL" {
		t.Errorf("plain = %+v", plain)
	}
}

func TestParseKeepsXMLBodyInline(t *testing.T) {
	f := Parse("POST http://x.test\nContent-Type: application/xml\n\n<root>\n  <a/>\n</root>\n")
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("errors %v, requests %d", f.Errors, len(f.Requests))
	}
	if r := f.Requests[0]; r.BodyFile != "" || r.Body != "<root>\n  <a/>\n</root>" {
		t.Errorf("body = %q, file = %q", r.Body, r.BodyFile)
	}
}

func TestParseBodyFileWithVariables(t *testing.T) {
	r := Parse("POST http://x.test\n\n<@ ./tmpl.json\n").Requests[0]
	if r.BodyFile != "./tmpl.json" || !r.BodyFileVars {
		t.Errorf("got %+v", r)
	}
}

func TestParseCRLF(t *testing.T) {
	f := Parse("GET http://x.test\r\nAccept: */*\r\n\r\nbody\r\n")
	if r := f.Requests[0]; r.Headers[0].Value != "*/*" || r.Body != "body" {
		t.Errorf("got %+v", r)
	}
}

func TestParseErrorsWithLines(t *testing.T) {
	cases := []struct {
		name, src string
		line      int
		contains  string
	}{
		{"unknown method", "GTE http://x.test", 1, "unknown HTTP method"},
		{"missing URL", "### a\nPOST", 2, "needs a URL"},
		{"bad version", "GET http://x.test HTTP/x", 1, "HTTP version"},
		{"space in URL", "GET http://x.test/a b HTTP/1.1", 1, "after the URL"},
		{"bad header", "GET http://x.test\nAccept application/json", 2, "Name: value"},
		{"bad variable", "@ = 1\nGET http://x.test", 1, "@name = value"},
		{"bad timeout", "# @timeout soon\nGET http://x.test", 1, "@timeout"},
		{"empty name", "# @name\nGET http://x.test", 1, "@name"},
		{"file plus text", "POST http://x.test\n\n< ./a.json\nmore", 4, "whole body"},
	}
	for _, c := range cases {
		f := Parse(c.src)
		if len(f.Errors) == 0 {
			t.Errorf("%s: no error", c.name)
			continue
		}
		if e := f.Errors[0]; e.Line != c.line || !strings.Contains(e.Message, c.contains) {
			t.Errorf("%s: got %v, want line %d containing %q", c.name, e, c.line, c.contains)
		}
	}
}

func TestParseErrorDoesNotHideOtherRequests(t *testing.T) {
	f := Parse("### broken\nGET http://x.test\nnot a header\n\n### ok\nGET http://y.test\n")
	if _, err := f.RequestAt(2); err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Errorf("broken request: err = %v", err)
	}
	r, err := f.RequestAt(6)
	if err != nil || r.URL != "http://y.test" {
		t.Errorf("ok request: %+v, %v", r, err)
	}
}

func TestRequestAt(t *testing.T) {
	f := Parse(sample)
	for line, want := range map[int]string{4: "List users", 9: "List users", 10: "create", 21: "create", 22: "Upload", 29: "Plain URL"} {
		r, err := f.RequestAt(line)
		if err != nil || r.Name != want {
			t.Errorf("line %d: got %q, %v; want %q", line, r.Name, err, want)
		}
	}
	if _, err := f.RequestAt(1); err == nil {
		t.Error("the variables above the first ### are not a request")
	}
	if _, err := Parse("@a = 1\n\n### only comments\n# nothing\n").RequestAt(4); err == nil {
		t.Error("a block without a request should fail")
	}
	if _, err := f.RequestAt(999); err == nil {
		t.Error("a line past the end should fail")
	}
}

func TestOutline(t *testing.T) {
	o := OutlineOf(sample + "\n###\nPOST\n")
	if len(o.Requests) != 5 || o.Requests[1] != (Mark{Line: 14, Name: "create", Method: "POST", URL: "{{host}}/users"}) {
		t.Errorf("requests = %+v", o.Requests)
	}
	if len(o.Errors) != 1 || o.Errors[0].Line != 32 {
		t.Errorf("errors = %+v", o.Errors)
	}
	if empty := OutlineOf(""); empty.Requests == nil || empty.Errors == nil {
		t.Error("empty outline must use empty slices, not nil (the frontend expects arrays)")
	}
}
