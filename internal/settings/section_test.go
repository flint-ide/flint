package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testSection is a feature-style section; tests register it (idempotently).
type testSection struct {
	Name  string   `json:"name"`
	Limit int      `json:"limit"`
	Tags  []string `json:"tags"`
}

var testSec = &Section[testSection]{
	Key:     "zzTest",
	Default: func() testSection { return testSection{Limit: 5, Tags: []string{}} },
	Normalize: func(v testSection) testSection {
		v.Name = strings.TrimSpace(v.Name)
		if v.Limit < 1 || v.Limit > 10 {
			v.Limit = 5
		}
		if v.Tags == nil {
			v.Tags = []string{}
		}
		return v
	},
	Check: func(v testSection) error {
		if v.Limit < 1 || v.Limit > 10 {
			return errors.New("zzTest limit out of range")
		}
		return nil
	},
	Clone: func(v testSection) testSection { v.Tags = slices.Clone(v.Tags); return v },
}

var testFlag = &Section[bool]{Key: "zzFlag", Default: func() bool { return true }}

func registerTestSections(t *testing.T) {
	t.Helper()
	Register(testSec, testFlag)
	Register(testSec) // again: a no-op
}

func TestRegisterRejectsBadKeys(t *testing.T) {
	registerTestSections(t)
	for name, sec := range map[string]AnySection{
		"built-in":  &Section[bool]{Key: "theme", Default: func() bool { return false }},
		"empty":     &Section[bool]{Key: "", Default: func() bool { return false }},
		"spaces":    &Section[bool]{Key: " x", Default: func() bool { return false }},
		"duplicate": &Section[bool]{Key: "zzFlag", Default: func() bool { return false }},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) did not panic", sec.SectionKey())
				}
			}()
			Register(sec)
		})
	}
	if keys := SectionKeys(); !slices.Contains(keys, "zzTest") || !slices.Contains(keys, "zzFlag") {
		t.Errorf("SectionKeys() = %v", keys)
	}
}

func TestSectionDefaultsGetAndWith(t *testing.T) {
	registerTestSections(t)
	def := Default()
	if got := testSec.Get(def); got.Limit != 5 || got.Name != "" {
		t.Fatalf("default = %+v", got)
	}
	if !testFlag.Get(Settings{}) {
		t.Error("Get on settings without the section is not the default")
	}
	changed := testSec.With(def, testSection{Name: "x", Limit: 7})
	if testSec.Get(def).Name != "" || testSec.Get(changed).Name != "x" {
		t.Error("With changed its input")
	}
	if err := Validate(testSec.With(def, testSection{Limit: 99})); err == nil || !strings.Contains(err.Error(), "zzTest limit") {
		t.Errorf("Validate = %v, want the section's check", err)
	}
	n := Normalize(testSec.With(def, testSection{Name: " y ", Limit: 99}))
	if got := testSec.Get(n); got.Name != "y" || got.Limit != 5 || got.Tags == nil {
		t.Errorf("Normalize = %+v", got)
	}
}

func TestSectionCloneIsDeep(t *testing.T) {
	registerTestSections(t)
	s := testSec.With(Default(), testSection{Limit: 2, Tags: []string{"a"}})
	c := s.Clone()
	testSec.Get(c).Tags[0] = "changed"
	if testSec.Get(s).Tags[0] != "a" {
		t.Error("Clone shares the section's slice")
	}
}

func TestSectionFileRoundtrip(t *testing.T) {
	registerTestSections(t)
	path := filepath.Join(t.TempDir(), FileName)
	// Missing keys inside the section keep their defaults; a wrong type loses
	// only that value; unknown keys are ignored.
	data := `{"theme": "light", "zzTest": {"name": " n ", "tags": ["a"]}, "zzFlag": "no", "zzUnknown": 1}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Theme != ThemeLight {
		t.Errorf("Theme = %q", got.Theme)
	}
	if v := testSec.Get(got); v.Name != "n" || v.Limit != 5 || !slices.Equal(v.Tags, []string{"a"}) {
		t.Errorf("zzTest = %+v", v)
	}
	if !testFlag.Get(got) {
		t.Error("zzFlag lost its default on a wrong type")
	}

	saved, err := Save(path, testFlag.With(got, false))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["zzFlag"]) != "false" || !strings.Contains(string(doc["zzTest"]), `"limit": 5`) {
		t.Errorf("file = %s", raw)
	}
	if _, ok := doc["zzUnknown"]; ok {
		t.Error("an unknown key was written back")
	}
	// The sections follow the built-in keys, so the file reads as before.
	if strings.Index(string(raw), `"zzTest"`) < strings.Index(string(raw), `"github"`) {
		t.Errorf("sections are not after the built-in keys:\n%s", raw)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if testFlag.Get(loaded) || testSec.Get(loaded).Name != testSec.Get(saved).Name {
		t.Errorf("reloaded %+v / %v", testSec.Get(loaded), testFlag.Get(loaded))
	}
}

func TestUnmarshalFromZeroSettings(t *testing.T) {
	registerTestSections(t)
	// What the Wails binding does with Save's argument: decode into a zero value.
	var s Settings
	if err := json.Unmarshal([]byte(`{"zzTest": {"name": "w", "limit": 3}}`), &s); err != nil {
		t.Fatal(err)
	}
	if v := testSec.Get(s); v.Name != "w" || v.Limit != 3 {
		t.Errorf("zzTest = %+v", v)
	}
	if !testFlag.Get(s) {
		t.Error("a missing section is not its default")
	}
	if err := json.Unmarshal([]byte(`{"zzTest": {"limit": "x"}}`), &s); err == nil {
		t.Error("a wrong type inside a section is not reported")
	}
}
