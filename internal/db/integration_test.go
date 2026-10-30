//go:build integration

// Integration tests against real servers. Start them with `make test-db-up`, then:
//
//	FLINT_TEST_PG=localhost:55432 FLINT_TEST_CH=localhost:59000 go test -tags integration ./internal/db/
package db

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testPassword = "flint-test"

func integrationManager(t *testing.T, env string, c Connection) (*Manager, Connection) {
	t.Helper()
	addr := os.Getenv(env)
	if addr == "" {
		t.Skipf("%s not set", env)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	c.Host = host
	c.Port, _ = strconv.Atoi(port)
	store, err := OpenStore(t.TempDir(), newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(c, ptr(testPassword))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(store)
	t.Cleanup(m.CloseAll)
	return m, saved
}

func query(t *testing.T, m *Manager, id, sql string, limit int) *QueryResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := m.Query(ctx, id, "", sql, limit)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return res
}

func TestPostgresIntegration(t *testing.T) {
	m, conn := integrationManager(t, "FLINT_TEST_PG", Connection{
		Driver: Postgres, Database: "postgres", User: "postgres",
	})

	t.Run("types", func(t *testing.T) {
		res := query(t, m, conn.ID, `SELECT 9007199254740993::int8 AS big, 1.50::numeric AS num,
			'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid AS id, '{"k": [1, 2]}'::jsonb AS doc,
			'2024-01-02 03:04:05+00'::timestamptz AS ts, ARRAY[1,2] AS arr, NULL::text AS nothing,
			'zażółć'::text AS pl`, 10)
		row := res.Rows[0]
		want := map[int]any{0: "9007199254740993", 1: "1.50", 2: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", 6: nil, 7: "zażółć"}
		for i, w := range want {
			if row[i] != w {
				t.Errorf("column %s = %#v, want %#v", res.Columns[i].Name, row[i], w)
			}
		}
		if res.Columns[0].Type != "int8" || res.Columns[2].Type != "uuid" {
			t.Errorf("column types = %+v", res.Columns)
		}
		if doc, ok := row[3].(map[string]any); !ok || doc["k"] == nil {
			t.Errorf("jsonb = %#v", row[3])
		}
		if !strings.HasPrefix(row[4].(string), "2024-01-02") {
			t.Errorf("timestamptz = %#v", row[4])
		}
	})

	t.Run("limit truncates", func(t *testing.T) {
		res := query(t, m, conn.ID, "SELECT g FROM generate_series(1, 100000) g", 50)
		if len(res.Rows) != 50 || !res.Truncated {
			t.Errorf("rows = %d truncated = %v", len(res.Rows), res.Truncated)
		}
	})

	t.Run("dml and schema", func(t *testing.T) {
		query(t, m, conn.ID, "CREATE TABLE IF NOT EXISTS flint_it (id int primary key, name text)", 10)
		t.Cleanup(func() { query(t, m, conn.ID, "DROP TABLE IF EXISTS flint_it", 10) })
		res := query(t, m, conn.ID, "INSERT INTO flint_it VALUES (1, 'a'), (2, 'b') ON CONFLICT DO NOTHING", 10)
		if res.Affected != 2 {
			t.Errorf("affected = %d (%s)", res.Affected, res.Command)
		}
		tables, err := m.Schema(context.Background(), conn.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, tb := range tables {
			if tb.Name == "flint_it" && tb.Schema == "public" && len(tb.Columns) == 2 {
				found = true
			}
		}
		if !found {
			t.Errorf("flint_it not in schema: %+v", tables)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		done := make(chan error, 1)
		go func() {
			_, err := m.Query(context.Background(), conn.ID, "slow", "SELECT pg_sleep(30)", 10)
			done <- err
		}()
		time.Sleep(300 * time.Millisecond)
		m.Cancel("slow")
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "cancel") {
				t.Errorf("err = %v, want cancellation", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("query was not cancelled")
		}
	})

	t.Run("read-only", func(t *testing.T) {
		ro := conn
		ro.ID = ""
		ro.ReadOnly = true
		saved, err := m.Store().Save(ro, ptr(testPassword))
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.Query(context.Background(), saved.ID, "", "CREATE TABLE flint_ro (id int)", 10)
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("err = %v, want read-only transaction error", err)
		}
	})
}

func TestClickHouseIntegration(t *testing.T) {
	m, conn := integrationManager(t, "FLINT_TEST_CH", Connection{
		Driver: ClickHouse, Database: "default", User: "flint",
	})

	t.Run("types", func(t *testing.T) {
		res := query(t, m, conn.ID, `SELECT toUInt64(18446744073709551615) AS big,
			toDecimal64(1.5, 2) AS dec, toUUID('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11') AS id,
			toDateTime('2024-01-02 03:04:05', 'UTC') AS ts, CAST(NULL AS Nullable(DateTime)) AS nts,
			[1, 2] AS arr, map('a', 1) AS m, 'zażółć' AS pl`, 10)
		row := res.Rows[0]
		want := map[int]any{0: "18446744073709551615", 1: "1.5", 2: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", 4: nil, 7: "zażółć"}
		for i, w := range want {
			if row[i] != w {
				t.Errorf("column %s = %#v, want %#v", res.Columns[i].Name, row[i], w)
			}
		}
		if ts, _ := row[3].(string); !strings.HasPrefix(ts, "2024-01-02 03:04:05") {
			t.Errorf("DateTime = %#v", row[3])
		}
		if res.Columns[4].Type != "Nullable(DateTime)" {
			t.Errorf("type = %s", res.Columns[4].Type)
		}
	})

	t.Run("limit truncates", func(t *testing.T) {
		res := query(t, m, conn.ID, "SELECT number FROM system.numbers LIMIT 10000000", 100)
		if len(res.Rows) != 100 || !res.Truncated {
			t.Errorf("rows = %d truncated = %v", len(res.Rows), res.Truncated)
		}
	})

	t.Run("ddl and schema", func(t *testing.T) {
		query(t, m, conn.ID, "CREATE TABLE IF NOT EXISTS flint_it (id UInt32, name String) ENGINE = Memory", 10)
		t.Cleanup(func() { query(t, m, conn.ID, "DROP TABLE IF EXISTS flint_it", 10) })
		query(t, m, conn.ID, "INSERT INTO flint_it VALUES (1, 'a')", 10)
		tables, err := m.Schema(context.Background(), conn.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, tb := range tables {
			if tb.Name == "flint_it" && tb.Schema == "default" && len(tb.Columns) == 2 {
				found = true
			}
		}
		if !found {
			t.Errorf("flint_it not in schema: %+v", tables)
		}
	})
}
