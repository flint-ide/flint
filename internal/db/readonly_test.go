package db

import (
	"context"
	"strings"
	"testing"
)

func TestCheckReadStatement(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1", "select * from t where a = 'x'", "  -- why\nWITH x AS (SELECT 1) SELECT * FROM x;\n", "SHOW TABLES", "EXPLAIN SELECT 1",
		"(SELECT 1) UNION (SELECT 2)", "DESCRIBE t", "VALUES (1)", "SELECT 1 ; ;",
	} {
		if err := CheckReadStatement(sql); err != nil {
			t.Errorf("CheckReadStatement(%q) = %v", sql, err)
		}
	}
	for _, tc := range []struct{ sql, want string }{
		{"", "sql is required"},
		{" ;\n", "sql is required"},
		{"INSERT INTO t VALUES (1)", "only reading statements"},
		{"DROP TABLE t", "not DROP"},
		{"SET default_transaction_read_only = off", "not SET"},
		{"BEGIN READ WRITE", "not BEGIN"},
		{"/* SELECT */ DELETE FROM t", "not DELETE"},
		{"SELECT 1; DROP TABLE t", "one statement"},
		{"SELECT 1; SET default_transaction_read_only = off; DELETE FROM t", "one statement"},
		{"SELECT ';'", "one statement"}, // refused although harmless: no SQL parsing here
		{"SELECT 1\x00", "not valid text"},
		{"SELECT '" + strings.Repeat("x", MaxReadOnlySQL) + "'", "longer than"},
	} {
		if err := CheckReadStatement(tc.sql); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("CheckReadStatement(%.40q) = %v, want %q", tc.sql, err, tc.want)
		}
	}
}

func TestCheckDatabaseName(t *testing.T) {
	for _, ok := range []string{"", "app", "app_db-2", "my.db$"} {
		if err := CheckDatabaseName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"a b", "x?sslmode=disable", "a/b", "-x", strings.Repeat("a", 200), "db;drop"} {
		if err := CheckDatabaseName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestReadOnlyConnection(t *testing.T) {
	s, _, _ := openTestStore(t)
	save := func(c Connection) Connection {
		t.Helper()
		saved, err := s.Save(c, nil)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	ro := save(Connection{Name: "replica", Driver: Postgres, Host: "192.0.2.1", User: "u", ReadOnly: true})
	rw := save(Connection{Name: "primary", Driver: Postgres, Host: "192.0.2.2", User: "u"})
	save(Connection{Name: "twin", Driver: Postgres, Host: "192.0.2.3", User: "u", ReadOnly: true})
	save(Connection{Name: "twin", Driver: Postgres, Host: "192.0.2.4", User: "u", ReadOnly: true})
	m := NewManager(s)

	if c, err := m.ReadOnlyConnection("replica"); err != nil || c.ID != ro.ID {
		t.Fatalf("by name: %+v, %v", c, err)
	}
	if c, err := m.ReadOnlyConnection(ro.ID); err != nil || c.ID != ro.ID {
		t.Fatalf("by id: %+v, %v", c, err)
	}
	for _, tc := range []struct{ name, want string }{
		{"", "connection is required"},
		{"primary", `"primary" is not read-only`},
		{rw.ID, "is not read-only"},
		{"nope", `no saved database connection named "nope" (read-only connections: replica, twin, twin)`},
		{"twin", "2 connections are named"},
	} {
		if _, err := m.ReadOnlyConnection(tc.name); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ReadOnlyConnection(%q) = %v, want %q", tc.name, err, tc.want)
		}
	}
}

// A query for an agent runs only on a read-only connection, one reading
// statement, with a bounded number of rows; refusals open no session.
func TestQueryReadOnly(t *testing.T) {
	m, conn, fake := newTestManager(t, Connection{Name: "replica", Driver: Postgres, Host: "192.0.2.1", User: "u", ReadOnly: true})
	ctx := context.Background()
	res, err := m.QueryReadOnly(ctx, conn.ID, "", "q1", "SELECT 1", 0)
	if err != nil || res.Command != "SELECT 1" {
		t.Fatalf("query: %+v, %v", res, err)
	}
	if fake.lastSQL != "SELECT 1" || fake.lastLimit != DefaultReadRows {
		t.Errorf("ran %q with limit %d", fake.lastSQL, fake.lastLimit)
	}
	if _, err := m.QueryReadOnly(ctx, conn.ID, "", "q2", "SELECT 2", 100000); err != nil || fake.lastLimit != MaxReadOnlyRows {
		t.Errorf("limit %d, %v", fake.lastLimit, err)
	}
	for _, tc := range []struct{ id, database, sql, want string }{
		{"nope", "", "SELECT 1", "unknown connection"},
		{conn.ID, "", "DELETE FROM t", "only reading statements"},
		{conn.ID, "", "SELECT 1; DELETE FROM t", "one statement"},
		{conn.ID, "x y", "SELECT 1", "plain name"},
	} {
		if _, err := m.QueryReadOnly(ctx, tc.id, tc.database, "", tc.sql, 0); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("QueryReadOnly(%q, %q) = %v, want %q", tc.database, tc.sql, err, tc.want)
		}
	}
	if fake.lastSQL != "SELECT 2" {
		t.Errorf("a refused statement ran: %q", fake.lastSQL)
	}

	// The same connection without read-only mode is refused, whatever the statement.
	rw, wconn, wfake := newTestManager(t, Connection{Name: "primary", Driver: Postgres, Host: "192.0.2.2", User: "u"})
	if _, err := rw.QueryReadOnly(ctx, wconn.ID, "", "", "SELECT 1", 0); err == nil || !strings.Contains(err.Error(), "not read-only") {
		t.Fatalf("writable connection: %v", err)
	}
	if wfake.lastSQL != "" {
		t.Errorf("ran %q on a writable connection", wfake.lastSQL)
	}
}

// The query is recorded in the connection's history like a console run.
func TestQueryReadOnlyIsRecorded(t *testing.T) {
	m, conn, _ := newTestManager(t, Connection{Name: "replica", Driver: Postgres, Host: "192.0.2.1", User: "u", ReadOnly: true})
	m.SetHistory(OpenHistory(t.TempDir(), 10))
	if _, err := m.QueryReadOnly(context.Background(), conn.ID, "", "", "SELECT 42", 5); err != nil {
		t.Fatal(err)
	}
	entries, err := m.QueryHistory(conn.ID, HistoryFilter{AllDatabases: true})
	if err != nil || len(entries) != 1 || entries[0].SQL != "SELECT 42" {
		t.Fatalf("history = %+v, %v", entries, err)
	}
}

func TestRenderResult(t *testing.T) {
	res := &QueryResult{
		Columns: []Column{{Name: "id"}, {Name: "note"}, {Name: "meta"}},
		Rows: [][]any{
			{int64(1), "a\tb\nc", nil},
			{int64(2), strings.Repeat("é", 300), map[string]any{"k": true}},
		},
		ElapsedMs: 7, Truncated: true,
	}
	text := RenderResult(res)
	lines := strings.Split(text, "\n")
	if len(lines) != 4 || lines[0] != "2 rows (7 ms); more rows exist, add a LIMIT or a WHERE to see others" || lines[1] != "id\tnote\tmeta" {
		t.Fatalf("rendered:\n%s", text)
	}
	if lines[2] != `1	a\tb\nc	NULL` {
		t.Errorf("row 1 = %q", lines[2])
	}
	cells := strings.Split(lines[3], "\t")
	if len(cells) != 3 || len(cells[1]) > maxRenderedCell+4 || !strings.HasSuffix(cells[1], "…") || cells[2] != `{"k":true}` {
		t.Errorf("row 2 = %q", lines[3])
	}

	big := &QueryResult{Columns: []Column{{Name: "v"}}}
	for range MaxReadOnlyRows {
		big.Rows = append(big.Rows, []any{strings.Repeat("x", maxRenderedCell)})
	}
	if text := RenderResult(big); len(text) > maxRenderedBytes+1000 || !strings.Contains(text, "more rows not shown") {
		t.Errorf("a big result is %d bytes", len(text))
	}
	if text := RenderResult(&QueryResult{Command: "SET", Affected: 0}); !strings.HasPrefix(text, "SET (0 rows affected") {
		t.Errorf("no columns: %q", text)
	}
	if RenderResult(nil) != "No result." {
		t.Error("nil result")
	}
}
