package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// A Section is one top-level key of settings.json that a feature owns
// (internal/feature): its value type, default, normalization and check live
// with the feature instead of in Settings. Registered sections are read,
// normalized, validated, cloned and written with the built-in keys, so the
// file stays one flat document ("docker": {…} next to "theme": "dark") and a
// key moving between Settings and a section changes nothing for users.
//
//	var Section = &settings.Section[Docker]{Key: "docker", Default: defaultDocker, Normalize: normalize, Check: check}
//	settings.Register(Section) // once at startup, before Load (feature.Register does it)
//	host := Section.Get(s).Host
type Section[T any] struct {
	// Key is the JSON key; it must not be a built-in key or another section's.
	Key string
	// Default returns a fresh default value.
	Default func() T
	// Normalize replaces invalid parts of a decoded value (like Normalize does
	// for built-in keys); nil keeps the value as decoded.
	Normalize func(T) T
	// Check reports why a value would not be saved; nil accepts every value.
	Check func(T) error
	// Clone deep-copies a value that holds slices or maps; nil copies by value.
	Clone func(T) T
}

// Get returns the section's value in s, or its default when s has none (the
// section is not registered, or s was not built by Default, Load or Normalize).
func (sec *Section[T]) Get(s Settings) T {
	if v, ok := s.sections[sec.Key].(T); ok {
		return v
	}
	return sec.Default()
}

// With returns s with the section set to v; s itself is not changed.
func (sec *Section[T]) With(s Settings, v T) Settings {
	out := s
	out.sections = make(map[string]any, len(s.sections)+1)
	for k, x := range s.sections {
		out.sections[k] = x
	}
	out.sections[sec.Key] = v
	return out
}

// AnySection is a Section of any value type, as Register takes it.
type AnySection interface {
	SectionKey() string
	value(s Settings) any
	decode(raw json.RawMessage, base any) (any, error)
	normalize(v any) any
	check(v any) error
	clone(v any) any
}

// SectionKey is the section's JSON key.
func (sec *Section[T]) SectionKey() string { return sec.Key }

func (sec *Section[T]) value(s Settings) any { return sec.Get(s) }

// decode overlays raw on base (the value so far, else the default), so keys
// missing inside the section keep their values, like for built-in keys.
func (sec *Section[T]) decode(raw json.RawMessage, base any) (any, error) {
	v, ok := base.(T)
	if !ok {
		v = sec.Default()
	}
	v = sec.copyOf(v) // Unmarshal reuses slices: never write into a shared value
	err := json.Unmarshal(raw, &v)
	return v, err
}

func (sec *Section[T]) normalize(v any) any {
	t, ok := v.(T)
	if !ok {
		return sec.Default()
	}
	if sec.Normalize != nil {
		return sec.Normalize(t)
	}
	return t
}

func (sec *Section[T]) check(v any) error {
	t, ok := v.(T)
	if !ok {
		return fmt.Errorf("%s: unexpected value", sec.Key)
	}
	if sec.Check != nil {
		return sec.Check(t)
	}
	return nil
}

func (sec *Section[T]) clone(v any) any {
	if t, ok := v.(T); ok {
		return sec.copyOf(t)
	}
	return v
}

func (sec *Section[T]) copyOf(v T) T {
	if sec.Clone != nil {
		return sec.Clone(v)
	}
	return v
}

var (
	sectionsMu sync.RWMutex
	sections   []AnySection // in registration order, which is their order in the file
)

// Register adds sections, normally once at startup before settings are loaded
// (values decoded earlier don't have them). Registering the same section again
// does nothing; a key that is empty, built in or taken by another section panics:
// it is a programming error, and two features must never share a key.
func Register(secs ...AnySection) {
	sectionsMu.Lock()
	defer sectionsMu.Unlock()
	for _, sec := range secs {
		key := sec.SectionKey()
		if err := checkSectionKey(key); err != nil {
			panic(err)
		}
		dup := false
		for _, have := range sections {
			if have.SectionKey() != key {
				continue
			}
			if have != sec {
				panic(fmt.Errorf("settings: section %q registered twice", key))
			}
			dup = true
		}
		if !dup {
			sections = append(sections, sec)
		}
	}
}

// SectionKeys lists the registered sections' keys in file order.
func SectionKeys() []string {
	out := []string{}
	for _, sec := range registered() {
		out = append(out, sec.SectionKey())
	}
	return out
}

func registered() []AnySection {
	sectionsMu.RLock()
	defer sectionsMu.RUnlock()
	return sections[:len(sections):len(sections)] // append-only: the prefix never changes
}

func checkSectionKey(key string) error {
	switch {
	case key == "" || strings.TrimSpace(key) != key:
		return fmt.Errorf("settings: invalid section key %q", key)
	case builtinKeys()[key]:
		return fmt.Errorf("settings: section %q is a built-in key", key)
	}
	return nil
}

// builtinKeys are the JSON keys of Settings' own fields.
var builtinKeys = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeFor[Settings]()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			name = f.Name
		}
		out[name] = true
	}
	return out
})

// plainSettings has Settings' fields without its JSON methods.
type plainSettings Settings

// MarshalJSON writes the built-in keys, then every registered section (its
// default when s has no value), in one object.
func (s Settings) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(plainSettings(s))
	secs := registered()
	if err != nil || len(secs) == 0 {
		return data, err
	}
	var b bytes.Buffer
	b.Write(bytes.TrimSuffix(bytes.TrimSpace(data), []byte("}")))
	for _, sec := range secs {
		key, _ := json.Marshal(sec.SectionKey())
		v, err := json.Marshal(sec.value(s))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sec.SectionKey(), err)
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON overlays data on s: built-in keys as encoding/json does,
// registered sections over their current values. Unknown keys are ignored. A
// value of the wrong type loses only that value; the first such error is
// returned after everything else was decoded (decode tolerates it).
func (s *Settings) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, (*plainSettings)(s))
	if err != nil && !errors.As(err, new(*json.UnmarshalTypeError)) {
		return err
	}
	secs := registered()
	if len(secs) == 0 {
		return err
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return err
	}
	next := make(map[string]any, len(secs))
	for k, v := range s.sections {
		next[k] = v
	}
	for _, sec := range secs {
		r, ok := raw[sec.SectionKey()]
		if !ok {
			continue
		}
		v, decErr := sec.decode(r, next[sec.SectionKey()])
		next[sec.SectionKey()] = v
		if err == nil {
			err = decErr
		}
	}
	s.sections = next
	return err
}

// sectionDefaults is the default of every registered section.
func sectionDefaults() map[string]any {
	secs := registered()
	out := make(map[string]any, len(secs))
	for _, sec := range secs {
		out[sec.SectionKey()] = sec.value(Settings{})
	}
	return out
}

func normalizeSections(s Settings) map[string]any {
	secs := registered()
	out := make(map[string]any, len(secs))
	for _, sec := range secs {
		out[sec.SectionKey()] = sec.normalize(sec.value(s))
	}
	return out
}

func checkSections(s Settings) error {
	for _, sec := range registered() {
		if err := sec.check(sec.value(s)); err != nil {
			return err
		}
	}
	return nil
}

func cloneSections(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	byKey := map[string]AnySection{}
	for _, sec := range registered() {
		byKey[sec.SectionKey()] = sec
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sec, ok := byKey[k]; ok {
			v = sec.clone(v)
		}
		out[k] = v
	}
	return out
}
