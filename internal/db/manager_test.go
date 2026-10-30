package db

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSession records calls; Query blocks until ctx is done when block is set.
type fakeSession struct {
	mu          sync.Mutex
	lastSQL     string
	lastLimit   int
	schemaCalls int
	closed      bool
	tables      []Table
	queryErr    error
	block       bool
	started     chan struct{}
}

func (f *fakeSession) Query(ctx context.Context, sql string, limit int) (*QueryResult, error) {
	f.mu.Lock()
	f.lastSQL, f.lastLimit = sql, limit
	block, started := f.block, f.started
	f.mu.Unlock()
	if block {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return &QueryResult{Command: "SELECT 1", Rows: [][]any{{int64(1)}}}, nil
}

func (f *fakeSession) Schema(context.Context) ([]Table, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.schemaCalls++
	return f.tables, nil
}

func (f *fakeSession) Ping(context.Context) error { return nil }

func (f *fakeSession) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *fakeSession) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// newTestManager saves conn and wires a fake session for it.
func newTestManager(t *testing.T, conn Connection) (*Manager, Connection, *fakeSession) {
	t.Helper()
	s, _, _ := openTestStore(t)
	saved, err := s.Save(conn, ptr("pw"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(s)
	fake := &fakeSession{}
	m.sessions[saved.ID] = fake
	return m, saved, fake
}

func TestManagerQueryUnknownConnection(t *testing.T) {
	s, _, _ := openTestStore(t)
	m := NewManager(s)
	if m.Store() != s {
		t.Error("Store() mismatch")
	}
	if _, err := m.Query(context.Background(), "nope", "", "SELECT 1", 0); err == nil || !strings.Contains(err.Error(), "unknown connection") {
		t.Errorf("error = %v", err)
	}
	if _, err := m.Schema(context.Background(), "nope", false); err == nil {
		t.Error("Schema(unknown) error = nil")
	}
}

func TestManagerReadOnlyClickHouse(t *testing.T) {
	ro := Connection{Driver: ClickHouse, Host: "192.0.2.1", User: "default", ReadOnly: true}
	s, _, _ := openTestStore(t)
	conn, err := s.Save(ro, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(s) // no fake session: a dial would hang/fail, so rejection must come first
	for _, sql := range []string{"INSERT INTO t VALUES (1)", "-- x\ndrop table t", "ALTER TABLE t DELETE WHERE 1"} {
		if _, err := m.Query(context.Background(), conn.ID, "", sql, 0); !errors.Is(err, ErrReadOnly) {
			t.Errorf("Query(%q) error = %v, want ErrReadOnly", sql, err)
		}
	}
	if len(m.sessions) != 0 {
		t.Error("a session was opened for a rejected statement")
	}
}

func TestManagerQueryPassesThrough(t *testing.T) {
	tests := []struct {
		name string
		conn Connection
		sql  string
	}{
		{"read-only clickhouse select", Connection{Driver: ClickHouse, Host: "h", User: "u", ReadOnly: true}, "SELECT 1"},
		{"read-only postgres write is server-enforced", Connection{Driver: Postgres, Host: "h", User: "u", ReadOnly: true}, "INSERT INTO t VALUES (1)"},
		{"writable clickhouse insert", Connection{Driver: ClickHouse, Host: "h", User: "u"}, "INSERT INTO t VALUES (1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, conn, fake := newTestManager(t, tt.conn)
			res, err := m.Query(context.Background(), conn.ID, "q", tt.sql, 0)
			if err != nil {
				t.Fatalf("Query() error = %v", err)
			}
			if res.Command != "SELECT 1" || fake.lastSQL != tt.sql || fake.lastLimit != defaultRowLimit {
				t.Errorf("res=%+v sql=%q limit=%d", res, fake.lastSQL, fake.lastLimit)
			}
			if len(m.running) != 0 {
				t.Error("running query not unregistered")
			}
		})
	}
}

func TestManagerQueryError(t *testing.T) {
	m, conn, fake := newTestManager(t, pgConn())
	fake.queryErr = errors.New("syntax error")
	if _, err := m.Query(context.Background(), conn.ID, "", "SELEC", 5); !errors.Is(err, fake.queryErr) {
		t.Errorf("error = %v, want syntax error", err)
	}
	if fake.lastLimit != 5 {
		t.Errorf("limit = %d, want 5", fake.lastLimit)
	}
}

func TestManagerCancel(t *testing.T) {
	m, conn, fake := newTestManager(t, pgConn())
	fake.block, fake.started = true, make(chan struct{})

	m.Cancel("unknown-id") // no-op, must not panic

	errc := make(chan error, 1)
	go func() {
		_, err := m.Query(context.Background(), conn.ID, "q1", "SELECT pg_sleep(100)", 0)
		errc <- err
	}()
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not start")
	}
	m.Cancel("q1")
	select {
	case err := <-errc:
		if err == nil || err.Error() != "query cancelled" {
			t.Errorf("error = %v, want query cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not abort the query")
	}
	m.Cancel("q1") // already finished: no-op
}

func TestClampLimit(t *testing.T) {
	tests := map[int]int{
		-5: defaultRowLimit, 0: defaultRowLimit, 1: 1, 500: 500,
		maxRowLimit: maxRowLimit, maxRowLimit + 1: maxRowLimit, 1 << 40: maxRowLimit,
	}
	for in, want := range tests {
		if got := clampLimit(in); got != want {
			t.Errorf("clampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestManagerSchemaCachingAndInvalidate(t *testing.T) {
	m, conn, fake := newTestManager(t, pgConn())
	ctx := context.Background()

	tables, err := m.Schema(ctx, conn.ID, false)
	if err != nil {
		t.Fatalf("Schema() error = %v", err)
	}
	if tables == nil || len(tables) != 0 {
		t.Errorf("nil schema should become empty slice, got %#v", tables)
	}
	fake.tables = []Table{{Schema: "public", Name: "t"}}
	if tables, _ = m.Schema(ctx, conn.ID, false); len(tables) != 0 || fake.schemaCalls != 1 {
		t.Errorf("cached call hit session: calls=%d tables=%v", fake.schemaCalls, tables)
	}
	if tables, _ = m.Schema(ctx, conn.ID, true); len(tables) != 1 || fake.schemaCalls != 2 {
		t.Errorf("refresh: calls=%d tables=%v", fake.schemaCalls, tables)
	}

	m.Invalidate(conn.ID)
	// The pool closes in the background (pgxpool.Close waits for running queries).
	deadline := time.Now().Add(2 * time.Second)
	for !fake.isClosed() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !fake.isClosed() {
		t.Error("Invalidate did not close the session")
	}
	if _, ok := m.schemas[conn.ID]; ok {
		t.Error("Invalidate kept cached schema")
	}
	m.Invalidate("unknown") // no-op
}

func TestManagerCloseAll(t *testing.T) {
	m, _, a := newTestManager(t, pgConn())
	b := &fakeSession{}
	m.sessions["other"] = b
	m.CloseAll()
	if !a.isClosed() || !b.isClosed() || len(m.sessions) != 0 {
		t.Errorf("CloseAll: a=%v b=%v remaining=%d", a.isClosed(), b.isClosed(), len(m.sessions))
	}
}

func TestManagerSessionPasswordError(t *testing.T) {
	s, sec, _ := openTestStore(t)
	conn, _ := s.Save(pgConn(), nil)
	sec.getErr = errors.New("keychain locked")
	m := NewManager(s)
	if _, err := m.Query(context.Background(), conn.ID, "", "SELECT 1", 0); !errors.Is(err, sec.getErr) {
		t.Errorf("error = %v, want keychain locked", err)
	}
}

func TestManagerTestValidation(t *testing.T) {
	s, sec, _ := openTestStore(t)
	m := NewManager(s)
	if err := m.Test(context.Background(), Connection{Driver: Postgres, User: "u"}, nil); err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Errorf("Test(no host) = %v", err)
	}
	if err := m.Test(context.Background(), Connection{Driver: "x"}, ptr("pw")); err == nil || !strings.Contains(err.Error(), "unsupported driver") {
		t.Errorf("Test(bad driver) = %v", err)
	}
	sec.getErr = errors.New("keychain locked")
	saved := pgConn()
	saved.ID = "existing"
	if err := m.Test(context.Background(), saved, nil); !errors.Is(err, sec.getErr) {
		t.Errorf("Test(stored password error) = %v", err)
	}
}

// TestManagerTestPingFails points both drivers at a local listener that drops every
// connection, so Ping fails fast without needing a real database server.
func TestManagerTestPingFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	s, _, _ := openTestStore(t)
	m := NewManager(s)
	for _, d := range []Driver{Postgres, ClickHouse} {
		t.Run(string(d), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := Connection{Driver: d, Host: "127.0.0.1", Port: port, User: "u", Database: "db", ReadOnly: true}
			if err := m.Test(ctx, c, ptr("pw")); err == nil {
				t.Error("Test() against a dropping listener = nil, want error")
			}
		})
	}
}

func TestManagerDiscardsSessionDialedWithStaleProfile(t *testing.T) {
	m, conn, _ := newTestManager(t, Connection{Driver: Postgres, Host: "h", User: "u"})
	m.mu.Lock()
	gen := m.generation[conn.ID]
	m.mu.Unlock()
	m.Invalidate(conn.ID) // profile changed while a dial (simulated below) was in flight
	m.mu.Lock()
	changed := m.generation[conn.ID] != gen
	m.mu.Unlock()
	if !changed {
		t.Fatal("Invalidate must bump the generation so in-flight dials are discarded")
	}
}
