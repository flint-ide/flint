package db

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"flint/internal/config"
	"flint/internal/fuzzy"
)

const (
	// DefaultHistoryLimit is how many statements are kept per connection.
	DefaultHistoryLimit = 5000
	// maxHistorySQL caps one stored statement: a pasted multi-megabyte INSERT script
	// would otherwise make the log (and every load of it) huge.
	maxHistorySQL = 256 << 10
	// maxHistoryList caps what one List call sends to the WebView.
	maxHistoryList = 500
)

// History kinds: what produced an entry.
const (
	HistoryQuery          = ""                // a statement run from a console
	HistoryExplain        = "explain"         // EXPLAIN of the recorded statement
	HistoryExplainAnalyze = "explain-analyze" // EXPLAIN ANALYZE (Postgres) / PIPELINE (ClickHouse)
	HistoryEdit           = "edit"            // rows edited in the result grid (the generated SQL)
)

// HistoryEntry is one executed statement of a connection.
type HistoryEntry struct {
	// ID is unique within the connection: unix microseconds of the run, bumped on collision
	// (microseconds stay below 2^53, so the ID survives as a JS number).
	ID       int64  `json:"id"`
	Database string `json:"database"` // resolved database name ("" = ClickHouse server default)
	SQL      string `json:"sql"`
	// At is the start of the run in unix milliseconds.
	At         int64  `json:"at"`
	DurationMs int64  `json:"durationMs"`
	Rows       int64  `json:"rows"` // rows returned, or affected for DML
	Error      string `json:"error,omitempty"`
	Kind       string `json:"kind,omitempty"`
	// Truncated marks SQL cut at maxHistorySQL.
	Truncated bool `json:"truncated,omitempty"`
}

// History keeps the last `limit` statements of each connection in memory and in
// <dir>/<connID>.jsonl. Recording appends one line (O_APPEND, a single write); the file
// is rewritten atomically without the oldest entries once it holds 25% more than limit.
// Connection profiles and passwords never reach it; password literals in statements
// (CREATE ROLE … PASSWORD '…') are masked.
type History struct {
	dir   string
	limit int

	mu   sync.Mutex
	logs map[string]*historyLog // loaded lazily, per connection
}

type historyLog struct {
	entries []HistoryEntry // oldest first, at most limit
	lines   int            // lines in the file (≥ len(entries) until compacted)
	lastID  int64
}

// OpenHistory returns a history stored in dir; limit <= 0 means DefaultHistoryLimit.
// Nothing is read until a connection's history is first used.
func OpenHistory(dir string, limit int) *History {
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	return &History{dir: dir, limit: limit, logs: map[string]*historyLog{}}
}

// Record appends an entry to the connection's history and returns it as stored.
func (h *History) Record(connID string, e HistoryEntry) (HistoryEntry, error) {
	if !validHistoryID(connID) {
		return e, fmt.Errorf("invalid connection id %q", connID)
	}
	e.SQL, e.Truncated = clipSQL(redactPasswords(e.SQL))
	e.Error = redactPasswords(e.Error)

	h.mu.Lock()
	defer h.mu.Unlock()
	log, err := h.load(connID)
	if err != nil {
		return e, err
	}
	e.ID = max(e.ID, log.lastID+1)
	if e.ID <= 0 {
		e.ID = time.Now().UnixMicro()
	}
	log.lastID = e.ID

	line, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	if err := appendLine(h.path(connID), line); err != nil {
		return e, err
	}
	log.lines++
	log.entries = append(log.entries, e)
	if over := len(log.entries) - h.limit; over > 0 {
		// A new slice so List results handed out earlier stay untouched.
		log.entries = slices.Clone(log.entries[over:])
	}
	if log.lines > h.limit+h.limit/4 {
		if err := h.rewrite(connID, log); err != nil {
			return e, err
		}
	}
	return e, nil
}

// HistoryFilter selects entries for List.
type HistoryFilter struct {
	// Database filters to one database; AllDatabases ignores it.
	Database     string `json:"database"`
	AllDatabases bool   `json:"allDatabases"`
	// Filter ranks entries by fuzzy match on their SQL; "" keeps newest first.
	Filter string `json:"filter"`
	Limit  int    `json:"limit"`
}

// List returns matching entries of a connection, newest first (or best match first
// when filtering), at most q.Limit (capped at maxHistoryList).
func (h *History) List(connID string, q HistoryFilter) ([]HistoryEntry, error) {
	if !validHistoryID(connID) {
		return nil, fmt.Errorf("invalid connection id %q", connID)
	}
	limit := q.Limit
	if limit <= 0 || limit > maxHistoryList {
		limit = maxHistoryList
	}
	h.mu.Lock()
	log, err := h.load(connID)
	var entries []HistoryEntry
	if err == nil {
		entries = log.entries // never mutated in place, safe to read unlocked
	}
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}

	newest := make([]HistoryEntry, 0, min(len(entries), 1024))
	for i := len(entries) - 1; i >= 0; i-- {
		if q.AllDatabases || entries[i].Database == q.Database {
			newest = append(newest, entries[i])
		}
	}
	filter := strings.TrimSpace(q.Filter)
	if filter == "" {
		return newest[:min(limit, len(newest))], nil
	}
	targets := make([]fuzzy.Target, len(newest))
	for i, e := range newest {
		targets[i] = fuzzy.Prepare(flattenSQL(e.SQL))
	}
	matches := fuzzy.SearchIndices(filter, targets, limit)
	out := make([]HistoryEntry, len(matches))
	for i, idx := range matches {
		out[i] = newest[idx]
	}
	return out, nil
}

// Clear removes a connection's history (memory and file), e.g. when the profile is deleted.
func (h *History) Clear(connID string) error {
	if !validHistoryID(connID) {
		return fmt.Errorf("invalid connection id %q", connID)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.logs, connID)
	if err := os.Remove(h.path(connID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (h *History) path(connID string) string {
	return filepath.Join(h.dir, connID+".jsonl")
}

// load returns the connection's log, reading the file on first use; callers hold h.mu.
// Malformed lines (e.g. one torn by a crash mid-append) are skipped.
func (h *History) load(connID string) (*historyLog, error) {
	if log, ok := h.logs[connID]; ok {
		return log, nil
	}
	log := &historyLog{}
	data, err := os.ReadFile(h.path(connID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read query history: %w", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 4*maxHistorySQL)
	for sc.Scan() {
		log.lines++
		var e HistoryEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		log.entries = append(log.entries, e)
		log.lastID = max(log.lastID, e.ID)
	}
	if over := len(log.entries) - h.limit; over > 0 {
		log.entries = slices.Clone(log.entries[over:])
	}
	h.logs[connID] = log
	return log, nil
}

// rewrite replaces the file with exactly the in-memory entries (atomic temp + rename).
func (h *History) rewrite(connID string, log *historyLog) error {
	var buf bytes.Buffer
	for _, e := range log.entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := config.WriteFileAtomic(h.path(connID), buf.Bytes(), 0o600); err != nil {
		return err
	}
	log.lines = len(log.entries)
	return nil
}

// appendLine writes line + "\n" in one write on an O_APPEND file, so concurrent
// appenders never interleave inside a line.
func appendLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open query history: %w", err)
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("append query history: %w", werr)
	}
	return cerr
}

// validHistoryID guards the file name: connection ids are hex, but they reach us from the UI.
func validHistoryID(id string) bool {
	return id != "" && len(id) <= 64 && !strings.ContainsAny(id, `/\.`+"\x00")
}

func clipSQL(sql string) (string, bool) {
	if len(sql) <= maxHistorySQL {
		return sql, false
	}
	cut := maxHistorySQL
	for cut > 0 && !utf8.RuneStart(sql[cut]) {
		cut--
	}
	return sql[:cut], true
}

// flattenSQL collapses whitespace so a filter can match across line breaks.
func flattenSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// passwordLiteral matches the secret of PASSWORD '…' (Postgres roles) and
// IDENTIFIED [WITH method] BY '…' (ClickHouse users), with ” escapes inside.
var passwordLiteral = regexp.MustCompile(`(?i)(\bPASSWORD\s+|\bIDENTIFIED\s+(?:WITH\s+\w+\s+)?BY\s+)'(?:[^']|'')*'`)

// redactPasswords masks password literals in role/user statements.
func redactPasswords(sql string) string {
	if !strings.Contains(strings.ToUpper(sql), "PASSWORD") && !strings.Contains(strings.ToUpper(sql), "IDENTIFIED") {
		return sql
	}
	return passwordLiteral.ReplaceAllString(sql, "${1}'***'")
}
