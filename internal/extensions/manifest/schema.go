package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The settings schema: a small subset of JSON Schema, enough for a generated
// Settings page. Supported keywords: type (string, integer, number, boolean,
// array of strings), enum (strings), default, minimum, maximum, maxLength,
// pattern, format (hostname, uri), title and description. Any other keyword
// is a manifest error, so an author never relies on one that is ignored.

const (
	maxProperties  = 50
	maxStringValue = 4096
	maxListItems   = 200
)

// Schema is contributes.settings.
type Schema struct {
	Title string `json:"title,omitempty"`
	// Properties keep the manifest's order, which is the page's order.
	Properties []Property `json:"properties"`
}

// Property is one setting.
type Property struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"` // string, integer, number, boolean, array
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	MaxLength   int      `json:"maxLength,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Format      string   `json:"format,omitempty"` // hostname, uri

	re *regexp.Regexp
}

// rawProperty is a property as written; unknown keywords are refused.
type rawProperty struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Default     json.RawMessage `json:"default"`
	Enum        []string        `json:"enum"`
	Minimum     *float64        `json:"minimum"`
	Maximum     *float64        `json:"maximum"`
	MaxLength   int             `json:"maxLength"`
	Pattern     string          `json:"pattern"`
	Format      string          `json:"format"`
	Items       *struct {
		Type string `json:"type"`
	} `json:"items"`
}

var keyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,39}$`)

func parseSchema(data []byte) (*Schema, error) {
	var top struct {
		Title      string          `json:"title"`
		Properties json.RawMessage `json:"properties"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&top); err != nil {
		return nil, err
	}
	if err := checkText("settings title", top.Title, 40, false); err != nil {
		return nil, err
	}
	s := &Schema{Title: top.Title}
	keys, values, err := orderedObject(top.Properties)
	if err != nil {
		return nil, fmt.Errorf("properties: %w", err)
	}
	if len(keys) > maxProperties {
		return nil, fmt.Errorf("properties: at most %d", maxProperties)
	}
	for i, key := range keys {
		if !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("property %q: use letters, digits, dashes and underscores", key)
		}
		p, err := parseProperty(key, values[i])
		if err != nil {
			return nil, fmt.Errorf("property %s: %w", key, err)
		}
		s.Properties = append(s.Properties, p)
	}
	return s, nil
}

// orderedObject splits a JSON object into its keys, in order, and values.
func orderedObject(data []byte) ([]string, []json.RawMessage, error) {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, errors.New("must be an object")
	}
	var keys []string
	var values []json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, _ := tok.(string)
		if slices.Contains(keys, key) {
			return nil, nil, fmt.Errorf("%q is listed twice", key)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		keys, values = append(keys, key), append(values, v)
	}
	return keys, values, nil
}

func parseProperty(key string, data []byte) (Property, error) {
	var r rawProperty
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Property{}, fmt.Errorf("%w (supported: type, enum, default, minimum, maximum, maxLength, pattern, format, title, description, items)", err)
	}
	p := Property{
		Key: key, Type: r.Type, Title: r.Title, Description: r.Description, Enum: r.Enum,
		Minimum: r.Minimum, Maximum: r.Maximum, MaxLength: r.MaxLength, Pattern: r.Pattern, Format: r.Format,
	}
	if err := checkText("title", p.Title, 60, false); err != nil {
		return p, err
	}
	if err := checkText("description", p.Description, 500, false); err != nil {
		return p, err
	}
	if err := p.checkKeywords(r); err != nil {
		return p, err
	}
	if p.Pattern != "" {
		re, err := regexp.Compile(`^(?:` + p.Pattern + `)$`)
		if err != nil {
			return p, fmt.Errorf("pattern: %w", err)
		}
		p.re = re
	}
	p.Default = p.zero()
	if len(r.Default) > 0 {
		var v any
		if err := json.Unmarshal(r.Default, &v); err != nil {
			return p, err
		}
		if err := p.Check(v); err != nil {
			return p, fmt.Errorf("default: %w", err)
		}
		p.Default = v
	}
	return p, nil
}

// checkKeywords: each keyword fits the type.
func (p *Property) checkKeywords(r rawProperty) error {
	numeric := p.Type == "integer" || p.Type == "number"
	stringy := p.Type == "string" || p.Type == "array"
	switch {
	case !slices.Contains([]string{"string", "integer", "number", "boolean", "array"}, p.Type):
		return fmt.Errorf("type %q is not supported (string, integer, number, boolean, array)", p.Type)
	case p.Type == "array" && (r.Items == nil || r.Items.Type != "string"):
		return errors.New(`an array needs "items": {"type": "string"}`)
	case p.Type != "array" && r.Items != nil:
		return errors.New(`"items" is only for arrays`)
	case len(p.Enum) > 0 && p.Type != "string":
		return errors.New(`"enum" is only for strings`)
	case len(p.Enum) > 50:
		return errors.New(`"enum": at most 50 values`)
	case (p.Minimum != nil || p.Maximum != nil) && !numeric:
		return errors.New(`"minimum" and "maximum" are only for numbers`)
	case p.Minimum != nil && p.Maximum != nil && *p.Minimum > *p.Maximum:
		return errors.New(`"minimum" is above "maximum"`)
	case (p.MaxLength != 0 || p.Pattern != "" || p.Format != "") && !stringy:
		return errors.New(`"maxLength", "pattern" and "format" are only for strings`)
	case p.MaxLength < 0 || p.MaxLength > maxStringValue:
		return fmt.Errorf(`"maxLength" must be between 1 and %d`, maxStringValue)
	case p.Format != "" && p.Format != "hostname" && p.Format != "uri":
		return fmt.Errorf(`format %q is not supported (hostname, uri)`, p.Format)
	}
	for _, e := range p.Enum {
		if err := checkText("enum value", e, 100, true); err != nil {
			return err
		}
	}
	return nil
}

// zero is the default when the manifest gives none.
func (p *Property) zero() any {
	switch {
	case len(p.Enum) > 0:
		return p.Enum[0]
	case p.Type == "string":
		return ""
	case p.Type == "boolean":
		return false
	case p.Type == "array":
		return []any{}
	case p.Minimum != nil && *p.Minimum > 0:
		return *p.Minimum
	case p.Maximum != nil && *p.Maximum < 0:
		return *p.Maximum
	}
	return float64(0)
}

// Check reports why v (decoded by encoding/json into any) is not a valid
// value of the property.
func (p *Property) Check(v any) error {
	switch p.Type {
	case "boolean":
		if _, ok := v.(bool); !ok {
			return errors.New("must be true or false")
		}
	case "integer", "number":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return errors.New("must be a number")
		}
		if p.Type == "integer" && n != math.Trunc(n) {
			return errors.New("must be a whole number")
		}
		if p.Minimum != nil && n < *p.Minimum || p.Maximum != nil && n > *p.Maximum {
			return fmt.Errorf("must be between %s and %s", bound(p.Minimum), bound(p.Maximum))
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return errors.New("must be text")
		}
		return p.checkString(s)
	case "array":
		list, ok := v.([]any)
		if !ok {
			return errors.New("must be a list of text")
		}
		if len(list) > maxListItems {
			return fmt.Errorf("at most %d entries", maxListItems)
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return errors.New("must be a list of text")
			}
			if err := p.checkString(s); err != nil {
				return fmt.Errorf("%q: %w", truncate(s, 40), err)
			}
		}
	}
	return nil
}

func (p *Property) checkString(s string) error {
	limit := p.MaxLength
	if limit == 0 {
		limit = maxStringValue
	}
	switch {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return errors.New("is not valid text")
	case utf8.RuneCountInString(s) > limit:
		return fmt.Errorf("at most %d characters", limit)
	case len(p.Enum) > 0 && !slices.Contains(p.Enum, s):
		return fmt.Errorf("must be one of %s", strings.Join(p.Enum, ", "))
	case p.re != nil && !p.re.MatchString(s):
		return fmt.Errorf("must match %s", p.Pattern)
	case p.Format == "hostname" && s != "" && !hostPattern.MatchString(s):
		return errors.New("must be a host name")
	case p.Format == "uri" && s != "" && !validURI(s):
		return errors.New("must be an http or https address")
	}
	return nil
}

func validURI(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func bound(f *float64) string {
	if f == nil {
		return "…"
	}
	return fmt.Sprint(*f)
}

// Values are the extension's settings as the extension receives them: every
// property of the schema, its stored value when that is valid, else its
// default. Unknown keys are left out. raw is the stored object (nil: none).
func (s *Schema) Values(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if s == nil {
		return out
	}
	var stored map[string]any
	_ = json.Unmarshal(raw, &stored) // not an object: all defaults
	for i := range s.Properties {
		p := &s.Properties[i]
		v, ok := stored[p.Key]
		if !ok || p.Check(v) != nil {
			v = p.Default
		}
		out[p.Key] = v
	}
	return out
}

// Check reports the first stored value that the schema refuses.
func (s *Schema) Check(raw json.RawMessage) error {
	if s == nil || len(raw) == 0 {
		return nil
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		return errors.New("must be an object")
	}
	for i := range s.Properties {
		p := &s.Properties[i]
		if v, ok := stored[p.Key]; ok {
			if err := p.Check(v); err != nil {
				return fmt.Errorf("%s %w", p.Key, err)
			}
		}
	}
	return nil
}
