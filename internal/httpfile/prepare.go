package httpfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxBodyFile bounds a "< file" body.
const maxBodyFile = 100 << 20

// Prepared is a request with every variable substituted, ready to send.
type Prepared struct {
	Name    string
	Method  string
	URL     string
	Headers []Header
	Body    []byte
	// BodyPath is the absolute path of a "< file" body sent as is (for curl's @file).
	BodyPath    string
	NoRedirect  bool
	NoCookieJar bool
	Timeout     time.Duration
	// Key identifies the request in its file across edits (response history).
	Key string
}

// Target says which request to prepare and how.
type Target struct {
	// Path is the .http file: env files and "< file" bodies are found next to it.
	Path string `json:"path"`
	// Text is the editor's content (may be unsaved).
	Text string `json:"text"`
	// Line is a 1-based line inside the request's block (the cursor).
	Line int `json:"line"`
	// Env is the selected environment ("" = none).
	Env string `json:"env"`
	// Roots are the open project folders: "< file" bodies must be inside
	// one (default: the .http file's folder). An untrusted .http file must not
	// send ~/.aws/credentials to its URL.
	Roots []string `json:"-"`
}

// Prepare parses t.Text, picks the request at t.Line and substitutes its variables.
func Prepare(t Target, now func() time.Time) (Prepared, error) {
	file := Parse(t.Text)
	req, err := file.RequestAt(t.Line)
	if err != nil {
		return Prepared{}, err
	}
	dir := filepath.Dir(t.Path)
	env, err := loadEnv(dir, t.Env)
	if err != nil {
		return Prepared{}, err
	}
	r := newResolver(file.Vars, req.Line, env, t.Env, now)

	out := Prepared{
		Name: req.Name, Method: req.Method, NoRedirect: req.NoRedirect, NoCookieJar: req.NoCookieJar,
		Timeout: req.Timeout, Key: requestKey(req),
	}
	if out.URL, err = r.expand(req.URL, req.Line); err != nil {
		return Prepared{}, err
	}
	if !strings.Contains(out.URL, "://") {
		out.URL = "http://" + out.URL // "localhost:8080/x", as in IntelliJ
	}
	for _, h := range req.Headers {
		value, err := r.expand(h.Value, h.Line)
		if err != nil {
			return Prepared{}, err
		}
		out.Headers = append(out.Headers, Header{Name: h.Name, Value: value})
	}
	switch {
	case req.BodyFile != "":
		path := req.BodyFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		roots := t.Roots
		if len(roots) == 0 {
			roots = []string{dir}
		}
		if err := insideRoots(path, roots); err != nil {
			return Prepared{}, fmt.Errorf("line %d: %w", req.BodyLine, err)
		}
		data, err := readBodyFile(path, req.BodyLine)
		if err != nil {
			return Prepared{}, err
		}
		if req.BodyFileVars {
			text, err := r.expand(string(data), req.BodyLine)
			if err != nil {
				return Prepared{}, err
			}
			data = []byte(text)
		} else {
			out.BodyPath = path
		}
		out.Body = data
	case req.Body != "":
		text, err := r.expand(req.Body, req.BodyLine)
		if err != nil {
			return Prepared{}, err
		}
		out.Body = []byte(text)
	}
	return out, nil
}

func readBodyFile(path string, line int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, Error{Line: line, Message: fmt.Sprintf("body file %s: %v", filepath.Base(path), unwrapPathError(err))}
	}
	if info.IsDir() || info.Size() > maxBodyFile {
		return nil, Error{Line: line, Message: fmt.Sprintf("body file %s is a folder or larger than %d MB", filepath.Base(path), maxBodyFile>>20)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Error{Line: line, Message: fmt.Sprintf("body file %s: %v", filepath.Base(path), unwrapPathError(err))}
	}
	return data, nil
}

func unwrapPathError(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

// requestKey names a request stably while lines move: its name, else method and URL.
func requestKey(r Request) string {
	if r.Name != "" {
		return "name:" + r.Name
	}
	return r.Method + " " + r.URL
}

// Mark is a request in the editor outline (gutter ▶).
type Mark struct {
	Line   int    `json:"line"`
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
}

// Outline lists the requests and parse errors of a file, for the editor.
type Outline struct {
	Requests []Mark  `json:"requests"`
	Errors   []Error `json:"errors"`
}

// OutlineOf parses src for the editor's gutter and error marks.
func OutlineOf(src string) Outline {
	file := Parse(src)
	marks := make([]Mark, 0, len(file.Requests))
	for _, r := range file.Requests {
		marks = append(marks, Mark{Line: r.Line, Name: r.Name, Method: r.Method, URL: r.URL})
	}
	return Outline{Requests: marks, Errors: file.Errors}
}

// insideRoots requires path, with symlinks resolved, to lie in one of roots.
func insideRoots(path string, roots []string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil // missing: readBodyFile reports it
	}
	for _, root := range roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if real == r || strings.HasPrefix(real, r+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("body file %s is outside the project: copy it into the project to send it", path)
}
