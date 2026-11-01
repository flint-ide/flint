package usage

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"flint/internal/config"
)

// FileName is the usage file in the config directory.
const FileName = "usage.json"

// Retention limits in days (settings.Usage.RetentionDays).
const (
	MinRetentionDays     = 7
	MaxRetentionDays     = 3650
	DefaultRetentionDays = 90
)

// Caps that bound the file whatever the retention: a row is one day ×
// project × agent × model × task × chat, so a few dozen a day is a busy day.
const (
	defaultMaxRows    = 20000
	defaultMaxChats   = 500
	maxAlerts         = 1000 // fired alerts remembered (dropping one can only repeat a notice)
	fileVersion       = 1
	dayLayout         = "2006-01-02"
	defaultFlushDelay = 2 * time.Second
)

// Key is what a row aggregates over.
type Key struct {
	Day     string `json:"day"` // YYYY-MM-DD, local time
	Project string `json:"project"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
	Task    string `json:"task,omitempty"`
	Chat    string `json:"chat,omitempty"`
}

// Row is one day's totals for one key.
type Row struct {
	Key
	Totals
}

// chatInfo is what is remembered about a chat besides its rows.
type chatInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Project string `json:"project"`
	Agent   string `json:"agent"`
	Task    string `json:"task,omitempty"`
	LastDay string `json:"lastDay"`
}

// Override is a "Continue anyway": new turns may run although the budget
// is spent, until Spent reaches Until (another budget's worth) or the limit
// changes.
type Override struct {
	Scope string  `json:"scope"`
	Key   string  `json:"key"`
	Kind  string  `json:"kind"`
	Limit float64 `json:"limit"`
	Until float64 `json:"until"`
}

type file struct {
	Version int        `json:"version"`
	Rows    []Row      `json:"rows"`
	Chats   []chatInfo `json:"chats"`
	// Alerts are the budget thresholds already reported
	// ("day/2026-11-01/usd/5"), so none repeats after a restart.
	Alerts    []string   `json:"alerts,omitempty"`
	Overrides []Override `json:"overrides,omitempty"`
}

// Store is the usage file, loaded once, changed in memory and written a
// moment after changes (Flush, Close).
type Store struct {
	path string
	now  func() time.Time
	// OnError receives errors of background writes (set before use).
	OnError func(error)

	mu         sync.Mutex
	f          file
	index      map[Key]int // row of a key in f.Rows
	retention  int
	maxRows    int
	maxChats   int
	flushDelay time.Duration // 0 writes on every change
	dirty      bool
	timer      *time.Timer
	closed     bool
	// readOnly says why nothing is written: the file could not be read, or
	// a newer Flint wrote it. Usage is still counted in memory.
	readOnly error
}

// Open loads the usage file at path. A missing file starts empty. A file
// that can't be decoded is moved aside (path.bad-<time>, never replacing
// an earlier one) and the store starts empty. A file that can't be read, or
// was written by a newer Flint, is never overwritten: the store keeps
// counting in memory only and Problem says why. Errors are returned with a
// usable store, for the caller to log.
func Open(path string, retentionDays int) (*Store, error) {
	s := &Store{
		path: path, now: time.Now, retention: clampRetention(retentionDays),
		maxRows: defaultMaxRows, maxChats: defaultMaxChats, flushDelay: defaultFlushDelay,
	}
	defer s.reindex()
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		s.readOnly = fmt.Errorf("could not read %s (%v): agent usage is not saved until it can be read", path, err)
		return s, s.readOnly
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		aside, mvErr := moveAside(path, s.now())
		if mvErr != nil {
			s.readOnly = fmt.Errorf("could not decode %s (%v) or move it aside (%v): agent usage is not saved", path, err, mvErr)
			return s, s.readOnly
		}
		return s, fmt.Errorf("decode %s: %w (moved to %s, starting empty)", path, err, aside)
	}
	if f.Version > fileVersion {
		s.readOnly = fmt.Errorf("%s was written by a newer Flint (format %d): shown read-only, new usage is not saved", path, f.Version)
		err = s.readOnly
	}
	s.f = f
	return s, err
}

// moveAside renames path to path.bad-<time>, never over an existing file.
func moveAside(path string, now time.Time) (string, error) {
	base := path + ".bad-" + now.Format("20060102-150405")
	for i := 0; ; i++ {
		target := base
		if i > 0 {
			target += "-" + strconv.Itoa(i)
		}
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			return target, os.Rename(path, target)
		} else if err != nil || i > 100 {
			return "", fmt.Errorf("no free name next to %s", path)
		}
	}
}

func clampRetention(days int) int {
	if days == 0 {
		return DefaultRetentionDays
	}
	return min(max(days, MinRetentionDays), MaxRetentionDays)
}

// SetRetention changes how many days are kept; older data goes on the next write.
// A nil store (an App built without one, in tests) ignores it.
func (s *Store) SetRetention(days int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retention = clampRetention(days)
}

// Problem says why usage is not being saved ("" when it is).
func (s *Store) Problem() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly != nil {
		return s.readOnly.Error()
	}
	return ""
}

// Add records one model's part of a turn.
func (s *Store) Add(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := Key{
		Day: s.today(), Project: clean(e.Project, maxField), Agent: clean(e.Agent, maxField),
		Model: clean(e.Model, maxField), Task: clean(e.Task, maxField), Chat: clean(e.Chat, maxField),
	}
	if i, ok := s.index[k]; ok {
		s.f.Rows[i].Totals = s.f.Rows[i].Totals.add(e.Usage)
	} else {
		s.index[k] = len(s.f.Rows)
		s.f.Rows = append(s.f.Rows, Row{Key: k, Totals: e.Usage})
	}
	s.touchChat(k, label(e.Label))
	return s.changed()
}

func (s *Store) touchChat(k Key, lbl string) {
	if k.Chat == "" {
		return
	}
	i := slices.IndexFunc(s.f.Chats, func(c chatInfo) bool { return c.ID == k.Chat })
	if i < 0 {
		s.f.Chats = append(s.f.Chats, chatInfo{ID: k.Chat, Label: lbl, Project: k.Project, Agent: k.Agent, Task: k.Task, LastDay: k.Day})
		return
	}
	c := &s.f.Chats[i]
	c.LastDay = k.Day
	if c.Label == "" {
		c.Label = lbl
	}
}

// changed schedules a write; with no delay (tests) or after Close it writes
// now. Called with s.mu held.
func (s *Store) changed() error {
	s.dirty = true
	if s.flushDelay <= 0 || s.closed {
		return s.flushLocked()
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(s.flushDelay, func() {
			if err := s.Flush(); err != nil && s.OnError != nil {
				s.OnError(err)
			}
		})
	}
	return nil
}

// Flush writes pending changes now.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

// Close writes pending changes; later changes are written at once.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.dirty || s.readOnly != nil {
		return nil
	}
	s.f = s.prune(s.f, s.today())
	s.reindex()
	s.f.Version = fileVersion
	data, err := json.Marshal(s.f)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return err // still dirty: the next change or Close tries again
	}
	s.dirty = false
	return nil
}

func (s *Store) reindex() {
	s.index = make(map[Key]int, len(s.f.Rows))
	for i, r := range s.f.Rows {
		s.index[r.Key] = i
	}
}

// prune drops what is older than the retention and enforces the caps
// (oldest rows first, least recently used chats first). Overrides are kept
// while their day is kept or their task has usage; alerts are capped.
func (s *Store) prune(f file, today string) file {
	first := s.firstKept(today)
	f.Rows = slices.DeleteFunc(f.Rows, func(r Row) bool { return r.Day < first })
	if over := len(f.Rows) - s.maxRows; over > 0 {
		slices.SortStableFunc(f.Rows, func(a, b Row) int { return strings.Compare(a.Day, b.Day) })
		f.Rows = f.Rows[over:]
	}
	f.Chats = slices.DeleteFunc(f.Chats, func(c chatInfo) bool { return c.LastDay < first })
	if over := len(f.Chats) - s.maxChats; over > 0 {
		slices.SortStableFunc(f.Chats, func(a, b chatInfo) int { return strings.Compare(a.LastDay, b.LastDay) })
		f.Chats = f.Chats[over:]
	}
	f.Alerts = slices.DeleteFunc(f.Alerts, func(m string) bool {
		d, ok := alertDay(m)
		return ok && d < first
	})
	if over := len(f.Alerts) - maxAlerts; over > 0 {
		f.Alerts = f.Alerts[over:]
	}
	tasks := map[string]bool{}
	for _, r := range f.Rows {
		tasks[r.Task] = true
	}
	f.Overrides = slices.DeleteFunc(f.Overrides, func(o Override) bool {
		if o.Scope == ScopeDay {
			return o.Key < first
		}
		return !tasks[o.Key]
	})
	return f
}

// firstKept is the oldest day inside the retention.
func (s *Store) firstKept(today string) string {
	t, _ := time.ParseInLocation(dayLayout, today, time.Local)
	return t.AddDate(0, 0, 1-s.retention).Format(dayLayout)
}

func (s *Store) today() string { return s.now().Format(dayLayout) }

// snapshot returns a copy of the data pruned for today (a store read on a
// later day must not show days past the retention). Called with s.mu held.
func (s *Store) snapshot() (file, string) {
	today := s.today()
	f := file{
		Rows: slices.Clone(s.f.Rows), Chats: slices.Clone(s.f.Chats),
		Alerts: slices.Clone(s.f.Alerts), Overrides: slices.Clone(s.f.Overrides),
	}
	return s.prune(f, today), today
}

// rows and chats are for tests: sorted copies.
func (s *Store) rows() []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.f.Rows)
	slices.SortStableFunc(out, func(a, b Row) int {
		return cmp.Or(strings.Compare(a.Day, b.Day), strings.Compare(a.Agent, b.Agent), strings.Compare(a.Model, b.Model), strings.Compare(a.Chat, b.Chat))
	})
	return out
}

func (s *Store) chats() []chatInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.f.Chats)
}
