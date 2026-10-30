package testrun

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Languages a test file can be written in.
const (
	LangGo = "go"
	LangJS = "js"
)

// Mark kinds for the editor gutter.
const (
	MarkFile    = "file"
	MarkSuite   = "suite"
	MarkTest    = "test"
	MarkSubtest = "subtest"
)

// Mark is a runnable spot in a test file.
type Mark struct {
	Line int    `json:"line"` // 1-based
	Kind string `json:"kind"`
	// Name is the test name a run selects (Request.Tests); "" for the file mark.
	Name string `json:"name"`
}

// Lang tells whether path is a Go or JS/TS test file ("" for neither).
func Lang(path string) string {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return LangGo
	}
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
		stem, ok := strings.CutSuffix(base, ext)
		if ok && (strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")) {
			return LangJS
		}
	}
	return ""
}

// Marks finds the tests in a file's content with a line scan (no parsing, so it
// works on code that doesn't compile yet). Subtests and nested suites are
// attributed by indentation, which gofmt and prettier keep consistent.
func Marks(path, content string) []Mark {
	switch Lang(path) {
	case LangGo:
		return goMarks(content)
	case LangJS:
		return jsMarks(content)
	}
	return nil
}

var (
	goPackage = regexp.MustCompile(`^package\s+\w+`)
	goTestFn  = regexp.MustCompile(`^func\s+((?:Test|Fuzz)(?:[A-Z0-9_]\w*)?)\s*\(\s*\w+\s+\*testing\.[TF]\s*\)`)
	goRun     = regexp.MustCompile(`\b\w+\.Run\(\s*("(?:[^"\\]|\\.)*"|` + "`[^`]*`" + `)`)
)

type scope struct {
	indent int
	name   string
}

func goMarks(content string) []Mark {
	var marks []Mark
	fn := ""
	var subs []scope
	for i, line := range strings.Split(content, "\n") {
		switch {
		case goPackage.MatchString(line):
			marks = append(marks, Mark{Line: i + 1, Kind: MarkFile})
		case strings.HasPrefix(line, "func "):
			fn, subs = "", nil
			if m := goTestFn.FindStringSubmatch(line); m != nil {
				fn = m[1]
				marks = append(marks, Mark{Line: i + 1, Kind: MarkTest, Name: fn})
			}
		case fn != "":
			m := goRun.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name, err := strconv.Unquote(m[1])
			if err != nil {
				continue
			}
			indent := len(line) - len(strings.TrimLeft(line, "\t "))
			subs = enter(subs, indent, goSubtestName(name))
			marks = append(marks, Mark{Line: i + 1, Kind: MarkSubtest, Name: fn + "/" + join(subs, "/")})
		}
	}
	return marks
}

// goSubtestName mirrors how the testing package rewrites names (spaces become
// underscores), so -run matches.
func goSubtestName(name string) string {
	return strings.ReplaceAll(name, " ", "_")
}

var jsTest = regexp.MustCompile(`^(\s*)(describe|suite|context|it|test)(?:\.(?:only|skip|todo|concurrent|each))?\(\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"|` + "`([^`$]*)`" + `)`)

func jsMarks(content string) []Mark {
	marks := []Mark{{Line: 1, Kind: MarkFile}}
	var open []scope
	for i, line := range strings.Split(content, "\n") {
		m := jsTest.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := unescapeJS(m[3] + m[4] + m[5])
		open = enter(open, len(m[1]), name)
		kind := MarkTest
		if m[2] == "describe" || m[2] == "suite" || m[2] == "context" {
			kind = MarkSuite
		}
		marks = append(marks, Mark{Line: i + 1, Kind: kind, Name: join(open, " ")})
	}
	return marks
}

// enter closes scopes indented at least as deep and opens a new one.
func enter(stack []scope, indent int, name string) []scope {
	kept := stack
	for len(kept) > 0 && kept[len(kept)-1].indent >= indent {
		kept = kept[:len(kept)-1]
	}
	return append(append([]scope(nil), kept...), scope{indent: indent, name: name})
}

func join(stack []scope, sep string) string {
	names := make([]string, len(stack))
	for i, s := range stack {
		names[i] = s.name
	}
	return strings.Join(names, sep)
}

func unescapeJS(s string) string {
	return strings.NewReplacer(`\'`, "'", `\"`, `"`, `\\`, `\`).Replace(s)
}
