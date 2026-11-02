package docker

import (
	"encoding/json"
	"strings"
	"testing"

	"flint/internal/settings"
)

func TestSettings(t *testing.T) {
	settings.Register(Section)
	if d := Section.Get(settings.Default()); d.Host != "" || d.LogTailLines != 500 {
		t.Fatalf("defaults %+v", d)
	}
	n := Section.With(settings.Default(), Settings{Host: "  unix:///tmp/d.sock ", LogTailLines: 0})
	if got := Section.Get(settings.Normalize(n)); got.Host != "unix:///tmp/d.sock" || got.LogTailLines != 500 {
		t.Fatalf("normalized %+v", got)
	}
	n = Section.With(n, Settings{LogTailLines: 99999})
	if got := Section.Get(settings.Normalize(n)).LogTailLines; got != MaxLogTailLines {
		t.Fatalf("clamp %d", got)
	}
	hosts := map[string]bool{"": true, "unix:///var/run/docker.sock": true, "/var/run/docker.sock": true, "tcp://h:2376": true, "https://h": true, "ssh://h": false, "a b": false, "unix:///x\ny": false}
	for host, ok := range hosts {
		err := Check(Settings{Host: host, LogTailLines: 10})
		if (err == nil) != ok {
			t.Errorf("host %q: err = %v", host, err)
		}
	}
	if err := settings.Validate(Section.With(settings.Default(), Settings{LogTailLines: -1})); err == nil || !strings.Contains(err.Error(), "log lines") {
		t.Errorf("err = %v", err)
	}
}

// The key and shape in settings.json are what they were before the feature
// owned them, so existing files keep working.
func TestSettingsJSONShape(t *testing.T) {
	settings.Register(Section)
	var s settings.Settings
	if err := json.Unmarshal([]byte(`{"docker": {"host": "tcp://h:2376", "logTailLines": 42}}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := Section.Get(s); got.Host != "tcp://h:2376" || got.LogTailLines != 42 {
		t.Fatalf("decoded %+v", got)
	}
	data, err := json.Marshal(settings.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"docker":{"host":"","logTailLines":500}`) {
		t.Errorf("encoded %s", data)
	}
}
