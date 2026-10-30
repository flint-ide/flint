package db

import (
	"context"
	"strings"
	"testing"
)

// analyzedPlan is trimmed EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) output of a hash join
// over a big sequential scan whose sort spilled to disk.
const analyzedPlan = `[
  {
    "Plan": {
      "Node Type": "Sort",
      "Startup Cost": 5000.0, "Total Cost": 5100.5, "Plan Rows": 100, "Plan Width": 12,
      "Actual Startup Time": 90.0, "Actual Total Time": 100.0, "Actual Rows": 50000, "Actual Loops": 1,
      "Sort Key": ["o.created_at DESC"], "Sort Method": "external merge", "Sort Space Used": 2048, "Sort Space Type": "Disk",
      "Shared Hit Blocks": 10, "Shared Read Blocks": 300, "Temp Read Blocks": 256, "Temp Written Blocks": 256,
      "Plans": [
        {
          "Node Type": "Hash Join", "Parent Relationship": "Outer", "Join Type": "Inner",
          "Startup Cost": 10.0, "Total Cost": 4000.0, "Plan Rows": 100, "Plan Width": 12,
          "Actual Startup Time": 1.0, "Actual Total Time": 80.0, "Actual Rows": 50000, "Actual Loops": 1,
          "Hash Cond": "(o.user_id = u.id)",
          "Plans": [
            {
              "Node Type": "Seq Scan", "Parent Relationship": "Outer", "Relation Name": "orders", "Schema": "public", "Alias": "o",
              "Startup Cost": 0.0, "Total Cost": 3000.0, "Plan Rows": 60000, "Plan Width": 12,
              "Actual Startup Time": 0.01, "Actual Total Time": 50.0, "Actual Rows": 50000, "Actual Loops": 1,
              "Filter": "(status = 'paid'::text)", "Rows Removed by Filter": 10000
            },
            {
              "Node Type": "Hash", "Parent Relationship": "Inner",
              "Startup Cost": 8.0, "Total Cost": 8.0, "Plan Rows": 200, "Plan Width": 4,
              "Actual Startup Time": 0.5, "Actual Total Time": 0.5, "Actual Rows": 200, "Actual Loops": 1,
              "Hash Buckets": 1024, "Hash Batches": 4,
              "Plans": [
                {
                  "Node Type": "Index Only Scan", "Parent Relationship": "Outer", "Relation Name": "users", "Alias": "u",
                  "Index Name": "users_pkey",
                  "Startup Cost": 0.1, "Total Cost": 8.0, "Plan Rows": 200, "Plan Width": 4,
                  "Actual Startup Time": 0.01, "Actual Total Time": 0.004, "Actual Rows": 2, "Actual Loops": 100,
                  "Heap Fetches": 0
                }
              ]
            }
          ]
        }
      ]
    },
    "Planning Time": 0.25,
    "Triggers": [],
    "Execution Time": 101.5
  }
]`

func TestParsePostgresPlanAnalyzed(t *testing.T) {
	res, err := ParsePostgresPlan([]byte(analyzedPlan))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Analyze || res.PlanningMs != 0.25 || res.ExecutionMs != 101.5 {
		t.Errorf("summary = analyze %v planning %v execution %v", res.Analyze, res.PlanningMs, res.ExecutionMs)
	}
	sort := res.Plan
	join := sort.Children[0]
	scan := join.Children[0]
	hash := join.Children[1]
	index := hash.Children[0]

	if sort.SelfMs != 20 || join.SelfMs != 29.5 || scan.SelfMs != 50 {
		t.Errorf("self ms: sort %v join %v scan %v", sort.SelfMs, join.SelfMs, scan.SelfMs)
	}
	// Loops multiply per-loop times: 0.004 ms × 100 loops.
	if index.TotalMs < 0.39 || index.TotalMs > 0.41 {
		t.Errorf("index total ms = %v", index.TotalMs)
	}
	// Hash's child total (0.4) is under its own 0.5.
	if hash.SelfMs < 0.09 || hash.SelfMs > 0.11 {
		t.Errorf("hash self ms = %v", hash.SelfMs)
	}
	if sort.SelfCost != 1100.5 || join.SelfCost != 992 {
		t.Errorf("self cost: sort %v join %v", sort.SelfCost, join.SelfCost)
	}
	if scan.Relation != "public.orders" || scan.Alias != "o" || scan.Filter == "" || scan.RowsRemovedByFilter != 10000 {
		t.Errorf("scan = %+v", scan)
	}
	if index.Index != "users_pkey" || index.Relation != "users" {
		t.Errorf("index scan = %+v", index)
	}
	if sort.Buffers == nil || sort.Buffers.SharedRead != 300 || sort.Buffers.TempWritten != 256 {
		t.Errorf("buffers = %+v", sort.Buffers)
	}
	if scan.Buffers != nil {
		t.Errorf("scan without buffer keys has buffers %+v", scan.Buffers)
	}
	if !hasDetail(sort, "Sort Key", "o.created_at DESC") || !hasDetail(&join, "Hash Cond", "(o.user_id = u.id)") {
		t.Errorf("details: sort %+v join %+v", sort.Details, join.Details)
	}

	wantWarn := map[string]*PlanNode{
		"Sort spilled to disk":           sort,
		"Seq Scan on a big table: 60000": &scan,
		"Hash spilled to disk in 4":      &hash,
		"Row estimate off by 500×":       sort, // 100 planned, 50000 actual
	}
	for prefix, node := range wantWarn {
		if !hasWarning(*node, prefix) {
			t.Errorf("%s: no warning %q in %v", node.NodeType, prefix, node.Warnings)
		}
	}
	if len(index.Warnings) != 0 {
		// 200 planned vs 2 actual per loop is a 100× overestimate.
		if !hasWarning(index, "Row estimate off by 100× (overestimated") {
			t.Errorf("index warnings = %v", index.Warnings)
		}
	}
	if res.Warnings < 5 {
		t.Errorf("warning count = %d", res.Warnings)
	}
}

func TestParsePostgresPlanEstimateOnly(t *testing.T) {
	raw := `[{"Plan": {"Node Type": "Limit", "Startup Cost": 0, "Total Cost": 0.5, "Plan Rows": 10, "Plan Width": 4,
		"Plans": [{"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Relation Name": "big",
		"Startup Cost": 0, "Total Cost": 15000, "Plan Rows": 1000000, "Plan Width": 4}]}, "Planning Time": 0.1}]`
	res, err := ParsePostgresPlan([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	limit := res.Plan
	if res.Analyze || limit.Analyzed || limit.SelfMs != 0 {
		t.Errorf("estimate-only plan marked analyzed: %+v", limit)
	}
	// A Limit is cheaper than its child: self cost clamps at 0.
	if limit.SelfCost != 0 {
		t.Errorf("limit self cost = %v", limit.SelfCost)
	}
	if !hasWarning(limit.Children[0], "Seq Scan on a big table (estimated 1000000 rows") {
		t.Errorf("seq scan warnings = %v", limit.Children[0].Warnings)
	}
	if len(limit.Warnings) != 0 {
		t.Errorf("no row-estimate warnings without ANALYZE: %v", limit.Warnings)
	}
}

func TestParsePostgresPlanNeverExecuted(t *testing.T) {
	raw := `[{"Plan": {"Node Type": "Seq Scan", "Relation Name": "t", "Total Cost": 99999, "Plan Rows": 500000,
		"Actual Startup Time": 0, "Actual Total Time": 0, "Actual Rows": 0, "Actual Loops": 0}}]`
	res, err := ParsePostgresPlan([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Plan.NeverExecuted || len(res.Plan.Warnings) != 0 {
		t.Errorf("never executed node: %+v", res.Plan)
	}
}

func TestParsePostgresPlanErrors(t *testing.T) {
	for _, raw := range []string{``, `[]`, `[{"x": 1}]`, `{"Plan": {}}`} {
		if _, err := ParsePostgresPlan([]byte(raw)); err == nil {
			t.Errorf("ParsePostgresPlan(%q) succeeded", raw)
		}
	}
}

func TestParseTextPlan(t *testing.T) {
	lines := ParseTextPlan([]string{
		"Expression ((Projection + Before ORDER BY))",
		"  Aggregating",
		"    Expression (Before GROUP BY)",
		"      ReadFromMergeTree (default.hits)",
		"      Indexes:",
		"",
		"\tTabbed",
	})
	want := []ExplainLine{
		{0, "Expression ((Projection + Before ORDER BY))"},
		{1, "Aggregating"},
		{2, "Expression (Before GROUP BY)"},
		{3, "ReadFromMergeTree (default.hits)"},
		{3, "Indexes:"},
		{1, "Tabbed"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
}

func TestExplainStatements(t *testing.T) {
	if got := postgresExplainSQL("SELECT 1", true); got != "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT 1" {
		t.Errorf("analyze = %q", got)
	}
	if got := clickhouseExplainSQL("SELECT 1", true); got != "EXPLAIN PIPELINE SELECT 1" {
		t.Errorf("pipeline = %q", got)
	}
	if s, err := explainTarget("  SELECT 1;  \n"); err != nil || s != "SELECT 1" {
		t.Errorf("explainTarget = %q, %v", s, err)
	}
	for _, bad := range []string{"", " ; ", "explain select 1", "/* x */ EXPLAIN SELECT 1"} {
		if _, err := explainTarget(bad); err == nil {
			t.Errorf("explainTarget(%q) accepted", bad)
		}
	}
}

func TestAnalyzeModifies(t *testing.T) {
	cases := map[string]bool{
		"SELECT * FROM t": false,
		"  (SELECT 1)":    false,
		"VALUES (1)":      false,
		"TABLE t":         false,
		"SELECT 'insert into' AS x, \"update\" FROM t":          false,
		"SELECT 1 -- delete later":                              false,
		"WITH x AS (SELECT 1) SELECT * FROM x":                  false,
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d": true,
		"SELECT * INTO copy FROM t":                             true,
		"UPDATE t SET a = 1":                                    true,
		"delete from t":                                         true,
		"CREATE TABLE x AS SELECT 1":                            true,
	}
	for sql, want := range cases {
		if got := analyzeModifies(sql); got != want {
			t.Errorf("analyzeModifies(%q) = %v, want %v", sql, got, want)
		}
	}
}

func TestManagerExplainAsksBeforeAnalyzingWrites(t *testing.T) {
	m, conn, _ := newTestManager(t, Connection{Name: "pg", Driver: Postgres, Host: "h", User: "u"})
	res, err := m.Explain(context.Background(), conn.ID, "", "", "DELETE FROM t", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsConfirm || res.ConfirmReason == "" {
		t.Errorf("result = %+v, want a confirmation request", res)
	}
	// The fake session can't explain: with confirmation it gets as far as the session.
	if _, err := m.Explain(context.Background(), conn.ID, "", "", "DELETE FROM t", true, true); err == nil ||
		!strings.Contains(err.Error(), "isn't supported") {
		t.Errorf("confirmed explain err = %v", err)
	}

	ro, roConn, _ := newTestManager(t, Connection{Name: "ro", Driver: Postgres, Host: "h", User: "u", ReadOnly: true})
	if _, err := ro.Explain(context.Background(), roConn.ID, "", "", "UPDATE t SET a = 1", true, true); err != ErrReadOnly {
		t.Errorf("read-only analyze err = %v", err)
	}
}

func hasDetail(n *PlanNode, key, value string) bool {
	for _, d := range n.Details {
		if d.Key == key && d.Value == value {
			return true
		}
	}
	return false
}

func hasWarning(n PlanNode, prefix string) bool {
	for _, w := range n.Warnings {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}
