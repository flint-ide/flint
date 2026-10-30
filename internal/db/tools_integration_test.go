//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPostgresExplainIntegration(t *testing.T) {
	m, conn := integrationManager(t, "FLINT_TEST_PG", Connection{Driver: Postgres, Database: "postgres", User: "postgres"})
	m.SetHistory(OpenHistory(t.TempDir(), 100))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	query(t, m, conn.ID, "CREATE TABLE IF NOT EXISTS flint_explain (id int primary key, v text)", 10)
	t.Cleanup(func() { query(t, m, conn.ID, "DROP TABLE IF EXISTS flint_explain", 10) })
	query(t, m, conn.ID, "INSERT INTO flint_explain SELECT g, 'v' || g FROM generate_series(1, 20000) g ON CONFLICT DO NOTHING", 10)
	query(t, m, conn.ID, "ANALYZE flint_explain", 10)

	t.Run("estimate", func(t *testing.T) {
		res, err := m.Explain(ctx, conn.ID, "", "", "SELECT * FROM flint_explain WHERE v = 'v5';", false, false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Analyze || res.Plan == nil || res.Plan.Analyzed || res.RolledBack {
			t.Fatalf("result = %+v", res)
		}
		if !strings.Contains(res.Plan.NodeType, "Seq Scan") || res.Plan.Relation != "flint_explain" {
			t.Errorf("root = %s on %s", res.Plan.NodeType, res.Plan.Relation)
		}
	})

	t.Run("analyze select", func(t *testing.T) {
		res, err := m.Explain(ctx, conn.ID, "", "", "SELECT * FROM flint_explain WHERE v = 'v5'", true, false)
		if err != nil {
			t.Fatal(err)
		}
		p := res.Plan
		if !res.Analyze || !p.Analyzed || res.ExecutionMs <= 0 || p.ActualLoops < 1 || p.Buffers == nil || !res.RolledBack {
			t.Fatalf("result = %+v plan = %+v", res, p)
		}
		// 20k rows read for one match: flagged as a big sequential scan.
		if !hasWarning(*p, "Seq Scan on a big table") {
			t.Errorf("warnings = %v", p.Warnings)
		}
	})

	t.Run("analyze write is confirmed and rolled back", func(t *testing.T) {
		const del = "DELETE FROM flint_explain WHERE id <= 100"
		res, err := m.Explain(ctx, conn.ID, "", "", del, true, false)
		if err != nil || !res.NeedsConfirm {
			t.Fatalf("unconfirmed: %+v, %v", res, err)
		}
		res, err = m.Explain(ctx, conn.ID, "", "", del, true, true)
		if err != nil {
			t.Fatal(err)
		}
		if !res.RolledBack || !hasDetail(res.Plan, "Operation", "Delete") {
			t.Errorf("result = %+v", res.Plan)
		}
		left := query(t, m, conn.ID, "SELECT count(*) FROM flint_explain WHERE id <= 100", 10)
		if left.Rows[0][0] != int64(100) {
			t.Errorf("rows after rolled-back EXPLAIN ANALYZE DELETE = %v", left.Rows[0][0])
		}
	})

	t.Run("one statement only", func(t *testing.T) {
		if _, err := m.Explain(ctx, conn.ID, "", "", "SELECT 1; DELETE FROM flint_explain", false, false); err == nil {
			t.Error("multi-statement EXPLAIN accepted")
		}
	})

	t.Run("recorded in history", func(t *testing.T) {
		got, err := m.QueryHistory(conn.ID, HistoryFilter{Filter: "flint_explain where v"})
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]bool{}
		for _, e := range got {
			kinds[e.Kind] = true
		}
		if !kinds[HistoryExplain] || !kinds[HistoryExplainAnalyze] {
			t.Errorf("history kinds = %v (%d entries)", kinds, len(got))
		}
	})
}

func TestPostgresEditIntegration(t *testing.T) {
	m, conn := integrationManager(t, "FLINT_TEST_PG", Connection{Driver: Postgres, Database: "postgres", User: "postgres"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	query(t, m, conn.ID, `DROP TABLE IF EXISTS "Flint Edit"`, 10)
	query(t, m, conn.ID, `CREATE TABLE "Flint Edit" (
		"Id" int, line int2, "na""me" varchar(10), doc jsonb, ok boolean, at timestamptz,
		tags int[], total numeric GENERATED ALWAYS AS (line * 2) STORED, PRIMARY KEY ("Id", line))`, 10)
	t.Cleanup(func() { query(t, m, conn.ID, `DROP TABLE IF EXISTS "Flint Edit"`, 10) })
	query(t, m, conn.ID, `INSERT INTO "Flint Edit" ("Id", line, "na""me", doc, ok, at) VALUES
		(1, 1, 'a', '{"x": 1}', true, '2024-01-02 03:04:05+00'), (1, 2, 'b', NULL, false, NULL)`, 10)

	res := query(t, m, conn.ID, `SELECT *, 1 + 1 AS two FROM "Flint Edit" ORDER BY line`, 10)
	if res.TableOID == 0 || res.EditBlocked != "" {
		t.Fatalf("result not editable: oid %d, %q", res.TableOID, res.EditBlocked)
	}
	if res.Attnums[0] != 1 || res.Attnums[len(res.Attnums)-1] != 0 {
		t.Errorf("attnums = %v", res.Attnums)
	}
	joined := query(t, m, conn.ID, `SELECT a.line, b.relname FROM "Flint Edit" a, pg_class b LIMIT 1`, 10)
	if joined.TableOID != 0 || joined.EditBlocked == "" {
		t.Errorf("join editable: oid %d", joined.TableOID)
	}

	target, err := m.EditTarget(ctx, conn.ID, "", res.TableOID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Table != "Flint Edit" || strings.Join(target.Key, ",") != "Id,line" {
		t.Fatalf("target = %+v", target)
	}
	byName := map[string]EditColumn{}
	for _, c := range target.Columns {
		byName[c.Name] = c
	}
	if c := byName["tags"]; c.Editable {
		t.Errorf("array column editable: %+v", c)
	}
	if c := byName["total"]; c.Editable || c.ReadOnlyReason != "generated column" {
		t.Errorf("generated column = %+v", c)
	}
	if byName["doc"].Kind != KindJSON || byName["ok"].Kind != KindBool || byName["line"].Kind != KindNumber {
		t.Errorf("kinds = %+v", target.Columns)
	}

	// Key values exactly as the grid has them (normalized), values as typed.
	key := func(line string) map[string]any { return map[string]any{"Id": sp("1"), "line": sp(line)} }
	batch := EditBatch{TableOID: res.TableOID, Edits: []RowEdit{
		{Op: EditUpdate, Key: key("1"), Values: map[string]any{`na"me`: sp("O'Brien"), "doc": sp(`{"y": [1, 2]}`), "ok": nil, "at": sp("2025-05-06 07:08:09 Z")}},
		{Op: EditDelete, Key: key("2")},
		{Op: EditInsert, Values: map[string]any{"Id": sp("2"), "line": sp("1"), "ok": sp("true")}},
	}}
	preview, err := m.PreviewEdits(ctx, conn.ID, "", batch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, `'O''Brien'::character varying`) || strings.Count(preview, ";\n") != 3 {
		t.Errorf("preview = %s", preview)
	}
	applied, err := m.ApplyEdits(ctx, conn.ID, "", batch)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Statements != 3 || applied.Affected != 3 {
		t.Errorf("applied = %+v", applied)
	}
	after := query(t, m, conn.ID, `SELECT "Id", line, "na""me", doc->'y'->>1, ok, at = '2025-05-06 07:08:09+00', total FROM "Flint Edit" ORDER BY "Id", line`, 10)
	if len(after.Rows) != 2 {
		t.Fatalf("rows after edit = %v", after.Rows)
	}
	r := after.Rows[0]
	if r[2] != "O'Brien" || r[3] != "2" || r[4] != nil || r[5] != true || r[6] != "2" {
		t.Errorf("updated row = %#v", r)
	}
	if after.Rows[1][0] != int64(2) || after.Rows[1][4] != true {
		t.Errorf("inserted row = %#v", after.Rows[1])
	}

	t.Run("stale row rolls everything back", func(t *testing.T) {
		_, err := m.ApplyEdits(ctx, conn.ID, "", EditBatch{TableOID: res.TableOID, Edits: []RowEdit{
			{Op: EditUpdate, Key: key("1"), Values: map[string]any{`na"me`: sp("changed")}},
			{Op: EditDelete, Key: key("99")},
		}})
		if err == nil || !strings.Contains(err.Error(), "instead of 1") {
			t.Fatalf("err = %v", err)
		}
		name := query(t, m, conn.ID, `SELECT "na""me" FROM "Flint Edit" WHERE line = 1 AND "Id" = 1`, 10)
		if name.Rows[0][0] != "O'Brien" {
			t.Errorf("first change not rolled back: %v", name.Rows[0][0])
		}
	})

	t.Run("bad value", func(t *testing.T) {
		_, err := m.ApplyEdits(ctx, conn.ID, "", EditBatch{TableOID: res.TableOID, Edits: []RowEdit{
			{Op: EditUpdate, Key: key("1"), Values: map[string]any{`na"me`: sp("far too long for ten")}},
		}})
		if err == nil {
			t.Error("varchar(10) overflow accepted")
		}
	})

	t.Run("read-only connection", func(t *testing.T) {
		ro, roConn := integrationManager(t, "FLINT_TEST_PG", Connection{Driver: Postgres, Database: "postgres", User: "postgres", ReadOnly: true})
		if _, err := ro.ApplyEdits(ctx, roConn.ID, "", EditBatch{TableOID: res.TableOID, Edits: []RowEdit{{Op: EditDelete, Key: key("1")}}}); err != ErrReadOnly {
			t.Errorf("err = %v", err)
		}
	})
}

func TestClickHouseExplainIntegration(t *testing.T) {
	for _, env := range []struct{ name, env, protocol string }{
		{"native", "FLINT_TEST_CH", ProtocolNative},
		{"http", "FLINT_TEST_CH_HTTP", ProtocolHTTP},
	} {
		t.Run(env.name, func(t *testing.T) {
			m, conn := integrationManager(t, env.env, Connection{Driver: ClickHouse, User: "flint", Protocol: env.protocol})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			sql := "SELECT number % 10 AS k, count() FROM numbers(1000) GROUP BY k ORDER BY k"
			plan, err := m.Explain(ctx, conn.ID, "", "", sql, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Lines) < 2 || plan.Lines[0].Depth != 0 || !strings.HasPrefix(plan.Statement, "EXPLAIN PLAN") {
				t.Fatalf("plan = %+v", plan)
			}
			deeper := false
			for _, l := range plan.Lines {
				deeper = deeper || l.Depth > 0
			}
			if !deeper {
				t.Errorf("plan has no nesting: %+v", plan.Lines)
			}
			pipe, err := m.Explain(ctx, conn.ID, "", "", sql, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(pipe.Lines) == 0 || !pipe.Analyze || pipe.NeedsConfirm {
				t.Errorf("pipeline = %+v", pipe)
			}
			if _, err := m.EditTarget(ctx, conn.ID, "", 1); err != ErrEditUnsupported {
				t.Errorf("clickhouse edit err = %v", err)
			}
		})
	}
}
