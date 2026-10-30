package db

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// dialRecorder replaces Manager.open: every dial gets its own fake session and is recorded.
type dialRecorder struct {
	mu        sync.Mutex
	dials     []Connection
	passwords []string
	sessions  map[string]*fakeSession // by database
	err       error
}

func recordDials(m *Manager) *dialRecorder {
	r := &dialRecorder{sessions: map[string]*fakeSession{}}
	m.open = func(_ context.Context, c Connection, password string) (session, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.err != nil {
			return nil, r.err
		}
		r.dials = append(r.dials, c)
		r.passwords = append(r.passwords, password)
		f := &fakeSession{}
		r.sessions[c.Database] = f
		return f, nil
	}
	return r
}

func (r *dialRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.dials)
}

func TestManagerDatabases(t *testing.T) {
	m, conn, fake := newTestManager(t, pgConn())
	ctx := context.Background()

	dbs, err := m.Databases(ctx, conn.ID)
	if err != nil {
		t.Fatalf("Databases() error = %v", err)
	}
	if dbs == nil || len(dbs) != 0 {
		t.Errorf("nil list should become an empty slice, got %#v", dbs)
	}

	fake.databases = []DatabaseInfo{{Name: "admindb", CanConnect: true}, {Name: "rdsadmin", CanConnect: false}}
	dbs, err = m.Databases(ctx, conn.ID)
	if err != nil || len(dbs) != 2 || dbs[1].Name != "rdsadmin" || dbs[1].CanConnect {
		t.Errorf("Databases() = %+v, %v", dbs, err)
	}

	if _, err := m.Databases(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "unknown connection") {
		t.Errorf("Databases(unknown) error = %v", err)
	}
}

func TestManagerDatabasesWithoutProfileDatabaseUsesPostgres(t *testing.T) {
	s, _, _ := openTestStore(t)
	conn, err := s.Save(Connection{Driver: Postgres, Host: "h", User: "u"}, ptr("pw"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(s)
	dials := recordDials(m)
	if _, err := m.Databases(context.Background(), conn.ID); err != nil {
		t.Fatalf("Databases() error = %v", err)
	}
	if dials.count() != 1 || dials.dials[0].DefaultDatabase() != "postgres" {
		t.Errorf("dials = %+v, want one to the postgres database", dials.dials)
	}
}

func TestManagerDatabasesDialError(t *testing.T) {
	s, _, _ := openTestStore(t)
	conn, _ := s.Save(pgConn(), ptr("pw"))
	m := NewManager(s)
	dials := recordDials(m)
	dials.err = errors.New("connection refused")
	if _, err := m.Databases(context.Background(), conn.ID); !errors.Is(err, dials.err) {
		t.Errorf("Databases() error = %v, want connection refused", err)
	}
}

func TestManagerQueryRoutesToDatabase(t *testing.T) {
	m, conn, def := newTestManager(t, pgConn()) // profile database "app"
	dials := recordDials(m)
	ctx := context.Background()

	// "" and the profile's own database both hit the default pool.
	for _, database := range []string{"", "app"} {
		if _, err := m.Query(ctx, conn.ID, database, "", "SELECT 1", 0); err != nil {
			t.Fatalf("Query(%q) error = %v", database, err)
		}
	}
	if dials.count() != 0 || def.lastSQL != "SELECT 1" {
		t.Fatalf("default database dialed again: dials=%d", dials.count())
	}

	// Another database opens its own pool once, with the same host, user and password.
	for range 2 {
		if _, err := m.Query(ctx, conn.ID, "billing", "", "SELECT 2", 0); err != nil {
			t.Fatalf("Query(billing) error = %v", err)
		}
	}
	if dials.count() != 1 {
		t.Fatalf("dials = %d, want 1 (pool reused)", dials.count())
	}
	got := dials.dials[0]
	if got.Database != "billing" || got.Host != conn.Host || got.User != conn.User || got.Port != conn.Port || dials.passwords[0] != "pw" {
		t.Errorf("dialed %+v with %q, want billing on the profile's server with its password", got, dials.passwords[0])
	}
	if dials.sessions["billing"].lastSQL != "SELECT 2" || def.lastSQL != "SELECT 1" {
		t.Error("query went to the wrong pool")
	}
}

func TestManagerSchemaPerDatabase(t *testing.T) {
	m, conn, def := newTestManager(t, pgConn())
	dials := recordDials(m)
	ctx := context.Background()
	def.tables = []Table{{Schema: "public", Name: "users"}}

	if tables, err := m.Schema(ctx, conn.ID, "", false); err != nil || len(tables) != 1 {
		t.Fatalf("Schema(default) = %v, %v", tables, err)
	}
	other, err := m.Schema(ctx, conn.ID, "billing", false)
	if err != nil || len(other) != 0 {
		t.Fatalf("Schema(billing) = %v, %v", other, err)
	}
	dials.sessions["billing"].tables = []Table{{Schema: "public", Name: "invoices"}}
	if cached, _ := m.Schema(ctx, conn.ID, "billing", false); len(cached) != 0 {
		t.Errorf("billing schema not cached: %v", cached)
	}
	if fresh, _ := m.Schema(ctx, conn.ID, "billing", true); len(fresh) != 1 || fresh[0].Name != "invoices" {
		t.Errorf("billing refresh = %v", fresh)
	}
	if def.schemaCalls != 1 {
		t.Errorf("default schema calls = %d, want 1 (billing refresh must not touch it)", def.schemaCalls)
	}
}

func TestManagerInvalidateClosesEveryDatabase(t *testing.T) {
	m, conn, def := newTestManager(t, pgConn())
	dials := recordDials(m)
	ctx := context.Background()
	if _, err := m.Schema(ctx, conn.ID, "billing", false); err != nil {
		t.Fatal(err)
	}
	other := &fakeSession{}
	otherKey := sessionKey{connID: "other", database: "app"}
	m.sessions[otherKey] = other

	m.Invalidate(conn.ID)
	billing := dials.sessions["billing"]
	deadline := time.Now().Add(2 * time.Second)
	for !(def.isClosed() && billing.isClosed()) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !def.isClosed() || !billing.isClosed() {
		t.Errorf("closed: default=%v billing=%v", def.isClosed(), billing.isClosed())
	}
	if other.isClosed() || m.sessions[otherKey] == nil {
		t.Error("Invalidate closed another connection's session")
	}
	if len(m.schemas) != 0 {
		t.Errorf("cached schemas left: %v", m.schemas)
	}
}

func TestKeyForResolvesDefault(t *testing.T) {
	pg := Connection{ID: "c", Driver: Postgres}
	if keyFor(pg, "") != keyFor(pg, "postgres") {
		t.Error(`"" and "postgres" must share a pool for a profile without a database`)
	}
	if k := keyFor(Connection{ID: "c", Driver: ClickHouse}, ""); k.database != "" {
		t.Errorf("clickhouse without database must use the server default, key = %+v", k)
	}
	ch := Connection{ID: "c", Driver: ClickHouse, Database: "logs"}
	if keyFor(ch, "") != (sessionKey{connID: "c", database: "logs"}) || keyFor(ch, "system").database != "system" {
		t.Errorf("clickhouse keys = %+v / %+v", keyFor(ch, ""), keyFor(ch, "system"))
	}
}

func TestClickHouseProtocol(t *testing.T) {
	if clickhouseProtocol(ProtocolHTTP).String() != "http" || clickhouseProtocol("").String() != "native" {
		t.Errorf("protocols = %s / %s", clickhouseProtocol(ProtocolHTTP), clickhouseProtocol(""))
	}
}

// fakeClock drives Manager.now so idle eviction is tested without sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func useFakeClock(m *Manager) *fakeClock {
	c := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m.now = func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.t
	}
	return c
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func waitClosed(t *testing.T, f *fakeSession) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !f.isClosed() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !f.isClosed() {
		t.Error("evicted session was not closed")
	}
}

func hasSession(m *Manager, conn Connection, database string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[keyFor(conn, database)]
	return ok
}

func TestManagerEvictsIdleNonDefaultPools(t *testing.T) {
	m, conn, def := newTestManager(t, pgConn())
	dials := recordDials(m)
	clock := useFakeClock(m)
	ctx := context.Background()

	if _, err := m.Query(ctx, conn.ID, "billing", "", "SELECT 1", 0); err != nil {
		t.Fatal(err)
	}
	billing := dials.sessions["billing"]

	clock.advance(idlePoolTTL / 2)
	if _, err := m.Query(ctx, conn.ID, "", "", "SELECT 1", 0); err != nil {
		t.Fatal(err)
	}
	if !hasSession(m, conn, "billing") || billing.isClosed() {
		t.Fatal("pool evicted before its idle timeout")
	}

	clock.advance(idlePoolTTL)
	if _, err := m.Query(ctx, conn.ID, "", "", "SELECT 1", 0); err != nil {
		t.Fatal(err)
	}
	if hasSession(m, conn, "billing") {
		t.Error("idle billing pool still cached")
	}
	waitClosed(t, billing)
	if def.isClosed() || !hasSession(m, conn, "") {
		t.Error("the default database's pool must stay open however idle")
	}

	// The evicted database reopens transparently on next use.
	if _, err := m.Query(ctx, conn.ID, "billing", "", "SELECT 1", 0); err != nil {
		t.Fatal(err)
	}
	if dials.count() != 2 {
		t.Errorf("dials = %d, want a fresh dial after eviction", dials.count())
	}
}

func TestManagerKeepsBusyPools(t *testing.T) {
	m, conn, _ := newTestManager(t, pgConn())
	clock := useFakeClock(m)
	slow := &fakeSession{block: true, started: make(chan struct{})}
	m.sessions[keyFor(conn, "billing")] = slow

	errc := make(chan error, 1)
	go func() {
		_, err := m.Query(context.Background(), conn.ID, "billing", "slow", "SELECT pg_sleep(600)", 0)
		errc <- err
	}()
	select {
	case <-slow.started:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not start")
	}

	clock.advance(10 * idlePoolTTL)
	if _, err := m.Query(context.Background(), conn.ID, "", "", "DROP DATABASE other", 0); err != nil {
		t.Fatal(err)
	}
	if !hasSession(m, conn, "billing") || slow.isClosed() {
		t.Error("a pool with a running query was evicted")
	}
	m.Cancel("slow")
	<-errc
}

func TestManagerWriteEvictsIdleSiblingPools(t *testing.T) {
	m, conn, _ := newTestManager(t, pgConn())
	dials := recordDials(m)
	useFakeClock(m)
	ctx := context.Background()
	other := &fakeSession{}
	otherKey := sessionKey{connID: "other", database: "billing"}
	m.sessions[otherKey] = other

	if _, err := m.Query(ctx, conn.ID, "billing", "", "SELECT 1", 0); err != nil {
		t.Fatal(err)
	}
	billing := dials.sessions["billing"]

	// Reads never evict fresh pools.
	if _, err := m.Query(ctx, conn.ID, "", "", "SELECT 1", 0); err != nil {
		t.Fatal(err)
	}
	if !hasSession(m, conn, "billing") {
		t.Fatal("a read evicted a fresh sibling pool")
	}

	// A write (e.g. DROP DATABASE billing) must not trip over Flint's own idle connections.
	if _, err := m.Query(ctx, conn.ID, "", "", "DROP DATABASE billing", 0); err != nil {
		t.Fatal(err)
	}
	if hasSession(m, conn, "billing") {
		t.Error("idle sibling pool kept open across a write")
	}
	waitClosed(t, billing)
	if other.isClosed() {
		t.Error("another connection's pool was closed")
	}
}
