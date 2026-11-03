package host

import (
	"strings"
	"testing"

	"flint/internal/extensions/manifest"
)

func TestInstallMessageListsDebuggersAndTestRunners(t *testing.T) {
	m, err := manifest.Parse([]byte(`{
	  "contract": 1, "id": "me.ruby-tools", "name": "Ruby tools", "version": "1.0.0",
	  "contributes": {
	    "debuggers": [ { "type": "rdbg", "label": "Ruby (rdbg)", "languages": ["ruby"],
	      "adapter": { "candidates": [["rdbg", "--open"], ["./bin/adapter"]] } } ],
	    "testRunners": [ { "id": "rspec", "label": "RSpec", "languages": ["ruby"], "command": ["rspec", "${file}"], "format": "tap" } ]
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	msg := installMessage(m)
	for _, want := range []string{
		"Debuggers:\n• Ruby (rdbg), for ruby files\n  runs the first one installed: rdbg --open | ./bin/adapter (in trusted folders only)",
		"Test runners:\n• RSpec, for ruby files\n  runs: rspec ${file} (in trusted folders only)",
		"No program of its own: only the programs listed above run, and only in folders you trust.",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "runs no code") {
		t.Errorf("message claims no code runs:\n%s", msg)
	}
}
