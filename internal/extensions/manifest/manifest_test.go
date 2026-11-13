package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const valid = `{
  "contract": 1, "id": "acme.jira", "name": "Jira", "version": "0.3.0", "publisher": "Acme",
  "run": { "command": "bin/jira", "args": ["--stdio"] },
  "activation": ["onStartup", "onCommand:acme.jira.refresh", "onView:acme.jira.issues", "onAgentTool:jira_issue"],
  "permissions": ["network:acme.atlassian.net", "secrets", "workspace.read"],
  "contributes": {
    "commands": [{ "id": "acme.jira.refresh", "title": "Refresh Issues", "category": "Jira", "keys": "Mod+Alt+J" }],
    "panels": [{ "id": "acme.jira.issues", "title": "Jira", "icon": "checkbox", "order": 120, "view": "tree" }],
    "statusItems": [{ "id": "acme.jira.count", "order": 10 }],
    "settings": { "title": "Jira", "properties": {
      "site": { "type": "string", "format": "hostname", "default": "" },
      "pollMinutes": { "type": "integer", "minimum": 1, "maximum": 60, "default": 10 },
      "mode": { "type": "string", "enum": ["mine", "all"] },
      "labels": { "type": "array", "items": { "type": "string" }, "maxLength": 20 },
      "verbose": { "type": "boolean" }
    }},
    "agentTools": [{ "name": "jira_issue", "description": "An issue by key.", "inputSchema": { "type": "object" } }],
    "fileMenu": [{ "id": "acme.jira.link", "label": "Link" }]
  }
}`

func TestParseValid(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "acme.jira" || !m.RunsAtStartup() || !m.HasPermission(PermSecrets) || m.HasPermission(PermAgentAsk) {
		t.Errorf("parsed %+v", m)
	}
	if got := m.NetworkHosts(); !reflect.DeepEqual(got, []string{"acme.atlassian.net"}) {
		t.Errorf("hosts = %v", got)
	}
	if !reflect.DeepEqual(m.Contributes.Unsupported, []string{"fileMenu"}) {
		t.Errorf("unsupported = %v", m.Contributes.Unsupported)
	}
	var keys []string
	for _, p := range m.Contributes.Settings.Properties {
		keys = append(keys, p.Key)
	}
	if !reflect.DeepEqual(keys, []string{"site", "pollMinutes", "mode", "labels", "verbose"}) {
		t.Errorf("settings keep the manifest's order: %v", keys)
	}
	if m.Contributes.Settings.Properties[2].Default != "mine" {
		t.Errorf("an enum defaults to its first value: %v", m.Contributes.Settings.Properties[2].Default)
	}
}

// edit applies change to the valid manifest's JSON object.
func edit(t *testing.T, change func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(valid), &m); err != nil {
		t.Fatal(err)
	}
	change(m)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func contributes(m map[string]any) map[string]any { return m["contributes"].(map[string]any) }

// A tool may declare that it acts; by default it only reads.
func TestAgentToolActs(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil || m.Contributes.AgentTools[0].Acts {
		t.Fatalf("a tool acts by default: %+v, %v", m, err)
	}
	m, err = Parse(edit(t, func(m map[string]any) {
		contributes(m)["agentTools"] = []any{map[string]any{"name": "jira_issue", "description": "Adds a comment.", "acts": true}}
	}))
	if err != nil || !m.Contributes.AgentTools[0].Acts {
		t.Fatalf("acts not read: %+v, %v", m, err)
	}
	if _, err := Parse(edit(t, func(m map[string]any) {
		contributes(m)["agentTools"] = []any{map[string]any{"name": "jira_issue", "description": "d", "acts": "yes"}}
	})); err == nil {
		t.Fatal(`"acts": "yes" accepted`)
	}
}

func TestParseRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		change     func(m map[string]any)
	}{
		{"newer contract", "contract 2; this Flint supports 1", func(m map[string]any) { m["contract"] = 2 }},
		{"no contract", `"contract" is missing`, func(m map[string]any) { delete(m, "contract") }},
		{"upper-case id", "publisher.name", func(m map[string]any) { m["id"] = "Acme.Jira" }},
		{"id without publisher", "publisher.name", func(m map[string]any) { m["id"] = "jira" }},
		{"bad version", "major.minor.patch", func(m map[string]any) { m["version"] = "1" }},
		{"absolute command", "relative path", func(m map[string]any) { m["run"] = map[string]any{"command": "/bin/sh"} }},
		{"escaping command", "clean relative path", func(m map[string]any) { m["run"] = map[string]any{"command": "../x"} }},
		{"unclean command", "clean relative path", func(m map[string]any) { m["run"] = map[string]any{"command": "bin/../../x"} }},
		{"unknown permission", "unknown permission", func(m map[string]any) { m["permissions"] = []string{"exec"} }},
		{"bad network host", "not a host name", func(m map[string]any) { m["permissions"] = []string{"network:a b"} }},
		{"foreign command id", `must start with "acme.jira."`, func(m map[string]any) {
			contributes(m)["commands"] = []any{map[string]any{"id": "git.push", "title": "Push"}}
		}},
		{"duplicate command", "used twice", func(m map[string]any) {
			c := map[string]any{"id": "acme.jira.x", "title": "X"}
			contributes(m)["commands"] = []any{c, c}
			m["activation"] = []string{}
		}},
		{"table view", `view "table" is not supported`, func(m map[string]any) {
			contributes(m)["panels"] = []any{map[string]any{"id": "acme.jira.issues", "title": "J", "icon": "files", "view": "table"}}
		}},
		{"activation of a missing command", "activation event", func(m map[string]any) { m["activation"] = []string{"onCommand:acme.jira.nope"} }},
		{"unknown schema keyword", "unknown field", func(m map[string]any) {
			contributes(m)["settings"] = map[string]any{"properties": map[string]any{"x": map[string]any{"type": "string", "oneOf": []any{}}}}
		}},
		{"invalid default", "default: must be between", func(m map[string]any) {
			contributes(m)["settings"] = map[string]any{"properties": map[string]any{"x": map[string]any{"type": "integer", "minimum": 1, "maximum": 5, "default": 9}}}
		}},
		{"object type", `type "object" is not supported`, func(m map[string]any) {
			contributes(m)["settings"] = map[string]any{"properties": map[string]any{"x": map[string]any{"type": "object"}}}
		}},
		{"tool schema not an object", `"type": "object"`, func(m map[string]any) {
			contributes(m)["agentTools"] = []any{map[string]any{"name": "jira_issue", "description": "d", "inputSchema": map[string]any{"type": "string"}}}
		}},
		{"control characters in a title", "control characters", func(m map[string]any) { m["name"] = "Ji\x1bra" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(edit(t, tc.change))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadAndProgram(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(FileName, valid, 0o644)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "bin/jira") {
		t.Fatalf("a missing program: %v", err)
	}
	write("bin/jira", "#!/bin/sh\n", 0o644)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "not an executable") {
		t.Fatalf("a program without the executable bit: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, "bin/jira"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	path, args, err := m.Program(dir)
	if err != nil || filepath.Base(path) != "jira" || !reflect.DeepEqual(args, []string{"--stdio"}) {
		t.Errorf("Program = %q %v %v", path, args, err)
	}
	// A symbolic link out of the folder is refused.
	outside := filepath.Join(t.TempDir(), "evil")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "bin/jira")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "bin/jira")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("a link out of the folder: %v", err)
	}
}

func TestSchemaValues(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	s := m.Contributes.Settings
	got := s.Values(json.RawMessage(`{"site":"x.example.com","pollMinutes":99,"mode":"all","labels":["a",3],"extra":1}`))
	want := map[string]any{"site": "x.example.com", "pollMinutes": float64(10), "mode": "all", "labels": []any{}, "verbose": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Values = %#v, want %#v", got, want)
	}
	if got := s.Values(json.RawMessage(`"not an object"`)); got["pollMinutes"] != float64(10) {
		t.Errorf("a non-object gives the defaults: %v", got)
	}
	for raw, want := range map[string]string{
		`{"pollMinutes": 1.5}`: "whole number",
		`{"site": "a b"}`:      "host name",
		`{"mode": "some"}`:     "one of mine, all",
		`{"labels": ["` + strings.Repeat("x", 21) + `"]}`: "at most 20",
		`{"verbose": "yes"}`:                              "true or false",
		`[]`:                                              "object",
	} {
		if err := s.Check(json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Check(%s) = %v, want %q", raw, err, want)
		}
	}
	if err := s.Check(json.RawMessage(`{"pollMinutes": 5, "unknown": true}`)); err != nil {
		t.Errorf("valid values: %v", err)
	}
}
