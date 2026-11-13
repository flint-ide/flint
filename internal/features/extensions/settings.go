package extensions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"flint/internal/extensions/index"
	"flint/internal/extensions/manifest"
	"flint/internal/settings"
)

// Settings is the "extensions" key of settings.json:
//
//	"extensions": {
//	  "enabled":    { "acme.jira": false },
//	  "agentTools": { "acme.jira": false },
//	  "decorations": { "acme.jira.refs": false },
//	  "indexUrl": "https://extensions.example.com/index.json",
//	  "indexPublicKey": "<base64 Ed25519 public key>",
//	  "requireSandbox": true,
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
	// Decorations turns editor decoration sets on or off by set id (missing:
	// the manifest's default).
	Decorations map[string]bool
	// IndexURL is the signed extension index Flint downloads when asked;
	// "" is index.DefaultURL. IndexPublicKey verifies it; "" is the key built
	// into Flint (index.OfficialPublicKey). Both are the user's own settings:
	// settings.json is per user, and nothing in a project can set them.
	IndexURL       string
	IndexPublicKey string
	// RequireSandbox: only extensions whose program runs sandboxed are
	// started (internal/extensions/sandbox); the others stay installed.
	RequireSandbox bool
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
	if len(s.Decorations) > 0 {
		if err := field("decorations", s.Decorations); err != nil {
			return nil, err
		}
	}
	if s.IndexURL != "" {
		if err := field("indexUrl", s.IndexURL); err != nil {
			return nil, err
		}
	}
	if s.IndexPublicKey != "" {
		if err := field("indexPublicKey", s.IndexPublicKey); err != nil {
			return nil, err
		}
	}
	if s.RequireSandbox {
		if err := field("requireSandbox", true); err != nil {
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
		case "decorations":
			out.Decorations = setFlags(raw)
		case "indexUrl":
			out.IndexURL = text(raw)
		case "indexPublicKey":
			out.IndexPublicKey = text(raw)
		case "requireSandbox":
			_ = json.Unmarshal(raw, &out.RequireSandbox) // not a boolean: off
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

// text decodes a string, trimmed ("" for anything else).
func text(raw json.RawMessage) string {
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v)
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

// setFlags decodes { set id: bool }, keeping ids that an extension could
// contribute: "<extension id>.<name>".
func setFlags(raw json.RawMessage) map[string]bool {
	var m map[string]bool
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	maps.DeleteFunc(m, func(id string, _ bool) bool { return !validSetID(id) })
	return m
}

func validSetID(id string) bool {
	parts := strings.SplitN(id, ".", 3)
	return len(id) <= 128 && len(parts) == 3 && parts[2] != "" && manifest.ValidID(parts[0]+"."+parts[1])
}

func check(s Settings) error {
	if len(s.Enabled) > maxEntries || len(s.AgentTools) > maxEntries || len(s.Values) > maxEntries || len(s.Decorations) > 10*maxEntries {
		return fmt.Errorf("extensions: at most %d entries", maxEntries)
	}
	if s.IndexURL != "" {
		if err := index.CheckURL(s.IndexURL); err != nil {
			return fmt.Errorf("extensions.indexUrl: %w", err)
		}
	}
	if s.IndexPublicKey != "" {
		if _, err := index.ParsePublicKey(s.IndexPublicKey); err != nil {
			return fmt.Errorf("extensions.indexPublicKey: %w", err)
		}
	}
	for id, v := range s.Values {
		if len(v) > maxValueSize {
			return fmt.Errorf("extensions.%s: larger than %d KiB", id, maxValueSize>>10)
		}
	}
	return nil
}

func cloneSettings(s Settings) Settings {
	return Settings{
		Enabled: maps.Clone(s.Enabled), AgentTools: maps.Clone(s.AgentTools), Decorations: maps.Clone(s.Decorations), Values: maps.Clone(s.Values),
		IndexURL: s.IndexURL, IndexPublicKey: s.IndexPublicKey, RequireSandbox: s.RequireSandbox,
	}
}

// enabled: on unless turned off.
func (s Settings) enabled(id string) bool {
	on, ok := s.Enabled[id]
	return !ok || on
}

// setEnabled: the user's choice for a decoration set, else def.
func (s Settings) setEnabled(set string, def bool) bool {
	if on, ok := s.Decorations[set]; ok {
		return on
	}
	return def
}

// toolsOffered: on unless turned off.
func (s Settings) toolsOffered(id string) bool {
	on, ok := s.AgentTools[id]
	return !ok || on
}

// indexSource is the index to download and the key that verifies it, with
// the built-in defaults filled in. key is "" while none is configured.
func (s Settings) indexSource() (url, key string, ownKey bool) {
	url, key = s.IndexURL, s.IndexPublicKey
	if url == "" {
		url = index.DefaultURL
	}
	if key == "" {
		return url, index.OfficialPublicKey, false
	}
	return url, key, true
}
