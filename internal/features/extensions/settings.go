package extensions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"flint/internal/extensions/manifest"
	"flint/internal/settings"
)

// Settings is the "extensions" key of settings.json:
//
//	"extensions": {
//	  "enabled":    { "acme.jira": false },
//	  "agentTools": { "acme.jira": false },
//	  "acme.jira":  { "site": "acme.atlassian.net" }
//	}
//
// Each extension's object is kept as written, also for extensions that are
// not installed, so uninstalling and installing again keeps the values (the
// one place settings.json keeps keys Flint doesn't know). Values are checked
// against the extension's schema when Settings ▸ Extensions writes them and
// when they are handed to the extension (invalid ones become defaults);
// saving only checks sizes, so a newer manifest with a stricter schema never
// makes settings.json unsavable. Secrets are never settings.
type Settings struct {
	// Enabled: false turns an installed extension off (missing: on).
	Enabled map[string]bool
	// AgentTools: false keeps an extension's tools from agents (missing: offered).
	AgentTools map[string]bool
	// Values are the extensions' settings objects by id, as written.
	Values map[string]json.RawMessage
}

// Limits of the section.
const (
	maxEntries   = 500
	maxValueSize = 64 << 10
)

// Section is the "extensions" key.
var Section = &settings.Section[Settings]{
	Key:     "extensions",
	Default: func() Settings { return Settings{} },
	Check:   check,
	Clone:   cloneSettings,
}

// MarshalJSON writes enabled, agentTools, then each extension's object by id.
func (s Settings) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	field := func(key string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(data)
		return nil
	}
	if len(s.Enabled) > 0 {
		if err := field("enabled", s.Enabled); err != nil {
			return nil, err
		}
	}
	if len(s.AgentTools) > 0 {
		if err := field("agentTools", s.AgentTools); err != nil {
			return nil, err
		}
	}
	for _, id := range slices.Sorted(maps.Keys(s.Values)) {
		if err := field(id, s.Values[id]); err != nil {
			return nil, fmt.Errorf("extensions.%s: %w", id, err)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON replaces s with data. Entries whose key is not an
// extension id, or whose value has the wrong shape, are dropped.
func (s *Settings) UnmarshalJSON(data []byte) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	out := Settings{}
	for key, raw := range doc {
		switch key {
		case "enabled":
			out.Enabled = idFlags(raw)
		case "agentTools":
			out.AgentTools = idFlags(raw)
		default:
			trimmed := bytes.TrimSpace(raw)
			if manifest.ValidID(key) && len(trimmed) > 0 && trimmed[0] == '{' {
				if out.Values == nil {
					out.Values = map[string]json.RawMessage{}
				}
				var compact bytes.Buffer
				if json.Compact(&compact, trimmed) == nil {
					out.Values[key] = compact.Bytes()
				}
			}
		}
	}
	*s = out
	return nil
}

// idFlags decodes { id: bool }, keeping valid ids only.
func idFlags(raw json.RawMessage) map[string]bool {
	var m map[string]bool
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	maps.DeleteFunc(m, func(id string, _ bool) bool { return !manifest.ValidID(id) })
	return m
}

func check(s Settings) error {
	if len(s.Enabled) > maxEntries || len(s.AgentTools) > maxEntries || len(s.Values) > maxEntries {
		return fmt.Errorf("extensions: at most %d entries", maxEntries)
	}
	for id, v := range s.Values {
		if len(v) > maxValueSize {
			return fmt.Errorf("extensions.%s: larger than %d KiB", id, maxValueSize>>10)
		}
	}
	return nil
}

func cloneSettings(s Settings) Settings {
	return Settings{Enabled: maps.Clone(s.Enabled), AgentTools: maps.Clone(s.AgentTools), Values: maps.Clone(s.Values)}
}

// enabled: on unless turned off.
func (s Settings) enabled(id string) bool {
	on, ok := s.Enabled[id]
	return !ok || on
}

// toolsOffered: on unless turned off.
func (s Settings) toolsOffered(id string) bool {
	on, ok := s.AgentTools[id]
	return !ok || on
}
