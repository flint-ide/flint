package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// full is the example manifest of docs/design/extensions.md plus a theme.
const full = `{
  "contract": 1,
  "id": "acme.jira",
  "name": "Jira",
  "version": "0.3.0",
  "publisher": "Acme",
  "description": "Your Jira issues next to your code.",
  "run": {
    "command": "bin/flint-jira",
    "args": ["--stdio"],
    "darwin": { "command": "bin/flint-jira-darwin" },
    "linux": { "command": "bin/flint-jira-linux" }
  },
  "activation": ["onCommand:acme.jira.refresh", "onView:acme.jira.issues", "onAgentTool:jira_issue", "onStartup"],
  "permissions": ["network:acme.atlassian.net", "secrets", "workspace.read", "agent.ask"],
  "contributes": {
    "commands": [{ "id": "acme.jira.refresh", "title": "Refresh Issues", "category": "Jira", "keys": "Mod+Alt+J" },
                 { "id": "acme.jira.link", "title": "Link to Jira Issue" }],
    "panels": [{ "id": "acme.jira.issues", "title": "Jira", "icon": "checkbox", "order": 120, "view": "tree" }],
    "settings": {
      "title": "Jira",
      "properties": {
        "site": { "type": "string", "format": "hostname", "default": "", "description": "Your Atlassian site." },
        "jql": { "type": "string", "default": "assignee = currentUser() AND resolution = Unresolved", "maxLength": 500 },
        "pollMinutes": { "type": "integer", "minimum": 1, "maximum": 60, "default": 10 },
        "projects": { "type": "array", "items": { "type": "string" }, "default": ["ABC"] }
      }
    },
    "statusItems": [{ "id": "acme.jira.count", "order": 10 }],
    "fileMenu": [{ "id": "acme.jira.linkfile", "label": "Link to Jira Issue…", "command": "acme.jira.link" }],
    "tabViews": [{ "id": "acme.jira.issue", "view": "stack" }],
    "agentTools": [{
      "name": "jira_issue",
      "title": "Jira issue",
      "description": "Summary, status and description of a Jira issue by key (ABC-123).",
      "inputSchema": { "type": "object", "properties": { "key": { "type": "string" } }, "required": ["key"] }
    }],
    "themes": [{ "id": "acme.jira.dark", "label": "Jira Dark", "base": "dark",
                 "colors": { "--bg-editor": "#1d2125", "--accent": "#579dff" },
                 "terminal": { "red": "#f87168", "brightBlue": "#85b8ff" } }]
  }
}`

// themeOnly is an extension without a program: a color theme.
const themeOnly = `{
  "contract": 1, "id": "me.nord", "name": "Nord", "version": "1.0.0",
  "contributes": { "themes": [
    { "id": "me.nord.dark", "label": "Nord", "base": "dark", "colors": { "--bg-editor": "#2e3440", "--fg": "#d8dee9" },
      "terminal": { "background": "#2e3440", "red": "#bf616a" } },
    { "id": "me.nord.light", "label": "Nord Light", "base": "light", "colors": { "--bg-editor": "#eceff4" } }
  ] }
}`

func TestParseFull(t *testing.T) {
	m, err := Parse([]byte(full))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if m.ID != "acme.jira" || len(m.Contributes.Commands) != 2 || m.Contributes.Settings.Properties["pollMinutes"].Type != "integer" {
		t.Errorf("Parse() = %+v", m)
	}
	if !m.NeedsProgram() {
		t.Error("NeedsProgram() = false for an extension with commands")
	}
}

func TestParseThemeOnly(t *testing.T) {
	m, err := Parse([]byte(themeOnly))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if m.NeedsProgram() || m.Run != nil {
		t.Error("a theme-only extension needs a program")
	}
	c := m.Contributes.Themes[0].CustomTheme()
	if c.Name != "Nord" || c.Source != "me.nord.dark" || c.Terminal["red"] != "#bf616a" || c.Colors["--fg"] != "#d8dee9" {
		t.Errorf("CustomTheme() = %+v", c)
	}
	if light := m.Contributes.Themes[1].CustomTheme(); light.Terminal != nil || light.Base != "light" {
		t.Errorf("CustomTheme() = %+v, want no terminal map on light", light)
	}
}

// mutate applies f to the decoded full manifest and parses the result.
func mutate(t *testing.T, base string, f func(m map[string]any)) error {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(base), &doc); err != nil {
		t.Fatal(err)
	}
	f(doc)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(data)
	return err
}

func contributes(m map[string]any) map[string]any { return m["contributes"].(map[string]any) }

func first(m map[string]any, kind string) map[string]any {
	return contributes(m)[kind].([]any)[0].(map[string]any)
}

func setting(m map[string]any, name string) map[string]any {
	return contributes(m)["settings"].(map[string]any)["properties"].(map[string]any)[name].(map[string]any)
}

func TestParseRefuses(t *testing.T) {
	cases := []struct {
		name string
		f    func(m map[string]any)
		want string
	}{
		{"other contract", func(m map[string]any) { m["contract"] = 2 }, "contract 2; this Flint supports 1"},
		{"no contract", func(m map[string]any) { delete(m, "contract") }, "contract: missing"},
		{"id without publisher", func(m map[string]any) { m["id"] = "jira" }, "publisher.name"},
		{"id upper case", func(m map[string]any) { m["id"] = "Acme.Jira" }, "publisher.name"},
		{"no name", func(m map[string]any) { m["name"] = " " }, "name: missing"},
		{"control char in name", func(m map[string]any) { m["name"] = "Ji\u001bra" }, "control characters"},
		{"bad version", func(m map[string]any) { m["version"] = "v1" }, "semantic version"},
		{"no run", func(m map[string]any) { delete(m, "run") }, "run: missing"},
		{"absolute command", func(m map[string]any) { m["run"].(map[string]any)["command"] = "/bin/sh" }, "clean relative path"},
		{"escaping command", func(m map[string]any) { m["run"].(map[string]any)["command"] = "../../bin/sh" }, "clean relative path"},
		{"unclean command", func(m map[string]any) { m["run"].(map[string]any)["command"] = "bin/../x" }, "clean relative path"},
		{"backslash command", func(m map[string]any) { m["run"].(map[string]any)["command"] = `bin\x` }, "clean relative path"},
		{"escaping platform command", func(m map[string]any) {
			m["run"].(map[string]any)["linux"] = map[string]any{"command": "../x"}
		}, "run.linux.command"},
		{"newline in command", func(m map[string]any) {
			m["run"].(map[string]any)["command"] = "bin/x\n\nThis extension contains only a theme."
		}, "control characters"},
		{"control char in arg", func(m map[string]any) { m["run"].(map[string]any)["args"] = []any{"--a\rb"} }, "control characters"},
		{"too many permissions", func(m map[string]any) {
			perms := []any{}
			for i := 0; i < 21; i++ {
				perms = append(perms, fmt.Sprintf("network:h%d.example.com", i))
			}
			m["permissions"] = perms
		}, "more than 20 entries"},
		{"too many hosts", func(m map[string]any) {
			m["permissions"] = []any{"network:" + strings.TrimSuffix(strings.Repeat("a.example.com,", 21), ",")}
		}, "more than 20 network hosts"},
		{"null default", func(m map[string]any) { setting(m, "pollMinutes")["default"] = nil }, "null is not"},
		{"control char in enum", func(m map[string]any) { setting(m, "jql")["enum"] = []any{"a\nb"} }, "control characters"},
		{"long pattern", func(m map[string]any) { setting(m, "jql")["pattern"] = strings.Repeat("a", 501) }, "pattern: longer"},
		{"unknown permission", func(m map[string]any) { m["permissions"] = []any{"exec"} }, "unknown permission"},
		{"bad network host", func(m map[string]any) { m["permissions"] = []any{"network:acme.com,https://x"} }, "not a host name"},
		{"duplicate permission", func(m map[string]any) { m["permissions"] = []any{"secrets", "secrets"} }, "listed twice"},
		{"activation of unknown command", func(m map[string]any) { m["activation"] = []any{"onCommand:acme.jira.nope"} }, "activation"},
		{"unknown activation", func(m map[string]any) { m["activation"] = []any{"onLanguage:go"} }, "activation"},
		{"foreign command id", func(m map[string]any) { first(m, "commands")["id"] = "git.push" }, `must start with "acme.jira."`},
		{"bare prefix id", func(m map[string]any) { first(m, "panels")["id"] = "acme.jira." }, `must start with "acme.jira."`},
		{"duplicate command", func(m map[string]any) {
			c := contributes(m)
			c["commands"] = append(c["commands"].([]any), c["commands"].([]any)[0])
		}, "listed twice"},
		{"command without title", func(m map[string]any) { first(m, "commands")["title"] = "" }, "title: missing"},
		{"bad icon", func(m map[string]any) { first(m, "panels")["icon"] = "<svg>" }, "not an icon name"},
		{"unknown view", func(m map[string]any) { first(m, "panels")["view"] = "html" }, "is not one of"},
		{"file menu with unknown command", func(m map[string]any) { first(m, "fileMenu")["command"] = "acme.jira.nope" }, "not a contributed command"},
		{"tool name with dash", func(m map[string]any) {
			first(m, "agentTools")["name"] = "jira-issue"
			m["activation"] = []any{}
		}, "agentTools: name"},
		{"tool schema not object", func(m map[string]any) { first(m, "agentTools")["inputSchema"] = map[string]any{"type": "string"} }, "inputSchema"},
		{"unsupported setting keyword", func(m map[string]any) { setting(m, "jql")["oneOf"] = []any{} }, `"oneOf" is not supported`},
		{"unknown setting type", func(m map[string]any) { setting(m, "jql")["type"] = "object" }, "is not string"},
		{"bad default", func(m map[string]any) { setting(m, "pollMinutes")["default"] = 100 }, "out of range"},
		{"default not integer", func(m map[string]any) { setting(m, "pollMinutes")["default"] = 1.5 }, "not an integer"},
		{"array of numbers", func(m map[string]any) { setting(m, "projects")["items"] = map[string]any{"type": "number"} }, "arrays must have"},
		{"bad pattern", func(m map[string]any) { setting(m, "jql")["pattern"] = "(" }, "pattern"},
		{"bad format", func(m map[string]any) { setting(m, "site")["format"] = "email" }, "not hostname or uri"},
		{"maxLength on number", func(m map[string]any) { setting(m, "pollMinutes")["maxLength"] = 3 }, "apply to strings"},
		{"theme unknown variable", func(m map[string]any) { first(m, "themes")["colors"] = map[string]any{"--evil": "#fff"} }, "unknown color"},
		{"theme css injection", func(m map[string]any) {
			first(m, "themes")["colors"] = map[string]any{"--bg": "red; } body { display: none"}
		}, "must be a hex color"},
		{"theme unknown terminal color", func(m map[string]any) { first(m, "themes")["terminal"] = map[string]any{"pink": "#f0f"} }, "unknown terminal color"},
		{"theme bad base", func(m map[string]any) { first(m, "themes")["base"] = "system" }, "must be dark or light"},
		{"theme foreign id", func(m map[string]any) { first(m, "themes")["id"] = "nord" }, `must start with "acme.jira."`},
		{"theme without colors", func(m map[string]any) {
			th := first(m, "themes")
			delete(th, "colors")
			delete(th, "terminal")
		}, "no colors"},
		{"theme without label", func(m map[string]any) { first(m, "themes")["label"] = "" }, "label: missing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mutate(t, full, c.f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Parse() = %v, want an error containing %q", err, c.want)
			}
		})
	}
}

func TestRunFor(t *testing.T) {
	m, err := Parse([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Run.For("darwin"); got.Command != "bin/flint-jira-darwin" || len(got.Args) != 0 {
		t.Errorf("For(darwin) = %+v", got)
	}
	if got := m.Run.For("linux"); got.Command != "bin/flint-jira-linux" {
		t.Errorf("For(linux) = %+v", got)
	}
	if got := m.Run.For("freebsd"); got.Command != "bin/flint-jira" || len(got.Args) != 1 {
		t.Errorf("For(freebsd) = %+v", got)
	}
}

func TestParseListsEveryProblem(t *testing.T) {
	err := mutate(t, full, func(m map[string]any) {
		m["version"] = "x"
		m["permissions"] = []any{"root"}
	})
	if err == nil || !strings.Contains(err.Error(), "semantic version") || !strings.Contains(err.Error(), "unknown permission") {
		t.Errorf("Parse() = %v, want both problems", err)
	}
}

func TestParseRefusesLargeAndBrokenFiles(t *testing.T) {
	if _, err := Parse([]byte(strings.Repeat(" ", MaxSize+1))); err == nil {
		t.Error("Parse() accepted an oversized manifest")
	}
	if _, err := Parse([]byte(`{"contract":`)); err == nil {
		t.Error("Parse() accepted broken JSON")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "no flint-extension.json") {
		t.Errorf("Load(empty) = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(themeOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, err := Load(dir); err != nil || m.ID != "me.nord" {
		t.Errorf("Load() = %+v, %v", m, err)
	}
	link := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, FileName), filepath.Join(link, FileName)); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := Load(link); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("Load(symlink) = %v", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(full))
	f.Add([]byte(themeOnly))
	f.Add([]byte(`{"contract":1,"contributes":{"settings":{"properties":{"x":{"type":"string","pattern":"(a"}}}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data)
		if err == nil && m.Contract != Contract {
			t.Errorf("Parse() accepted contract %d", m.Contract)
		}
	})
}

func TestPropertyCheck(t *testing.T) {
	maxLen := 3
	p := Property{Type: "string", MaxLength: &maxLen, Enum: []string{"a", "abcd"}}
	if err := p.Check(json.RawMessage(`"a"`)); err != nil {
		t.Errorf("Check(a) = %v", err)
	}
	for _, v := range []string{`"abcd"`, `"b"`, `3`} {
		if p.Check(json.RawMessage(v)) == nil {
			t.Errorf("Check(%s) accepted", v)
		}
	}
	host := Property{Type: "string", Format: "hostname"}
	if host.Check(json.RawMessage(`"acme.atlassian.net"`)) != nil || host.Check(json.RawMessage(`"http://x"`)) == nil {
		t.Error("hostname format check is wrong")
	}
	if (Property{Type: "boolean"}).Check(json.RawMessage(`"true"`)) == nil {
		t.Error("boolean check accepted a string")
	}
}
