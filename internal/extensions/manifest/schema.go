package manifest

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
)

// Settings is an extension's settings: a small JSON Schema subset that Flint
// renders as a Settings page and checks values against.
type Settings struct {
	Title      string              `json:"title,omitempty"`
	Properties map[string]Property `json:"properties"`
}

// Property is one setting. Only the keywords below are allowed; anything
// else in the manifest is an error (UnmarshalJSON).
type Property struct {
	Type        string          `json:"type"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Enum        []string        `json:"enum,omitempty"`
	Items       *Items          `json:"items,omitempty"`
	Minimum     *float64        `json:"minimum,omitempty"`
	Maximum     *float64        `json:"maximum,omitempty"`
	MaxLength   *int            `json:"maxLength,omitempty"`
	Pattern     string          `json:"pattern,omitempty"`
	Format      string          `json:"format,omitempty"`
}

// Items is the element type of an array setting: strings only.
type Items struct {
	Type string `json:"type"`
}

var propertyKeys = []string{"type", "title", "description", "default", "enum", "items", "minimum", "maximum", "maxLength", "pattern", "format"}

// UnmarshalJSON refuses keywords outside the supported subset, so a
// manifest never relies on a check Flint would silently skip.
func (p *Property) UnmarshalJSON(data []byte) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		if !slices.Contains(propertyKeys, k) {
			return fmt.Errorf("setting keyword %q is not supported", k)
		}
	}
	type plain Property
	return json.Unmarshal(data, (*plain)(p))
}

const maxSettings = 100

var settingName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

func checkSettings(p *problems, s Settings) {
	checkText(p, "settings.title", s.Title, maxTitleLen, false)
	if len(s.Properties) > maxSettings {
		p.add("settings: more than %d properties", maxSettings)
	}
	for _, name := range slices.Sorted(maps.Keys(s.Properties)) {
		checkProperty(p, name, s.Properties[name])
	}
}

func checkProperty(p *problems, name string, prop Property) {
	field := "settings." + name
	if !settingName.MatchString(name) {
		p.add("%s: names are letters, digits and underscores, starting with a letter", field)
	}
	checkText(p, field+".title", prop.Title, maxTitleLen, false)
	checkText(p, field+".description", prop.Description, maxDescLen, false)
	switch prop.Type {
	case "string":
		checkStringProperty(p, field, prop)
	case "integer", "number":
		if prop.Minimum != nil && prop.Maximum != nil && *prop.Minimum > *prop.Maximum {
			p.add("%s: minimum is above maximum", field)
		}
	case "boolean":
	case "array":
		if prop.Items == nil || prop.Items.Type != "string" {
			p.add("%s: arrays must have \"items\": {\"type\": \"string\"}", field)
		}
	default:
		p.add("%s: type %q is not string, integer, number, boolean or array", field, prop.Type)
		return
	}
	if (prop.Minimum != nil || prop.Maximum != nil) && prop.Type != "integer" && prop.Type != "number" {
		p.add("%s: minimum and maximum apply to numbers", field)
	}
	if prop.Type != "string" && (prop.Enum != nil || prop.MaxLength != nil || prop.Pattern != "" || prop.Format != "") {
		p.add("%s: enum, maxLength, pattern and format apply to strings", field)
	}
	if len(prop.Default) > 0 {
		if err := prop.Check(prop.Default); err != nil {
			p.add("%s.default: %w", field, err)
		}
	}
}

func checkStringProperty(p *problems, field string, prop Property) {
	if prop.MaxLength != nil && *prop.MaxLength < 0 {
		p.add("%s: maxLength is negative", field)
	}
	if prop.Pattern != "" {
		if _, err := regexp.Compile(prop.Pattern); err != nil {
			p.add("%s.pattern: %w", field, err)
		}
	}
	if prop.Format != "" && prop.Format != "hostname" && prop.Format != "uri" {
		p.add("%s.format %q is not hostname or uri", field, prop.Format)
	}
}

// Check reports why value is not a valid value of the setting.
func (prop Property) Check(value json.RawMessage) error {
	switch prop.Type {
	case "string":
		var s string
		if json.Unmarshal(value, &s) != nil {
			return fmt.Errorf("not a string")
		}
		return prop.checkString(s)
	case "integer", "number":
		var n float64
		if json.Unmarshal(value, &n) != nil {
			return fmt.Errorf("not a number")
		}
		if prop.Type == "integer" && n != math.Trunc(n) {
			return fmt.Errorf("not an integer")
		}
		if (prop.Minimum != nil && n < *prop.Minimum) || (prop.Maximum != nil && n > *prop.Maximum) {
			return fmt.Errorf("%v is out of range", n)
		}
	case "boolean":
		var b bool
		if json.Unmarshal(value, &b) != nil {
			return fmt.Errorf("not a boolean")
		}
	case "array":
		var list []string
		if json.Unmarshal(value, &list) != nil {
			return fmt.Errorf("not a list of strings")
		}
	}
	return nil
}

func (prop Property) checkString(s string) error {
	if prop.Enum != nil && !slices.Contains(prop.Enum, s) {
		return fmt.Errorf("%q is not one of the allowed values", s)
	}
	if prop.MaxLength != nil && len([]rune(s)) > *prop.MaxLength {
		return fmt.Errorf("longer than %d characters", *prop.MaxLength)
	}
	if prop.Pattern != "" {
		if re, err := regexp.Compile(prop.Pattern); err != nil || !re.MatchString(s) {
			return fmt.Errorf("%q does not match the pattern", s)
		}
	}
	if prop.Format == "hostname" && s != "" && !hostname.MatchString(s) {
		return fmt.Errorf("%q is not a host name", s)
	}
	return nil
}
