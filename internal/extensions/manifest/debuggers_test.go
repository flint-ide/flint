package manifest

import (
	"strings"
	"testing"
)

// rubyTools is a data-only extension with a debugger and a test runner.
const rubyTools = `{
  "contract": 1, "id": "me.ruby-tools", "name": "Ruby tools", "version": "1.0.0",
  "contributes": {
    "debuggers": [ {
      "type": "ruby-rdbg", "label": "Ruby (rdbg)", "languages": ["ruby"],
      "adapter": { "candidates": [["rdbg", "--open", "--command"], ["./bin/adapter"]], "install": "gem install debug" },
      "launch": { "type": "rdbg", "request": "launch", "script": "${file}", "cwd": "${workspaceFolder}", "args": ["${fileBasename}"], "port": 1 }
    } ],
    "testRunners": [ {
      "id": "rspec", "label": "RSpec", "languages": ["ruby"],
      "command": ["bundle", "exec", "rspec", "${file}", "${tests}"], "format": "tap"
    } ]
  }
}`

func TestParseDebuggersAndTestRunners(t *testing.T) {
	m, err := Parse([]byte(rubyTools))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if m.HasProgram() || m.NeedsProgram() {
		t.Error("a debugger-only extension has or needs a program")
	}
	d := m.Contributes.Debuggers[0]
	if d.Type != "ruby-rdbg" || d.Adapter == nil || len(d.Adapter.Candidates) != 2 || !strings.Contains(string(d.Launch), `"${file}"`) {
		t.Errorf("debugger = %+v", d)
	}
	r := m.Contributes.TestRunners[0]
	if r.ID != "rspec" || r.Format != TestFormatTAP || r.CommandLine() != "bundle exec rspec ${file} ${tests}" {
		t.Errorf("test runner = %+v", r)
	}
	if len(m.Contributes.Unsupported) != 0 {
		t.Errorf("listed as unsupported: %v", m.Contributes.Unsupported)
	}
}

func TestDebuggerAndTestRunnerChecks(t *testing.T) {
	for _, tc := range []struct{ name, from, to, want string }{
		{"bad type", `"type": "ruby-rdbg"`, `"type": "Ruby Debug"`, "debugger type"},
		{"no label", `"label": "Ruby (rdbg)", `, ``, "label is missing"},
		{"no languages", `"languages": ["ruby"],
      "adapter"`, `"languages": [],
      "adapter"`, "no languages"},
		{"bad language", `"languages": ["ruby"],
      "adapter"`, `"languages": ["Ruby"],
      "adapter"`, `language "Ruby"`},
		{"no adapter", `"adapter": { "candidates": [["rdbg", "--open", "--command"], ["./bin/adapter"]], "install": "gem install debug" },`, ``, "adapter is missing"},
		{"adapter path", `["./bin/adapter"]`, `["/usr/bin/rdbg"]`, "adapter program"},
		{"adapter escapes", `["./bin/adapter"]`, `["./../adapter"]`, "adapter program"},
		{"adapter variable", `"--command"]`, `"${env:X}"]`, "unknown variable"},
		{"launch not object", `"launch": { "type": "rdbg", "request": "launch", "script": "${file}", "cwd": "${workspaceFolder}", "args": ["${fileBasename}"], "port": 1 }`, `"launch": ["${file}"]`, "JSON object"},
		{"unknown variable", `"script": "${file}"`, `"script": "${env:HOME}"`, "unknown variable ${env:HOME}"},
		{"tests in launch", `"script": "${file}"`, `"script": "${tests}"`, "only for test runner commands"},
		{"launch too deep", `"port": 1`, `"x": [[[[[[[[["deep"]]]]]]]]]`, "nested deeper"},
		{"launch too big", `"port": 1`, `"x": "` + strings.Repeat("a", maxLaunch) + `"`, "larger than 16 KiB"},
		{"twice", `"type": "ruby-rdbg", "label": "Ruby (rdbg)", "languages": ["ruby"],
      "adapter": { "candidates": [["rdbg", "--open", "--command"], ["./bin/adapter"]], "install": "gem install debug" },
      "launch": { "type": "rdbg", "request": "launch", "script": "${file}", "cwd": "${workspaceFolder}", "args": ["${fileBasename}"], "port": 1 }
    }`, `"type": "a", "label": "A", "languages": ["ruby"], "adapter": { "candidates": [["a"]] } }, { "type": "a", "label": "A", "languages": ["ruby"], "adapter": { "candidates": [["a"]] } }`, `debugger "a" is listed twice`},
		{"bad runner id", `"id": "rspec"`, `"id": "RSpec!"`, "test runner id"},
		{"no command", `"command": ["bundle", "exec", "rspec", "${file}", "${tests}"], `, ``, "command is missing"},
		{"runner path", `["bundle",`, `["../bundle",`, "test runner program"},
		{"runner shell", `["bundle",`, `["sh -c",`, "test runner program"},
		{"tests inside argument", `"${tests}"]`, `"--example=${tests}"]`, "argument of its own"},
		{"runner unknown variable", `"${tests}"]`, `"${line}"]`, "unknown variable ${line}"},
		{"runner control character", `"exec",`, `"ex\u0007ec",`, "not allowed"},
		{"format", `"format": "tap"`, `"format": "junit"`, `format "junit"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(rubyTools, tc.from, tc.to, 1)
			if src == rubyTools {
				t.Fatalf("%q not found in the manifest", tc.from)
			}
			_, err := Parse([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestTooManyDebuggers(t *testing.T) {
	one := `{ "type": "d%d", "label": "D", "languages": ["x"], "adapter": { "candidates": [["d"]] } }`
	var list []string
	for i := range maxDebuggers + 1 {
		list = append(list, strings.Replace(one, "%d", string(rune('a'+i)), 1))
	}
	src := `{ "contract": 1, "id": "me.d", "name": "D", "version": "1.0.0", "contributes": { "debuggers": [` + strings.Join(list, ",") + `] } }`
	if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "contributes.debuggers: at most") {
		t.Fatalf("Parse() = %v", err)
	}
}
