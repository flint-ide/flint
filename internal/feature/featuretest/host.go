// Package featuretest is an in-memory feature.Host for the tests of feature
// modules: settings, roots and state.json keys are plain fields, events and
// opened links are recorded, and nothing touches the disk or a window.
package featuretest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"flint/internal/feature"
	"flint/internal/settings"
	"flint/internal/terminal"
)

var _ feature.Host = (*Host)(nil)

// Host implements feature.Host. Its zero value is not ready: use New.
type Host struct {
	Ctx context.Context
	// Version is what Version reports ("v1.0.0" by default).
	VersionString string

	// Dir is ConfigDir (a test's temporary folder).
	Dir string
	// Answer is what Confirm returns; Choice what Choose returns.
	Answer bool
	Choice string

	mu       sync.Mutex
	settings settings.Settings
	roots    []string
	trusted  map[string]bool
	asked    []string
	state    map[string]json.RawMessage
	events   []Event
	opened   []string
	streams  map[string]terminal.Stream
}

// Event is one Emit call.
type Event struct {
	Name string
	Data []any
}

// New returns a host with default settings and a context that ends with the test.
func New(ctx context.Context) *Host {
	return &Host{
		Ctx: ctx, VersionString: "v1.0.0", settings: settings.Default(),
		state: map[string]json.RawMessage{}, streams: map[string]terminal.Stream{},
	}
}

func (h *Host) Context() context.Context { return h.Ctx }

func (h *Host) Settings() settings.Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings.Clone()
}

// SetSettings replaces the settings the feature reads.
func (h *Host) SetSettings(s settings.Settings) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.settings = s
}

func (h *Host) Roots() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.roots...)
}

func (h *Host) Trusted(path string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.trusted[path]
}

// SetTrusted marks folders as trusted.
func (h *Host) SetTrusted(paths ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.trusted == nil {
		h.trusted = map[string]bool{}
	}
	for _, p := range paths {
		h.trusted[p] = true
	}
}

func (h *Host) ConfigDir() string { return h.Dir }

func (h *Host) Confirm(title, message, _ string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, title+"\n"+message)
	return h.Answer
}

func (h *Host) Choose(title, message string, _ []string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, title+"\n"+message)
	return h.Choice
}

// Asked returns the questions Confirm and Choose were asked.
func (h *Host) Asked() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.asked...)
}

// SetRoots replaces the open project folders.
func (h *Host) SetRoots(roots ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.roots = roots
}

func (h *Host) Emit(event string, data ...any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, Event{event, data})
}

// Events returns the events emitted so far.
func (h *Host) Events() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Event{}, h.events...)
}

func (h *Host) OpenURL(link string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opened = append(h.opened, link)
	return nil
}

// Opened returns the links passed to OpenURL.
func (h *Host) Opened() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.opened...)
}

func (h *Host) Version() string { return h.VersionString }

func (h *Host) LoadState(key string, v any) error {
	h.mu.Lock()
	raw, ok := h.state[key]
	h.mu.Unlock()
	if !ok {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func (h *Host) SaveState(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state[key] = raw
	return nil
}

// State returns the raw state of key (nil when never saved).
func (h *Host) State(key string) json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state[key]
}

func (h *Host) StartStream(termID, _ string, st terminal.Stream) error {
	if st.Reader == nil {
		return errors.New("no output")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.streams[termID] = st
	return nil
}
