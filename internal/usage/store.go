package usage

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
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
// project × agent × model × task, so a few dozen a day is a busy day.
const (
	defaultMaxRows  = 20000
	defaultMaxChats = 500
	maxMarks        = 1000 // fired alerts and budget overrides
	fileVersion     = 1
	dayLayout       = "2006-01-02"
)

// Key is what a row aggregates over.
type Key struct {
	Day     string `json:"day"` // YYYY-MM-DD, local time
	Project string `json:"project"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
	Task    string `json:"task,omitempty"`
}

// Row is one day's totals for one key.
type Row struct {
	Key
	Totals
}

// Chat is one chat's totals over the retention, for Top chats.
type Chat struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Project string `json:"project"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
	Task    string `json:"task,omitempty"`
	LastDay string `json:"lastDay"`
	Totals  Totals `json:"totals"`
}

type file struct {
	Version int    `json:"version"`
	Rows    []Row  `json:"rows"`
	Chats   []Chat `json:"chats"`
	// Marks remember fired alerts ("alert/day/2026-11-01/usd") and budget
	// overrides ("override/task/<id>"), so neither repeats after a restart.
	Marks []string `json:"marks,omitempty"`
}

// Store is the usage file, loaded once and rewritten on every change.
type Store struct {
	path string
	now  func() time.Time

	mu        sync.Mutex
	f         file
	retention int
	maxRows   int
	maxChats  int
}

// Open loads the usage file at path. A file that cannot be decoded is moved
// aside (path + ".bad") and the store starts empty: the error is returned
// with a usable store, for the caller to log.
func Open(path string, retentionDays int) (*Store, error) {
	s := &Store{
		path: path, now: time.Now, retention: clampRetention(retentionDays),
		maxRows: defaultMaxRows, maxChats: defaultMaxChats,
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return s, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s.f); err != nil {
		s.f = file{}
		if mvErr := os.Rename(path, path+".bad"); mvErr != nil {
			return s, fmt.Errorf("decode %s: %w (and could not move it aside: %v)", path, err, mvErr)
		}
		return s, fmt.Errorf("decode %s: %w (moved to %s.bad, starting empty)", path, err, path)
	}
	return s, nil
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

// Add records one turn and saves the file.
func (s *Store) Add(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := s.today()
	k := Key{
		Day: today, Project: clean(e.Project, maxField), Agent: clean(e.Agent, maxField),
		Model: clean(e.Model, maxField), Task: clean(e.Task, maxField),
	}
	f := s.copyFile()
	if i := slices.IndexFunc(f.Rows, func(r Row) bool { return r.Key == k }); i >= 0 {
		f.Rows[i].Totals = f.Rows[i].Totals.add(e.Usage)
	} else {
		f.Rows = append(f.Rows, Row{Key: k, Totals: e.Usage})
	}
	f.Chats = addChat(f.Chats, k, clean(e.Chat, maxField), label(e.Label), e.Usage)
	return s.write(f)
}

// write saves f after pruning it; f's slices must not be shared with s.f.
func (s *Store) write(f file) error { return s.save(s.prune(f, s.today())) }

func addChat(chats []Chat, k Key, id, lbl string, t Totals) []Chat {
	out := slices.Clone(chats)
	i := slices.IndexFunc(out, func(c Chat) bool { return c.ID == id })
	if i < 0 {
		return append(out, Chat{
			ID: id, Label: lbl, Project: k.Project, Agent: k.Agent, Model: k.Model, Task: k.Task, LastDay: k.Day, Totals: t,
		})
	}
	c := out[i]
	c.Totals, c.LastDay = c.Totals.add(t), k.Day
	if k.Model != "" {
		c.Model = k.Model
	}
	if c.Label == "" {
		c.Label = lbl
	}
	out[i] = c
	return out
}

// prune drops what is older than the retention and enforces the caps
// (oldest rows first, least recently used chats first).
func (s *Store) prune(f file, today string) file {
	first := s.firstKept(today)
	f.Rows = slices.DeleteFunc(f.Rows, func(r Row) bool { return r.Day < first })
	f.Chats = slices.DeleteFunc(f.Chats, func(c Chat) bool { return c.LastDay < first })
	f.Marks = slices.DeleteFunc(f.Marks, func(m string) bool {
		d, ok := markDay(m)
		return ok && d < first
	})
	if over := len(f.Rows) - s.maxRows; over > 0 {
		slices.SortStableFunc(f.Rows, func(a, b Row) int { return strings.Compare(a.Day, b.Day) })
		f.Rows = f.Rows[over:]
	}
	if over := len(f.Chats) - s.maxChats; over > 0 {
		slices.SortStableFunc(f.Chats, func(a, b Chat) int { return strings.Compare(a.LastDay, b.LastDay) })
		f.Chats = f.Chats[over:]
	}
	if over := len(f.Marks) - maxMarks; over > 0 {
		f.Marks = f.Marks[over:]
	}
	return f
}

// firstKept is the oldest day inside the retention.
func (s *Store) firstKept(today string) string {
	t, _ := time.ParseInLocation(dayLayout, today, time.Local)
	return t.AddDate(0, 0, 1-s.retention).Format(dayLayout)
}

// save writes f and makes it current; on error the store keeps the old data.
func (s *Store) save(f file) error {
	f.Version = fileVersion
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return err
	}
	s.f = f
	return nil
}

func (s *Store) today() string { return s.now().Format(dayLayout) }

// snapshot returns a copy of the data pruned for today, without writing it
// (a store read on a later day must not show days past the retention).
// Called with s.mu held.
func (s *Store) snapshot() (file, string) {
	today := s.today()
	return s.prune(s.copyFile(), today), today
}

// copyFile is s.f with its own slices. Called with s.mu held.
func (s *Store) copyFile() file {
	return file{Rows: slices.Clone(s.f.Rows), Chats: slices.Clone(s.f.Chats), Marks: slices.Clone(s.f.Marks)}
}

// rows and chats are for tests: sorted copies.
func (s *Store) rows() []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.f.Rows)
	slices.SortStableFunc(out, func(a, b Row) int {
		return cmp.Or(strings.Compare(a.Day, b.Day), strings.Compare(a.Agent, b.Agent), strings.Compare(a.Model, b.Model))
	})
	return out
}

func (s *Store) chats() []Chat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.f.Chats)
}
