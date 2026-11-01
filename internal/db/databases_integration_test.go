//go:build integration

package db

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func listDatabases(t *testing.T, m *Manager, connID string) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dbs, err := m.Databases(ctx, connID)
	if err != nil {
		t.Fatalf("Databases() error = %v", err)
	}
	canConnect := map[string]bool{}
	for _, d := range dbs {
		canConnect[d.Name] = d.CanConnect
	}
	return canConnect
}

// defaultDatabaseOf returns the database Databases() flags as the connection's default.
func defaultDatabaseOf(t *testing.T, m *Manager, connID string) string {
	t.Helper()
	dbs, err := m.Databases(context.Background(), connID)
	if err != nil {
		t.Fatalf("Databases() error = %v", err)
	}
	for _, d := range dbs {
		if d.Default {
			return d.Name
		}
	}
	return ""
}

func queryIn(t *testing.T, m *Manager, connID, database, sql string) *QueryResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := m.Query(ctx, connID, database, "", sql, 100)
	if err != nil {
		t.Fatalf("[%s] %s: %v", database, sql, err)
	}
	return res
}

func hasTable(tables []Table, name string) bool {
	return slices.ContainsFunc(tables, func(tb Table) bool { return tb.Name == name })
}

// TestPostgresDatabasesIntegration builds a small cluster (two databases, one locked away
// from a limited role) and browses it the way the Databases tree and consoles do.
func TestPostgresDatabasesIntegration(t *testing.T) {
	// No database in the profile: Flint must fall back to "postgres" and list the cluster.
	m, admin := integrationManager(t, "FLINT_TEST_PG", Connection{Driver: Postgres, User: "postgres"})
	setup := []string{
		"DROP DATABASE IF EXISTS flint_it_a WITH (FORCE)",
		"DROP DATABASE IF EXISTS flint_it_b WITH (FORCE)",
		"DROP DATABASE IF EXISTS flint_it_locked WITH (FORCE)",
		"DROP ROLE IF EXISTS flint_it_user",
		"CREATE DATABASE flint_it_a",
		"CREATE DATABASE flint_it_b",
		"CREATE DATABASE flint_it_locked",
		"REVOKE CONNECT ON DATABASE flint_it_locked FROM PUBLIC",
		"CREATE ROLE flint_it_user LOGIN PASSWORD '" + testPassword + "'",
	}
	for _, sql := range setup {
		queryIn(t, m, admin.ID, "", sql)
	}
	t.Cleanup(func() {
		m.CloseAll()
		for _, sql := range []string{
			"DROP DATABASE IF EXISTS flint_it_a WITH (FORCE)",
			"DROP DATABASE IF EXISTS flint_it_b WITH (FORCE)",
			"DROP DATABASE IF EXISTS flint_it_locked WITH (FORCE)",
			"DROP ROLE IF EXISTS flint_it_user",
		} {
			queryIn(t, m, admin.ID, "", sql)
		}
	})

	t.Run("superuser lists every database except templates", func(t *testing.T) {
		dbs := listDatabases(t, m, admin.ID)
		for _, name := range []string{"postgres", "flint_it_a", "flint_it_b", "flint_it_locked"} {
			if !dbs[name] {
				t.Errorf("%s missing or unavailable in %v", name, dbs)
			}
		}
		if got := defaultDatabaseOf(t, m, admin.ID); got != "postgres" {
			t.Errorf("default database = %q, want postgres", got)
		}
		if _, ok := dbs["template0"]; ok {
			t.Error("templates must be hidden")
		}
	})

	t.Run("queries and schema go to the chosen database", func(t *testing.T) {
		queryIn(t, m, admin.ID, "flint_it_a", "CREATE TABLE only_in_a (id int)")
		res := queryIn(t, m, admin.ID, "flint_it_a", "SELECT current_database()")
		if got := res.Rows[0][0]; got != "flint_it_a" {
			t.Errorf("current_database() = %v, want flint_it_a", got)
		}
		if got := queryIn(t, m, admin.ID, "", "SELECT current_database()").Rows[0][0]; got != "postgres" {
			t.Errorf("default database = %v, want postgres", got)
		}

		ctx := context.Background()
		inA, err := m.Schema(ctx, admin.ID, "flint_it_a", true)
		if err != nil || !hasTable(inA, "only_in_a") {
			t.Errorf("Schema(flint_it_a) = %+v, %v", inA, err)
		}
		inB, err := m.Schema(ctx, admin.ID, "flint_it_b", true)
		if err != nil || hasTable(inB, "only_in_a") {
			t.Errorf("Schema(flint_it_b) = %+v, %v (table leaked from flint_it_a)", inB, err)
		}
	})

	t.Run("dropping a database Flint has a pool to", func(t *testing.T) {
		queryIn(t, m, admin.ID, "flint_it_b", "SELECT 1") // leaves an idle pooled connection
		// Without evicting that pool Postgres refuses: "database is being accessed by other users".
		queryIn(t, m, admin.ID, "", "DROP DATABASE flint_it_b")
		if _, ok := listDatabases(t, m, admin.ID)["flint_it_b"]; ok {
			t.Error("flint_it_b still listed after DROP DATABASE")
		}
	})

	t.Run("limited role sees databases without CONNECT as unavailable", func(t *testing.T) {
		limited := admin
		limited.ID, limited.Name, limited.User = "", "limited", "flint_it_user"
		saved, err := m.Store().Save(limited, ptr(testPassword))
		if err != nil {
			t.Fatal(err)
		}
		dbs := listDatabases(t, m, saved.ID)
		if !dbs["flint_it_a"] || !dbs["postgres"] {
			t.Errorf("accessible databases marked unavailable: %v", dbs)
		}
		if can, ok := dbs["flint_it_locked"]; !ok || can {
			t.Errorf("flint_it_locked: listed=%v canConnect=%v, want listed and unavailable", ok, can)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err = m.Query(ctx, saved.ID, "flint_it_locked", "", "SELECT 1", 10)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("query on a locked database: err = %v, want permission denied", err)
		}
	})
}

func clickHouseDatabasesSuite(t *testing.T, env string) {
	m, conn := integrationManager(t, env, Connection{
		Driver: ClickHouse, User: "flint", // no database: falls back to "default"
	})
	queryIn(t, m, conn.ID, "", "CREATE DATABASE IF NOT EXISTS flint_it_db")
	t.Cleanup(func() { queryIn(t, m, conn.ID, "", "DROP DATABASE IF EXISTS flint_it_db") })
	queryIn(t, m, conn.ID, "flint_it_db", "CREATE TABLE IF NOT EXISTS only_here (id UInt32) ENGINE = Memory")

	dbs := listDatabases(t, m, conn.ID)
	for _, name := range []string{"default", "system", "flint_it_db"} {
		if !dbs[name] {
			t.Errorf("%s missing or unavailable in %v", name, dbs)
		}
	}

	if got := defaultDatabaseOf(t, m, conn.ID); got != "default" {
		t.Errorf("default database = %q, want the user's default_database", got)
	}
	if got := queryIn(t, m, conn.ID, "flint_it_db", "SELECT currentDatabase()").Rows[0][0]; got != "flint_it_db" {
		t.Errorf("currentDatabase() = %v, want flint_it_db", got)
	}
	if got := queryIn(t, m, conn.ID, "", "SELECT currentDatabase()").Rows[0][0]; got != "default" {
		t.Errorf("default database = %v, want default", got)
	}

	ctx := context.Background()
	here, err := m.Schema(ctx, conn.ID, "flint_it_db", true)
	if err != nil || !hasTable(here, "only_here") {
		t.Errorf("Schema(flint_it_db) = %+v, %v", here, err)
	}
	def, err := m.Schema(ctx, conn.ID, "", true)
	if err != nil || hasTable(def, "only_here") {
		t.Errorf("Schema(default) = %+v, %v (other database leaked in)", def, err)
	}
}

func TestClickHouseDatabasesIntegration(t *testing.T) {
	clickHouseDatabasesSuite(t, "FLINT_TEST_CH_HTTP")
}

func TestClickHouseHTTPIntegration(t *testing.T) {
	t.Run("types, limit and read-only guard", func(t *testing.T) {
		m, conn := integrationManager(t, "FLINT_TEST_CH_HTTP", Connection{
			Driver: ClickHouse, User: "flint", ReadOnly: true,
		})
		if err := m.Test(context.Background(), conn, nil); err != nil {
			t.Fatalf("Test() over HTTP = %v", err)
		}
		res := queryIn(t, m, conn.ID, "", "SELECT toUInt64(42) AS n, 'zażółć' AS pl")
		if res.Rows[0][0] != "42" && res.Rows[0][0] != uint64(42) && res.Rows[0][0] != int64(42) {
			t.Errorf("n = %#v", res.Rows[0][0])
		}
		if res.Rows[0][1] != "zażółć" {
			t.Errorf("pl = %#v", res.Rows[0][1])
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		big, err := m.Query(ctx, conn.ID, "", "", "SELECT number FROM system.numbers LIMIT 100000", 10)
		if err != nil || len(big.Rows) != 10 || !big.Truncated {
			t.Errorf("limit over HTTP: rows=%d truncated=%v err=%v", len(big.Rows), big != nil && big.Truncated, err)
		}
		if _, err := m.Query(ctx, conn.ID, "", "", "CREATE TABLE x (id UInt8) ENGINE = Memory", 10); err != ErrReadOnly {
			t.Errorf("read-only over HTTP: err = %v", err)
		}
	})
}
