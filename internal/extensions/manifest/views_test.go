package manifest

import (
	"strings"
	"testing"
)

func viewsManifest(contributes string) string {
	return `{"contract":1,"id":"acme.jira","name":"Jira","version":"0.1.0","run":{"command":"bin/jira"},"contributes":` + contributes + `}`
}

func TestDeclarativeViews(t *testing.T) {
	m, err := Parse([]byte(viewsManifest(`{
  "panels":[{"id":"acme.jira.issues","title":"Issues","icon":"checkbox","view":"table"},
            {"id":"acme.jira.signin","title":"Sign In","icon":"checkbox","view":"form"},
            {"id":"acme.jira.trend","title":"Trend","icon":"sum","view":"chart"},
            {"id":"acme.jira.tree","title":"Tree","icon":"files","view":"tree"}],
  "tabViews":[{"id":"acme.jira.board","title":"Board","view":"table","restore":true},
              {"id":"acme.jira.comment","title":"Comment","view":"form"},
              {"id":"acme.jira.burndown","title":"Burndown","icon":"sum","view":"chart"}]
}`)))
	if err != nil {
		t.Fatal(err)
	}
	if m.HasWebviews() {
		t.Error("declarative tab views count as webviews")
	}
	for id, want := range map[string]struct {
		kind string
		tab  bool
	}{
		"acme.jira.issues":   {"table", false},
		"acme.jira.signin":   {"form", false},
		"acme.jira.trend":    {"chart", false},
		"acme.jira.tree":     {"tree", false},
		"acme.jira.board":    {"table", true},
		"acme.jira.burndown": {"chart", true},
	} {
		kind, tab, ok := m.ViewKind(id)
		if !ok || kind != want.kind || tab != want.tab {
			t.Errorf("ViewKind(%s) = %q, %v, %v; want %q, %v", id, kind, tab, ok, want.kind, want.tab)
		}
	}
	if _, _, ok := m.ViewKind("acme.jira.none"); ok {
		t.Error("an unknown view has a kind")
	}
	if _, ok := m.WebviewOf("acme.jira.board"); ok {
		t.Error("a table tab view has a webview")
	}
}

func TestDeclarativeViewRules(t *testing.T) {
	for name, tc := range map[string]struct{ contributes, want string }{
		"unknown panel kind": {`{"panels":[{"id":"acme.jira.p","title":"P","icon":"files","view":"cards"}]}`, `view "cards" is not supported (this Flint draws "tree", "list", "table", "form", "chart" and "webview")`},
		"markdown panel":     {`{"panels":[{"id":"acme.jira.p","title":"P","icon":"files","view":"markdown"}]}`, "not supported"},
		"no view":            {`{"panels":[{"id":"acme.jira.p","title":"P","icon":"files"}]}`, "not supported"},
		"table with webview": {`{"panels":[{"id":"acme.jira.p","title":"P","icon":"files","view":"table","webview":{"html":"x.html"}}]}`, `only for a "webview" view`},
		"tree tab":           {`{"tabViews":[{"id":"acme.jira.t","title":"T","view":"tree"}]}`, `view "tree" is not supported (this Flint draws "table", "form", "chart" and "webview")`},
		"stack tab":          {`{"tabViews":[{"id":"acme.jira.t","title":"T","view":"stack"}]}`, "not supported"},
		"chart tab webview":  {`{"tabViews":[{"id":"acme.jira.t","title":"T","view":"chart","webview":{"html":"x.html"}}]}`, `only for a "webview" view`},
		"tab bad icon":       {`{"tabViews":[{"id":"acme.jira.t","title":"T","view":"chart","icon":"<svg>"}]}`, "icon"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(viewsManifest(tc.contributes)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseProperty(t *testing.T) {
	p, err := ParseProperty("site", []byte(`{"type":"string","format":"hostname","pattern":"[a-z.]+","title":"Site"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Check("acme.example") != nil || p.Check("ACME") == nil || p.Default != "" {
		t.Errorf("property = %+v", p)
	}
	for name, tc := range map[string]struct{ key, data, want string }{
		"bad key":     {"a b", `{"type":"string"}`, "letters, digits"},
		"bad type":    {"x", `{"type":"object"}`, "not supported"},
		"unknown key": {"x", `{"type":"string","multiline":true}`, "supported:"},
		"bad default": {"x", `{"type":"integer","default":"1"}`, "default"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProperty(tc.key, []byte(tc.data)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseProperty() = %v, want %q", err, tc.want)
			}
		})
	}
}
