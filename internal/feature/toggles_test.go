package feature

import (
	"slices"
	"testing"

	"flint/internal/settings"
)

// settingsHost is a Host that only answers Settings.
type settingsHost struct {
	Host
	s settings.Settings
}

func (h settingsHost) Settings() settings.Settings { return h.s }

func TestNewSkipsSwitchedOffOptionalModules(t *testing.T) {
	if err := Register(nil); err != nil { // registers the "features" section
		t.Fatal(err)
	}
	var built []string
	mod := func(id string, optional bool) Module {
		return Module{ID: id, Optional: optional, New: func(Host) Instance {
			built = append(built, id)
			return Instance{}
		}}
	}
	s := Section.With(settings.Default(), Toggles{Enabled: map[string]bool{"kube": false, "db": true, "core": false}})
	set := New([]Module{mod("core", false), mod("kube", true), mod("db", true), mod("docker", true)}, settingsHost{s: s})
	if want := []string{"core", "db", "docker"}; !slices.Equal(built, want) || !slices.Equal(set.Active(), want) {
		t.Errorf("built %v, active %v, want %v", built, set.Active(), want)
	}
	if (*Set)(nil).Active() != nil {
		t.Error("a nil Set has active modules")
	}
}

func TestTogglesRoundTripThroughSettingsJSON(t *testing.T) {
	if err := Register(nil); err != nil {
		t.Fatal(err)
	}
	var s settings.Settings
	if err := s.UnmarshalJSON([]byte(`{"features":{"enabled":{"docker":false,"Bad ID":false}}}`)); err != nil {
		t.Fatal(err)
	}
	got := Section.Get(settings.Normalize(s))
	if len(got.Enabled) != 1 || got.Enabled["docker"] {
		t.Errorf("toggles = %v", got.Enabled)
	}
	if !got.On(Module{ID: "todo"}) || got.On(Module{ID: "docker", Optional: true}) || !got.On(Module{ID: "kube", Optional: true}) {
		t.Error("On() disagrees with the toggles")
	}
}

func TestTogglesNormalizeReplacesNullMap(t *testing.T) {
	if normalizeToggles(Toggles{}).Enabled == nil {
		t.Error("nil map not replaced")
	}
}
