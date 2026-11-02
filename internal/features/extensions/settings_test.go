package extensions

import (
	"encoding/json"
	"strings"
	"testing"

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
