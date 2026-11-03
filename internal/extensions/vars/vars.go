// Package vars substitutes the ${…} variables of an extension's debugger
// launch configuration and test runner command (manifest contributes.debuggers
// and contributes.testRunners). Substitution is plain text inside one string
// or one argument: nothing is ever handed to a shell. The manifest checks
// the variables when the extension is installed; here they are filled in when
// a file is debugged or tested.
package vars

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Names are the variables a launch configuration or a command may use.
var Names = []string{"file", "fileBasename", "fileDirname", "workspaceFolder"}

// Tests is the test runner variable that becomes the selected test names:
// zero or more arguments, so it must be an argument of its own.
const Tests = "${tests}"

var variable = regexp.MustCompile(`\$\{([^}]*)\}`)

// Values are the variables for one file in one project.
type Values map[string]string

// ForFile fills the variables for file (absolute) in the project at root.
func ForFile(root, file string) Values {
	return Values{
		"file":            file,
		"fileBasename":    filepath.Base(file),
		"fileDirname":     filepath.Dir(file),
		"workspaceFolder": root,
	}
}

// Check reports an unknown variable in s; tests allows ${tests} as the whole
// of s (a command argument).
func Check(s string, tests bool) error {
	if tests && s == Tests {
		return nil
	}
	for _, m := range variable.FindAllStringSubmatch(s, -1) {
		name := m[1]
		if name == "tests" {
			if tests {
				return fmt.Errorf("%q: ${tests} must be an argument of its own", s)
			}
			return fmt.Errorf("%q: ${tests} is only for test runner commands", s)
		}
		if !slices.Contains(Names, name) {
			return fmt.Errorf("%q: unknown variable ${%s} (known: %s)", s, name, list())
		}
	}
	return nil
}

func list() string {
	out := make([]string, len(Names))
	for i, n := range Names {
		out[i] = "${" + n + "}"
	}
	return strings.Join(out, ", ")
}

// Expand substitutes the variables of s; unknown ones (refused by Check)
// stay as they are.
func Expand(s string, v Values) string {
	return variable.ReplaceAllStringFunc(s, func(m string) string {
		if val, ok := v[m[2:len(m)-1]]; ok {
			return val
		}
		return m
	})
}

// ExpandArgs substitutes the variables of each argument; an argument that is
// ${tests} becomes one argument per test name (none when tests is empty).
func ExpandArgs(args []string, v Values, tests []string) []string {
	out := make([]string, 0, len(args)+len(tests))
	for _, a := range args {
		if a == Tests {
			out = append(out, tests...)
			continue
		}
		out = append(out, Expand(a, v))
	}
	return out
}

// Walk calls fn for each string value of a decoded JSON value (object keys
// are left alone) and returns the value with fn's results, a new value; depth
// counts nested objects and arrays, and deeper values are refused.
func Walk(x any, maxDepth int, fn func(string) (string, error)) (any, error) {
	if maxDepth < 0 {
		return nil, fmt.Errorf("nested deeper than allowed")
	}
	switch t := x.(type) {
	case string:
		return fn(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			v, err := Walk(e, maxDepth-1, fn)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			v, err := Walk(e, maxDepth-1, fn)
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	}
	return x, nil // numbers, booleans, null
}

// Decode decodes JSON keeping numbers as written (json.Number), so a
// large integer in a launch configuration reaches the adapter unchanged.
func Decode(raw json.RawMessage) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var x any
	if err := d.Decode(&x); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, fmt.Errorf("more than one JSON value")
	}
	return x, nil
}

// ExpandJSON substitutes the variables in the string values of a JSON
// object (a launch configuration).
func ExpandJSON(raw json.RawMessage, v Values, maxDepth int) (json.RawMessage, error) {
	x, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	out, err := Walk(x, maxDepth, func(s string) (string, error) { return Expand(s, v), nil })
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
