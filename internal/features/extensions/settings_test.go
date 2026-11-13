package extensions

import (
	"encoding/json"
	"strings"
	"testing"

	"flint/internal/extensions/index"
	"flint/internal/settings"
)

func init() { settings.Register(Section) }

func TestSettingsRoundTrip(t *testing.T) {
	in := `{"extensions": {
	  "enabled": {"acme.jira": false, "Bad Id": true},
	  "agentTools": {"acme.jira": false},
	  "acme.jira": {"site": "acme.atlassian.net", "pollMinutes": 10},
	  "gone.ext": {"kept": [1, 2]},
	  "acme.list": [1],
	  "notanid": {"x": 1}
	}}`
	var s settings.Settings
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	got := Section.Get(s)
	if got.enabled("acme.jira") || !got.enabled("other.ext") || got.toolsOffered("acme.jira") {
		t.Errorf("flags = %+v", got)
	}
	if len(got.Enabled) != 1 {
		t.Errorf("an invalid id is dropped: %v", got.Enabled)
	}
	if string(got.Values["gone.ext"]) != `{"kept":[1,2]}` {
		t.Errorf("values of an extension that isn't installed are kept: %s", got.Values["gone.ext"])
	}
	if _, ok := got.Values["acme.list"]; ok {
		t.Error("a value that isn't an object is kept")
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"enabled":{"acme.jira":false},"agentTools":{"acme.jira":false},"acme.jira":{"site":"acme.atlassian.net","pollMinutes":10},"gone.ext":{"kept":[1,2]}}`
	if string(data) != want {
		t.Errorf("marshal = %s\nwant      %s", data, want)
	}
	// Decoding replaces: a value the frontend removed is gone.
	if err := json.Unmarshal([]byte(`{"extensions": {"acme.jira": {}}}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := Section.Get(s); len(got.Values) != 1 || len(got.Enabled) != 0 {
		t.Errorf("after a second decode: %+v", got)
	}
	if def, _ := json.Marshal(Settings{}); string(def) != `{}` {
		t.Errorf("default = %s", def)
	}
}

func TestDecorationSetFlags(t *testing.T) {
	var s settings.Settings
	in := `{"extensions": {"decorations": {"acme.lint.issues": false, "acme.lint.todo": true, "acme.lint": true, "Bad.Id.x": false, "acme.lint.": true}}}`
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	got := Section.Get(s)
	if len(got.Decorations) != 2 {
		t.Errorf("only set ids (<extension id>.<name>) are kept: %v", got.Decorations)
	}
	for _, tc := range []struct {
		set       string
		def, want bool
	}{
		{"acme.lint.issues", true, false}, // turned off
		{"acme.lint.todo", false, true},   // turned on
		{"acme.lint.other", true, true},   // the manifest's default
		{"acme.lint.other", false, false},
	} {
		if on := got.setEnabled(tc.set, tc.def); on != tc.want {
			t.Errorf("setEnabled(%s, %v) = %v", tc.set, tc.def, on)
		}
	}
	data, _ := json.Marshal(got)
	if string(data) != `{"decorations":{"acme.lint.issues":false,"acme.lint.todo":true}}` {
		t.Errorf("marshal = %s", data)
	}
	c := cloneSettings(got)
	c.Decorations["acme.lint.issues"] = true
	if got.Decorations["acme.lint.issues"] {
		t.Error("clone shares the decorations map")
	}
}

func TestSettingsCheck(t *testing.T) {
	big := Settings{Values: map[string]json.RawMessage{"acme.x": json.RawMessage(`{"a":"` + strings.Repeat("x", maxValueSize) + `"}`)}}
	if err := check(big); err == nil {
		t.Error("an oversized object is saved")
	}
	if err := check(Settings{Values: map[string]json.RawMessage{"acme.x": json.RawMessage(`{"any":"value"}`)}}); err != nil {
		t.Errorf("schema errors are not save errors: %v", err)
	}
	c := cloneSettings(Settings{Enabled: map[string]bool{"a.b": false}})
	c.Enabled["a.b"] = true
	if c2 := cloneSettings(c); !c2.Enabled["a.b"] {
		t.Error("clone")
	}
}

func TestIndexSettings(t *testing.T) {
	pub, _, err := index.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	var s settings.Settings
	in := `{"extensions": {"indexUrl": " https://ext.example.com/index.json ", "indexPublicKey": "` + pub + `", "acme.jira": {}}}`
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	got := Section.Get(s)
	if got.IndexURL != "https://ext.example.com/index.json" || got.IndexPublicKey != pub {
		t.Fatalf("settings = %+v", got)
	}
	if err := check(got); err != nil {
		t.Errorf("check: %v", err)
	}
	if u, key, own := got.indexSource(); u != got.IndexURL || key != pub || !own {
		t.Errorf("source = %q %q %v", u, key, own)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"indexUrl":"https://ext.example.com/index.json","indexPublicKey":"` + pub + `","acme.jira":{}}`; string(data) != want {
		t.Errorf("marshal = %s\nwant      %s", data, want)
	}
	if c := cloneSettings(got); c.IndexURL != got.IndexURL || c.IndexPublicKey != pub {
		t.Errorf("clone = %+v", c)
	}

	// The defaults: the official index and the key built into Flint.
	if u, key, own := (Settings{}).indexSource(); u != index.DefaultURL || key != index.OfficialPublicKey || own {
		t.Errorf("default source = %q %q %v", u, key, own)
	}
	// A value of the wrong type is dropped, like the other keys.
	if err := json.Unmarshal([]byte(`{"extensions": {"indexUrl": 5, "indexPublicKey": {"a": 1}}}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := Section.Get(s); got.IndexURL != "" || got.IndexPublicKey != "" {
		t.Errorf("wrong types kept: %+v", got)
	}

	for _, bad := range []Settings{
		{IndexURL: "http://ext.example.com/index.json"},
		{IndexURL: "ext.example.com/index.json"},
		{IndexURL: "file:///tmp/index.json"},
		{IndexURL: "https://user:pw@ext.example.com/index.json"},
		{IndexPublicKey: "not a key"},
		{IndexPublicKey: pub[:20]},
	} {
		if err := check(bad); err == nil {
			t.Errorf("%+v is saved", bad)
		}
	}
	if err := check(Settings{IndexURL: "http://127.0.0.1:8123/index.json"}); err != nil {
		t.Errorf("a loopback http index is refused: %v", err)
	}
}
