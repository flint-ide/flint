package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	defaultRowLimit = 1000
	maxRowLimit     = 200_000
)

// ErrReadOnly is returned when a write statement targets a read-only connection.
var ErrReadOnly = errors.New("connection is read-only: only SELECT-like statements are allowed")

// Manager lazily opens sessions, runs queries and caches schemas per connection.
type Manager struct {
	store *Store

	mu       sync.Mutex
	sessions map[string]session
	schemas  map[string][]Table
	running  map[string]context.CancelFunc
	// generation bumps on Invalidate so a dial that started with the old profile
	// isn't cached after the profile changed.
	generation map[string]uint64
}

// NewManager creates a manager backed by the given profile store.
func NewManager(store *Store) *Manager {
	return &Manager{
		store:    store,
		sessions: map[string]session{},
		schemas:  map[string][]Table{},
		running:  map[string]context.CancelFunc{},

		generation: map[string]uint64{},
	}
}

// Store exposes the underlying profile store.
func (m *Manager) Store() *Store { return m.store }

// Query runs sql on the connection. queryID identifies the run so Cancel can abort it.
func (m *Manager) Query(ctx context.Context, connID, queryID, sql string, limit int) (*QueryResult, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	// Postgres enforces read-only server-side; ClickHouse needs a client-side guard.
	if conn.ReadOnly && conn.Driver == ClickHouse && !isReadStatement(sql) {
		return nil, ErrReadOnly
	}
	limit = clampLimit(limit)

	sess, err := m.session(ctx, conn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if queryID != "" {
		m.mu.Lock()
		m.running[queryID] = cancel
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			delete(m.running, queryID)
			m.mu.Unlock()
		}()
	}

	res, err := sess.Query(ctx, sql, limit)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, errors.New("query cancelled")
		}
		return nil, err
	}
	return res, nil
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

// Schema returns the (cached unless refresh) tables of a connection.
func (m *Manager) Schema(ctx context.Context, connID string, refresh bool) ([]Table, error) {
	if !refresh {
		m.mu.Lock()
		cached, ok := m.schemas[connID]
		m.mu.Unlock()
		if ok {
			return cached, nil
		}
	}
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	sess, err := m.session(ctx, conn)
	if err != nil {
		return nil, err
	}
	tables, err := sess.Schema(ctx)
	if err != nil {
		return nil, err
	}
	if tables == nil {
		tables = []Table{}
	}
	m.mu.Lock()
	m.schemas[connID] = tables
	m.mu.Unlock()
	return tables, nil
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
	sess, err := openSession(ctx, c, pw)
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Ping(ctx)
}

// Invalidate closes the session and drops the cached schema, e.g. after the profile changed.
// The pool closes in the background: pgxpool.Close waits for running queries.
func (m *Manager) Invalidate(connID string) {
	m.mu.Lock()
	sess := m.sessions[connID]
	delete(m.sessions, connID)
	delete(m.schemas, connID)
	m.generation[connID]++
	m.mu.Unlock()
	if sess != nil {
		go sess.Close()
	}
}

// CloseAll closes every open session.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]session{}
	m.mu.Unlock()
	for _, s := range sessions {
		s.Close()
	}
}

// session returns the open session for conn, dialing it on first use.
func (m *Manager) session(ctx context.Context, conn Connection) (session, error) {
	m.mu.Lock()
	sess, ok := m.sessions[conn.ID]
	gen := m.generation[conn.ID]
	m.mu.Unlock()
	if ok {
		return sess, nil
	}

	pw, err := m.store.Password(conn.ID)
	if err != nil {
		return nil, err
	}
	opened, err := openSession(ctx, conn, pw)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if m.generation[conn.ID] != gen {
		m.mu.Unlock()
		opened.Close() // profile changed while dialing; dial again with the new one
		latest, ok := m.store.Get(conn.ID)
		if !ok {
			return nil, fmt.Errorf("unknown connection %q", conn.ID)
		}
		return m.session(ctx, latest)
	}
	defer m.mu.Unlock()
	if existing, ok := m.sessions[conn.ID]; ok {
		opened.Close() // lost a race with a concurrent open
		return existing, nil
	}
	m.sessions[conn.ID] = opened
	return opened, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultRowLimit
	}
	return min(limit, maxRowLimit)
}
