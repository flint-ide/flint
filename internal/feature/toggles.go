package feature

import (
	"maps"

	"flint/internal/settings"
)

// Toggles is the "features" key of settings.json: which optional built-in
// features (Module.Optional) are switched off. A missing id means on, so a new
// feature starts enabled and an old settings file keeps everything.
type Toggles struct {
	Enabled map[string]bool `json:"enabled"`
}

// Section registers the toggles with the settings package (Register).
var Section = &settings.Section[Toggles]{
	Key:       "features",
	Default:   func() Toggles { return Toggles{Enabled: map[string]bool{}} },
	Normalize: normalizeToggles,
	Clone:     func(t Toggles) Toggles { return Toggles{Enabled: maps.Clone(t.Enabled)} },
}

// normalizeToggles drops ids that can't name a feature (and a null map), and
// migrates renamed ids (renamedFeatures).
func normalizeToggles(t Toggles) Toggles {
	enabled := map[string]bool{}
	for id, on := range t.Enabled {
		if idPattern.MatchString(id) {
			enabled[id] = on
		}
	}
	for old, id := range renamedFeatures {
		if on, ok := enabled[old]; ok {
			if _, taken := enabled[id]; !taken {
				enabled[id] = on
			}
			delete(enabled, old)
		}
	}
	return Toggles{Enabled: enabled}
}

// renamedFeatures migrates the pre-rename id of the Projects feature (called
// Backlog on main for a short time, never released): features.enabled.backlog
// becomes features.enabled.projects when that is not set. It can be removed
// once nobody runs a main from before the rename.
var renamedFeatures = map[string]string{"backlog": "projects"}

// On reports whether the module runs with these toggles: required modules
// always do, optional ones unless switched off.
func (t Toggles) On(m Module) bool {
	if !m.Optional {
		return true
	}
	on, set := t.Enabled[m.ID]
	return !set || on
}
