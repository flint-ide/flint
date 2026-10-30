package httpfile

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

var fixedNow = func() time.Time { return time.Unix(1_700_000_000, 0) }

// project writes files into a temp folder and returns the path of api.http in it.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "api.http")
}

func prepare(t *testing.T, path, text string, line int, env string) Prepared {
	t.Helper()
	p, err := Prepare(Target{Path: path, Text: text, Line: line, Env: env}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrepareFileVariables(t *testing.T) {
	text := "@host = localhost:8080\n@base = {{host}}/api\n@token = abc\n\nGET {{base}}/users\nAuthorization: Bearer {{ token }}\n\n{\"at\": {{$timestamp}}}\n"
	p := prepare(t, project(t, nil), text, 5, "")
	if p.URL != "http://localhost:8080/api/users" {
		t.Errorf("URL = %q (no scheme means http://)", p.URL)
	}
	if p.Headers[0] != (Header{Name: "Authorization", Value: "Bearer abc"}) {
		t.Errorf("header = %+v", p.Headers[0])
	}
	if string(p.Body) != `{"at": 1700000000}` {
		t.Errorf("body = %q", p.Body)
	}
	if p.Key != "GET {{base}}/users" {
		t.Errorf("key = %q", p.Key)
	}
}

func TestPrepareVariableScopeIsAboveTheRequest(t *testing.T) {
	text := "@v = one\n###\nGET http://x.test/{{v}}\n\n###\n@v = two\nGET http://x.test/{{v}}\n"
	if p := prepare(t, project(t, nil), text, 3, ""); p.URL != "http://x.test/one" {
		t.Errorf("first = %q", p.URL)
	}
	if p := prepare(t, project(t, nil), text, 7, ""); p.URL != "http://x.test/two" {
		t.Errorf("second = %q", p.URL)
	}
}

func TestPrepareEnvironments(t *testing.T) {
	path := project(t, map[string]string{
		EnvFile: `{
			"$shared": {"version": "v1", "user": "shared"},
			"dev": {"host": "http://dev.test", "port": 8080, "debug": true},
			"prod": {"host": "https://prod.test"}
		}`,
		PrivateEnvFile: `{"dev": {"token": "dev-secret", "user": "me"}}`,
	})
	text := "GET {{host}}:{{port}}/{{version}}?debug={{debug}}&user={{user}}\nAuthorization: {{token}}\n"
	p := prepare(t, path, text, 1, "dev")
	if p.URL != "http://dev.test:8080/v1?debug=true&user=me" {
		t.Errorf("URL = %q (private wins over public, env over $shared)", p.URL)
	}
	if p.Headers[0].Value != "dev-secret" {
		t.Errorf("token = %q", p.Headers[0].Value)
	}

	names, err := Environments(path)
	if err != nil || !slices.Equal(names, []string{"dev", "prod"}) {
		t.Errorf("environments = %v, %v", names, err)
	}

	_, err = Prepare(Target{Path: path, Text: text, Line: 1, Env: "prod"}, fixedNow)
	if err == nil || !strings.Contains(err.Error(), `undefined variable "port" in environment "prod"`) {
		t.Errorf("prod: err = %v", err)
	}
	_, err = Prepare(Target{Path: path, Text: text, Line: 1, Env: "staging"}, fixedNow)
	if err == nil || !strings.Contains(err.Error(), `"staging" is not defined`) {
		t.Errorf("unknown env: err = %v", err)
	}
	_, err = Prepare(Target{Path: path, Text: text, Line: 1}, fixedNow)
	if err == nil || !strings.Contains(err.Error(), "select an HTTP environment") {
		t.Errorf("no env: err = %v", err)
	}
}

func TestFileVariablesOverrideEnvironment(t *testing.T) {
	path := project(t, map[string]string{EnvFile: `{"dev": {"host": "http://env.test", "path": "/{{id}}"}}`})
	text := "@host = http://file.test\n@id = 7\nGET {{host}}{{path}}\n"
	if p := prepare(t, path, text, 3, "dev"); p.URL != "http://file.test/7" {
		t.Errorf("URL = %q", p.URL)
	}
}

func TestEnvironmentFileErrors(t *testing.T) {
	path := project(t, map[string]string{EnvFile: "{\n  \"dev\": {\n    \"a\": 1,\n  }\n}"})
	_, err := Environments(path)
	if err == nil || !strings.Contains(err.Error(), EnvFile+":4") {
		t.Errorf("err = %v (want file and line)", err)
	}
	path = project(t, map[string]string{EnvFile: `{"dev": "secret-value"}`})
	_, err = Environments(path)
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Errorf("err = %v (must not echo values)", err)
	}
	if names, err := Environments(project(t, nil)); err != nil || len(names) != 0 || names == nil {
		t.Errorf("no env files: %v, %v", names, err)
	}
}

func TestDynamicVariables(t *testing.T) {
	text := "POST http://x.test/{{$uuid}}\n\n{{$randomInt}} {{$uuid}} {{$isoTimestamp}}\n"
	p := prepare(t, project(t, nil), text, 1, "")
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if id := strings.TrimPrefix(p.URL, "http://x.test/"); !uuid.MatchString(id) {
		t.Errorf("uuid = %q", id)
	}
	fields := strings.Fields(string(p.Body))
	if n, err := strconv.Atoi(fields[0]); err != nil || n < 0 || n > 1000 {
		t.Errorf("randomInt = %q", fields[0])
	}
	if fields[1] == strings.TrimPrefix(p.URL, "http://x.test/") {
		t.Error("each {{$uuid}} should be a new value")
	}
	if fields[2] != "2023-11-14T22:13:20Z" {
		t.Errorf("isoTimestamp = %q", fields[2])
	}
}

func TestVariableErrors(t *testing.T) {
	cases := []struct {
		name, text string
		line       int
		contains   string
	}{
		{"undefined in URL", "GET http://x.test/{{nope}}", 1, `undefined variable "nope"`},
		{"undefined in header", "GET http://x.test\nX-A: 1\nX-B: {{nope}}", 3, "nope"},
		{"undefined in body", "POST http://x.test\n\n{\n  \"a\": {{nope}}\n}", 4, "nope"},
		{"unclosed", "GET http://x.test/{{nope", 1, "unclosed"},
		{"cycle", "@a = {{b}}\n@b = {{a}}\nGET http://x.test/{{a}}", 2, "refers to itself"},
		{"unknown dynamic", "GET http://x.test/{{$guid}}", 1, "unknown dynamic variable"},
		{"defined below", "GET http://x.test/{{late}}\n\n###\n@late = 1", 1, "late"},
	}
	for _, c := range cases {
		_, err := Prepare(Target{Path: project(t, nil), Text: c.text, Line: 1}, fixedNow)
		e, ok := err.(Error)
		if !ok || e.Line != c.line || !strings.Contains(e.Message, c.contains) {
			t.Errorf("%s: got %v, want line %d containing %q", c.name, err, c.line, c.contains)
		}
	}
}

func TestPrepareBodyFiles(t *testing.T) {
	path := project(t, map[string]string{"payload.json": `{"id": "{{id}}"}`})
	text := "@id = 42\n### raw\nPOST http://x.test\n\n< ./payload.json\n\n### substituted\nPOST http://x.test\n\n<@ payload.json\n\n### missing\nPOST http://x.test\n\n< ./nope.json\n"

	raw := prepare(t, path, text, 3, "")
	if string(raw.Body) != `{"id": "{{id}}"}` || raw.BodyPath != filepath.Join(filepath.Dir(path), "payload.json") {
		t.Errorf("raw: body %q, path %q", raw.Body, raw.BodyPath)
	}
	sub := prepare(t, path, text, 8, "")
	if string(sub.Body) != `{"id": "42"}` || sub.BodyPath != "" {
		t.Errorf("substituted: body %q, path %q", sub.Body, sub.BodyPath)
	}
	_, err := Prepare(Target{Path: path, Text: text, Line: 13}, fixedNow)
	if e, ok := err.(Error); !ok || e.Line != 15 || !strings.Contains(e.Message, "nope.json") {
		t.Errorf("missing file: %v", err)
	}
}

func TestCurl(t *testing.T) {
	p := Prepared{
		Method: "POST", URL: "http://x.test/a?b=1",
		Headers: []Header{{Name: "Content-Type", Value: "application/json"}, {Name: "X-Q", Value: "it's"}},
		Body:    []byte(`{"a": 1}`),
	}
	want := "curl \\\n  -X POST \\\n  -L \\\n  'http://x.test/a?b=1' \\\n  -H 'Content-Type: application/json' \\\n  -H 'X-Q: it'\\''s' \\\n  --data-raw '{\"a\": 1}'"
	if got := Curl(p, true); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	file := Curl(Prepared{Method: "PUT", URL: "http://x.test", Body: []byte("x"), BodyPath: "/tmp/a b.bin"}, false)
	if !strings.HasSuffix(file, "--data-binary '@/tmp/a b.bin'") || strings.Contains(file, "-L") {
		t.Errorf("file body: %s", file)
	}
	if got := Curl(Prepared{Method: "GET", URL: "http://x.test"}, false); got != "curl \\\n  'http://x.test'" {
		t.Errorf("GET: %q", got)
	}
}
