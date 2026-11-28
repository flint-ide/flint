package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"flint/internal/settings"
)

// base is a valid section with only the given fields changed.
func base(change func(s *Settings)) Settings {
	s := defaultSettings()
	if change != nil {
		change(&s)
	}
	return s
}

func TestSettings(t *testing.T) {
	cases := []struct {
		in      Settings
		norm    Settings
		invalid bool
	}{
		{base(nil), base(nil), false},
		{base(func(s *Settings) { s.MaxAttempts, s.FallbackAgent, s.ResumeAfterMinutes = 1, "codex", 720 }),
			base(func(s *Settings) { s.MaxAttempts, s.FallbackAgent, s.ResumeAfterMinutes = 1, "codex", 720 }), false},
		{base(func(s *Settings) { s.MaxAttempts = 0 }), base(nil), true},
		{base(func(s *Settings) { s.MaxAttempts = 11 }), base(nil), true},
		{base(func(s *Settings) { s.MaxAttempts, s.FallbackAgent = 2, "Codex CLI" }), base(func(s *Settings) { s.MaxAttempts = 2 }), true},
		{base(func(s *Settings) { s.MaxAttempts, s.ResumeAfterMinutes = 2, -1 }), base(func(s *Settings) { s.MaxAttempts = 2 }), true},
		{base(func(s *Settings) { s.MaxAttempts, s.ResumeAfterMinutes = 2, 721 }), base(func(s *Settings) { s.MaxAttempts = 2 }), true},
		{base(func(s *Settings) { s.Sandbox, s.SandboxNetwork = SandboxOn, true }), base(func(s *Settings) { s.Sandbox, s.SandboxNetwork = SandboxOn, true }), false},
		// On is the default: an unknown or missing value is on, an explicit off stays off.
		{base(func(s *Settings) { s.Sandbox = SandboxOff }), base(func(s *Settings) { s.Sandbox = SandboxOff }), false},
		{base(func(s *Settings) { s.Sandbox, s.SandboxNetwork = SandboxOff, true }), base(func(s *Settings) { s.Sandbox, s.SandboxNetwork = SandboxOff, true }), false},
		{base(func(s *Settings) { s.Sandbox = "docker" }), base(nil), true},
		{base(func(s *Settings) { s.Sandbox = "OFF" }), base(nil), true},
		{base(func(s *Settings) { s.Sandbox = "" }), base(nil), true},
		{base(func(s *Settings) { s.Nudges = 0 }), base(func(s *Settings) { s.Nudges = 0 }), false},
		{base(func(s *Settings) { s.Nudges = 5 }), base(func(s *Settings) { s.Nudges = 5 }), false},
		{base(func(s *Settings) { s.Nudges = 6 }), base(nil), true},
		{base(func(s *Settings) { s.Nudges = -1 }), base(nil), true},
		// Nil lists (an old file, JSON null) become empty ones.
		{base(func(s *Settings) { s.SandboxWritable, s.Schedules = nil, nil }), base(nil), false},
	}
	for _, c := range cases {
		if got := normalizeSettings(c.in); !reflect.DeepEqual(got, c.norm) {
			t.Errorf("normalize(%+v) = %+v, want %+v", c.in, got, c.norm)
		}
		if err := checkSettings(c.in); (err != nil) != c.invalid {
			t.Errorf("check(%+v) = %v", c.in, err)
		}
	}
	if d := Section.Default(); !reflect.DeepEqual(d, Settings{MaxAttempts: 3, Nudges: 2, Sandbox: "on", SandboxWritable: []string{}, Schedules: []Schedule{}}) {
		t.Errorf("default %+v", d)
	}
}

func TestSettingsMissingKeysKeepDefaults(t *testing.T) {
	settings.Register(Section)
	var s settings.Settings
	if err := json.Unmarshal([]byte(`{"workflows": {"fallbackAgent": "codex"}}`), &s); err != nil {
		t.Fatal(err)
	}
	want := base(func(s *Settings) { s.FallbackAgent = "codex" })
	if got := Section.Get(settings.Normalize(s)); !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %+v", got)
	}
}

func TestSettingsSandboxFromFile(t *testing.T) {
	settings.Register(Section)
	cases := []struct{ json, want string }{
		{`{}`, SandboxOn},
		{`{"workflows": {}}`, SandboxOn},
		{`{"workflows": {"sandbox": "on"}}`, SandboxOn},
		{`{"workflows": {"sandbox": "off"}}`, SandboxOff},
		{`{"workflows": {"sandbox": "nonsense"}}`, SandboxOn},
		{`{"workflows": {"sandbox": ""}}`, SandboxOn},
	}
	for _, c := range cases {
		var s settings.Settings
		if err := json.Unmarshal([]byte(c.json), &s); err != nil {
			t.Fatal(err)
		}
		if got := Section.Get(settings.Normalize(s)).Sandbox; got != c.want {
			t.Errorf("%s: sandbox %q, want %q", c.json, got, c.want)
		}
	}
}

func TestSandboxWritable(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	if err := os.Mkdir(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	many := make([]string, maxWritable+1)
	for i := range many {
		many[i] = fmt.Sprintf("/cache%d", i)
	}
	cases := []struct {
		list []string
		// kept is what loading keeps; err a part of the save's refusal ("" saves).
		kept []string
		err  string
	}{
		{[]string{cache, "~/cache"}, []string{cache, "~/cache"}, ""},
		{[]string{"relative/cache"}, []string{}, "not an absolute, clean path"},
		{[]string{cache + "/../cache"}, []string{}, "not an absolute, clean path"},
		{[]string{"~/a/../cache"}, []string{}, "not an absolute, clean path"},
		{[]string{"~/"}, []string{}, "not an absolute, clean path"},
		{[]string{"~user/cache"}, []string{}, "not an absolute, clean path"},
		{[]string{cache + "\n"}, []string{}, "control characters"},
		{[]string{filepath.Join(home, "missing")}, []string{filepath.Join(home, "missing")}, "is not a folder"},
		{[]string{file}, []string{file}, "is not a folder"},
		{many, many[:maxWritable], "at most 10 folders"},
	}
	for _, c := range cases {
		err := checkWritable(c.list, home)
		switch {
		case c.err == "" && err != nil:
			t.Errorf("check(%q) = %v", c.list, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("check(%q) = %v, want %q", c.list, err, c.err)
		}
		got := normalizeSettings(base(func(s *Settings) { s.SandboxWritable = c.list })).SandboxWritable
		if !reflect.DeepEqual(got, c.kept) {
			t.Errorf("normalize(%q) = %q, want %q", c.list, got, c.kept)
		}
	}
}

func TestScriptOptions(t *testing.T) {
	if opts := scriptOptions(base(func(s *Settings) { s.Sandbox, s.SandboxWritable = SandboxOff, []string{"/c"} }), "/home/me"); opts.Sandbox != nil {
		t.Errorf("sandbox off runs sandboxed: %+v", opts)
	}
	if opts := scriptOptions(base(nil), "/home/me"); opts.Sandbox == nil || opts.Sandbox.Network || len(opts.Sandbox.Writable) != 0 {
		t.Errorf("the default does not confine without network or extra folders: %+v", opts.Sandbox)
	}
	opts := scriptOptions(base(func(s *Settings) {
		s.Sandbox, s.SandboxNetwork, s.SandboxWritable = SandboxOn, true, []string{"~/.cache/go-build", "/opt/cache"}
	}), "/home/me")
	if opts.Sandbox == nil || !opts.Sandbox.Network || !reflect.DeepEqual(opts.Sandbox.Writable, []string{"/home/me/.cache/go-build", "/opt/cache"}) {
		t.Errorf("options %+v", opts.Sandbox)
	}
}
