package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultRowLimit = 1000
	maxRowLimit     = 200_000
)

// idlePoolTTL is how long the pool of a non-default database may sit unused before it is
// closed. Browsing many databases would otherwise pin up to 4 server connections each.
const idlePoolTTL = time.Minute

// ErrReadOnly is returned when a write statement targets a read-only connection.
var ErrReadOnly = errors.New("connection is read-only: only SELECT-like statements are allowed")

// sessionKey identifies one pool: a connection profile pointed at one database of its server.
type sessionKey struct {
	connID   string
	database string // resolved by keyFor; "" only for ClickHouse's server-side default
}

func keyFor(conn Connection, database string) sessionKey {
	return sessionKey{connID: conn.ID, database: conn.WithDatabase(database).DefaultDatabase()}
}

// Manager lazily opens sessions, runs queries and caches schemas per connection and database.
type Manager struct {
	store *Store
	// open dials a session; tests replace it to observe dials without a server.
	open func(ctx context.Context, c Connection, password string) (session, error)
	// now is the clock for idle eviction; tests replace it.
	now func() time.Time

	mu       sync.Mutex
	sessions map[sessionKey]session
	schemas  map[sessionKey][]Table
	running  map[string]context.CancelFunc
	// active counts calls currently using each pool; lastUsed is when the last one ended.
	// Only pools with no active calls and a lastUsed are ever evicted.
	active   map[sessionKey]int
	lastUsed map[sessionKey]time.Time
	// generation bumps on Invalidate so a dial that started with the old profile
	// isn't cached after the profile changed.
	generation map[string]uint64

	// history records executed statements; nil disables it (tests).
	history *History
}

// SetHistory turns on recording of executed statements.
func (m *Manager) SetHistory(h *History) { m.history = h }

// History returns the query history (nil when disabled).
func (m *Manager) History() *History { return m.history }

// NewManager creates a manager backed by the given profile store.
func NewManager(store *Store) *Manager {
	return &Manager{
		store:    store,
		open:     openSession,
		now:      time.Now,
		sessions: map[sessionKey]session{},
		schemas:  map[sessionKey][]Table{},
		running:  map[string]context.CancelFunc{},
		active:   map[sessionKey]int{},
		lastUsed: map[sessionKey]time.Time{},

		generation: map[string]uint64{},
	}
}

// Store exposes the underlying profile store.
func (m *Manager) Store() *Store { return m.store }

// Query runs sql on one database of the connection ("" = the profile's default).
// queryID identifies the run so Cancel can abort it.
// The statement is recorded in the query history, failed or not.
func (m *Manager) Query(ctx context.Context, connID, database, queryID, sql string, limit int) (*QueryResult, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	start := time.Now()
	res, err := m.query(ctx, conn, database, queryID, sql, limit)
	var rows int64
	if res != nil {
		rows = res.Affected
		if len(res.Columns) > 0 {
			rows = int64(len(res.Rows))
		}
	}
	m.record(conn, database, HistoryQuery, sql, start, rows, err)
	return res, err
}

func (m *Manager) query(ctx context.Context, conn Connection, database, queryID, sql string, limit int) (*QueryResult, error) {
	// Postgres enforces read-only server-side; ClickHouse needs a client-side guard.
	if conn.ReadOnly && conn.Driver == ClickHouse && !isReadStatement(sql) {
		return nil, ErrReadOnly
	}
	limit = clampLimit(limit)

	sess, key, release, err := m.acquire(ctx, conn, database)
	if err != nil {
		return nil, err
	}
	defer release()
	if !isReadStatement(sql) {
		// e.g. DROP DATABASE: Flint's own idle connections to it would make the server refuse.
		// Closed synchronously: the statement must not race the pools' shutdown.
		m.evictIdle(func(k sessionKey, _ time.Duration) bool { return k.connID == conn.ID && k != key }, true)
	}

	ctx, done := m.track(ctx, queryID)
	defer done()
	res, err := sess.Query(ctx, sql, limit)
	if err != nil {
		return nil, cancelled(ctx, err)
	}
	return res, nil
}

// track makes a run cancellable by queryID ("" = not cancellable); done must be called.
func (m *Manager) track(ctx context.Context, queryID string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	if queryID == "" {
		return ctx, cancel
	}
	m.mu.Lock()
	m.running[queryID] = cancel
	m.mu.Unlock()
	return ctx, func() {
		m.mu.Lock()
		delete(m.running, queryID)
		m.mu.Unlock()
		cancel()
	}
}

// cancelled replaces the driver's error of a cancelled run with a plain one.
func cancelled(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return errors.New("query cancelled")
	}
	return err
}

// Cancel aborts a running query. Unknown ids are ignored (the query may have finished).
func (m *Manager) Cancel(queryID string) {
	m.mu.Lock()
	cancel := m.running[queryID]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Schema returns the (cached unless refresh) tables of one database of a connection.
func (m *Manager) Schema(ctx context.Context, connID, database string, refresh bool) ([]Table, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	key := keyFor(conn, database)
	m.mu.Lock()
	gen := m.generation[connID]
	m.mu.Unlock()
	if !refresh {
		m.mu.Lock()
		cached, ok := m.schemas[key]
		m.mu.Unlock()
		if ok {
			return cached, nil
		}
	}
	sess, _, release, err := m.acquire(ctx, conn, database)
	if err != nil {
		return nil, err
	}
	defer release()
	tables, err := sess.Schema(ctx)
	if err != nil {
		return nil, err
	}
	if tables == nil {
		tables = []Table{}
	}
	m.mu.Lock()
	// A profile change while introspecting may have re-pointed key's default database.
	if m.generation[connID] == gen {
		m.schemas[key] = tables
	}
	m.mu.Unlock()
	return tables, nil
}

// Databases lists the databases on the connection's server, read through its default database.
func (m *Manager) Databases(ctx context.Context, connID string) ([]DatabaseInfo, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	sess, _, release, err := m.acquire(ctx, conn, "")
	if err != nil {
		return nil, err
	}
	defer release()
	dbs, err := sess.Databases(ctx)
	if err != nil {
		return nil, err
	}
	if dbs == nil {
		dbs = []DatabaseInfo{}
	}
	return dbs, nil
}

// Test opens a throwaway session for an (unsaved) profile and pings it.
// A nil password falls back to the stored one for existing profiles.
func (m *Manager) Test(ctx context.Context, c Connection, password *string) error {
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return err
	}
	pw := ""
	if password != nil {
		pw = *password
	} else if c.ID != "" {
		stored, err := m.store.Password(c.ID)
		if err != nil {
			return err
		}
		pw = stored
	}
	sess, err := m.open(ctx, c, pw)
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Ping(ctx)
}

// Invalidate closes every session of the connection (one per database) and drops the
// cached schemas, e.g. after the profile changed.
// The pools close in the background: pgxpool.Close waits for running queries.
func (m *Manager) Invalidate(connID string) {
	m.mu.Lock()
	var closing []session
	for key, sess := range m.sessions {
		if key.connID == connID {
			closing = append(closing, sess)
			m.forget(key)
		}
	}
	for key := range m.schemas {
		if key.connID == connID {
			delete(m.schemas, key)
		}
	}
	m.generation[connID]++
	m.mu.Unlock()
	for _, sess := range closing {
		go sess.Close()
	}
}

// CloseAll closes every open session.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[sessionKey]session{}
	m.lastUsed = map[sessionKey]time.Time{}
	m.mu.Unlock()
	for _, s := range sessions {
		s.Close()
	}
}

// acquire returns the session for one database of conn ("" = default), dialing it on first
// use with the profile's host, user and password. The pool counts as busy (never evicted)
// until release is called; it also sweeps pools that have been idle too long.
func (m *Manager) acquire(ctx context.Context, conn Connection, database string) (session, sessionKey, func(), error) {
	m.evictIdle(m.expired, false)
	sess, key, err := m.session(ctx, conn, database)
	if err != nil {
		return nil, sessionKey{}, nil, err
	}
	release := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active[key] > 0 {
			m.active[key]--
		}
		if m.active[key] == 0 {
			delete(m.active, key)
		}
		if m.sessions[key] == sess {
			m.lastUsed[key] = m.now()
		}
	}
	return sess, key, release, nil
}

// expired matches pools of non-default databases unused for longer than idlePoolTTL.
// The default database's pool stays: it lists databases and serves most consoles.
func (m *Manager) expired(key sessionKey, idle time.Duration) bool {
	if idle <= idlePoolTTL {
		return false
	}
	conn, ok := m.store.Get(key.connID)
	return !ok || keyFor(conn, "") != key
}

// evictIdle closes the pools that match and have no call in flight; wait closes them before
// returning (fast, they're idle). Pools never released yet have no lastUsed and are skipped.
func (m *Manager) evictIdle(match func(key sessionKey, idle time.Duration) bool, wait bool) {
	now := m.now()
	var closing []session
	m.mu.Lock()
	for key, sess := range m.sessions {
		last, seen := m.lastUsed[key]
		if !seen || m.active[key] > 0 || !match(key, now.Sub(last)) {
			continue
		}
		closing = append(closing, sess)
		m.forget(key)
	}
	m.mu.Unlock()
	for _, sess := range closing {
		if wait {
			sess.Close()
		} else {
			go sess.Close()
		}
	}
}

// forget drops a pool from the bookkeeping (the cached schema stays valid); callers hold m.mu.
func (m *Manager) forget(key sessionKey) {
	delete(m.sessions, key)
	delete(m.active, key)
	delete(m.lastUsed, key)
}

// session returns the open session for one database of conn, dialing it on first use, and
// marks it active in the same critical section so a concurrent sweep can't close it.
func (m *Manager) session(ctx context.Context, conn Connection, database string) (session, sessionKey, error) {
	key := keyFor(conn, database)
	m.mu.Lock()
	sess, ok := m.sessions[key]
	gen := m.generation[conn.ID]
	if ok {
		m.active[key]++
	}
	m.mu.Unlock()
	if ok {
		return sess, key, nil
	}

	pw, err := m.store.Password(conn.ID)
	if err != nil {
		return nil, key, err
	}
	opened, err := m.open(ctx, conn.WithDatabase(database), pw)
	if err != nil {
		return nil, key, err
	}

	m.mu.Lock()
	if m.generation[conn.ID] != gen {
		m.mu.Unlock()
		opened.Close() // profile changed while dialing; dial again with the new one
		latest, ok := m.store.Get(conn.ID)
		if !ok {
			return nil, key, fmt.Errorf("unknown connection %q", conn.ID)
		}
		return m.session(ctx, latest, database)
	}
	defer m.mu.Unlock()
	m.active[key]++
	if existing, ok := m.sessions[key]; ok {
		opened.Close() // lost a race with a concurrent open
		return existing, key, nil
	}
	m.sessions[key] = opened
	return opened, key, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultRowLimit
	}
	return min(limit, maxRowLimit)
}
