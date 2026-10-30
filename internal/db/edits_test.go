package db

import (
	"context"
	"strings"
	"testing"
)

func sp(s string) any { return s }

// weirdTarget has identifiers that need quoting (a quote, a space, upper case, a "$1").
func weirdTarget() EditTarget {
	return EditTarget{
		TableOID: 42,
		Schema:   `My "Schema"`,
		Table:    "order items",
		Key:      []string{"Id", "line"},
		Columns: []EditColumn{
			{Attnum: 1, Name: "Id", Type: "integer", Kind: KindNumber, PrimaryKey: true, Editable: true},
			{Attnum: 2, Name: "line", Type: "smallint", Kind: KindNumber, PrimaryKey: true, Editable: true},
			{Attnum: 3, Name: `na"me$1`, Type: "character varying", Kind: KindText, Editable: true},
			{Attnum: 4, Name: "doc", Type: "jsonb", Kind: KindJSON, Editable: true},
			{Attnum: 5, Name: "total", Type: "numeric", Kind: KindNumber, Editable: false, ReadOnlyReason: "generated column"},
		},
	}
}

func TestBuildEditStatementsUpdate(t *testing.T) {
	stmts, err := BuildEditStatements(weirdTarget(), []RowEdit{{
		Op:     EditUpdate,
		Key:    map[string]any{"line": sp("2"), "Id": sp("7")},
		Values: map[string]any{"doc": nil, `na"me$1`: sp("O'Brien; DROP TABLE x")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 {
		t.Fatalf("statements = %+v", stmts)
	}
	st := stmts[0]
	wantSQL := `UPDATE "My ""Schema"""."order items" SET "na""me$1" = $1::text::character varying, "doc" = $2::text::jsonb` +
		` WHERE "Id" = $3::text::integer AND "line" = $4::text::smallint`
	if st.SQL != wantSQL {
		t.Errorf("sql =\n%s\nwant\n%s", st.SQL, wantSQL)
	}
	// Values only travel as parameters, in placeholder order.
	if len(st.Args) != 4 || *st.Args[0] != "O'Brien; DROP TABLE x" || st.Args[1] != nil || *st.Args[2] != "7" || *st.Args[3] != "2" {
		t.Errorf("args = %v", st.Args)
	}
	if strings.Contains(st.SQL, "O'Brien") {
		t.Error("value inlined into executed SQL")
	}
	if !st.ExpectOne {
		t.Error("UPDATE by key must expect one row")
	}
	wantPreview := `UPDATE "My ""Schema"""."order items" SET "na""me$1" = 'O''Brien; DROP TABLE x'::character varying, "doc" = NULL::jsonb` +
		` WHERE "Id" = '7'::integer AND "line" = '2'::smallint`
	if st.Preview != wantPreview {
		t.Errorf("preview =\n%s\nwant\n%s", st.Preview, wantPreview)
	}
}

func TestBuildEditStatementsInsertDelete(t *testing.T) {
	stmts, err := BuildEditStatements(weirdTarget(), []RowEdit{
		{Op: EditInsert, Values: map[string]any{"line": sp("1"), "Id": sp("9"), "doc": sp(`{"a": 1}`)}},
		{Op: EditInsert, Values: map[string]any{}},
		{Op: EditDelete, Key: map[string]any{"Id": sp("1"), "line": sp("1")}},
		{Op: EditUpdate, Key: map[string]any{"Id": sp("1"), "line": sp("1")}, Values: map[string]any{}}, // no-op
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 3 {
		t.Fatalf("statements = %d, want 3 (empty update skipped)", len(stmts))
	}
	if want := `INSERT INTO "My ""Schema"""."order items" ("Id", "line", "doc") VALUES ($1::text::integer, $2::text::smallint, $3::text::jsonb)`; stmts[0].SQL != want {
		t.Errorf("insert = %s", stmts[0].SQL)
	}
	if stmts[0].ExpectOne {
		t.Error("INSERT shouldn't check affected rows")
	}
	if want := `INSERT INTO "My ""Schema"""."order items" DEFAULT VALUES`; stmts[1].SQL != want {
		t.Errorf("default insert = %s", stmts[1].SQL)
	}
	if want := `DELETE FROM "My ""Schema"""."order items" WHERE "Id" = $1::text::integer AND "line" = $2::text::smallint`; stmts[2].SQL != want || !stmts[2].ExpectOne {
		t.Errorf("delete = %s", stmts[2].SQL)
	}
	preview := PreviewSQL(stmts)
	if strings.Count(preview, ";\n") != 3 || !strings.Contains(preview, `'{"a": 1}'::jsonb`) {
		t.Errorf("preview = %s", preview)
	}
}

func TestBuildEditStatementsRejects(t *testing.T) {
	target := weirdTarget()
	key := map[string]any{"Id": sp("1"), "line": sp("1")}
	cases := map[string]RowEdit{
		"unknown column":    {Op: EditUpdate, Key: key, Values: map[string]any{`x" = 1; --`: sp("1")}},
		"read-only column":  {Op: EditUpdate, Key: key, Values: map[string]any{"total": sp("1")}},
		"read-only insert":  {Op: EditInsert, Values: map[string]any{"total": sp("1")}},
		"partial key":       {Op: EditDelete, Key: map[string]any{"Id": sp("1")}},
		"extra key column":  {Op: EditDelete, Key: map[string]any{"Id": sp("1"), "line": sp("1"), "doc": sp("{}")}},
		"wrong key column":  {Op: EditDelete, Key: map[string]any{"Id": sp("1"), "doc": sp("1")}},
		"null key":          {Op: EditDelete, Key: map[string]any{"Id": sp("1"), "line": nil}},
		"unknown operation": {Op: "upsert", Key: key},
		"non-text value":    {Op: EditInsert, Values: map[string]any{"Id": 5.0}},
		"non-text key":      {Op: EditDelete, Key: map[string]any{"Id": true, "line": sp("1")}},
	}
	for name, e := range cases {
		if _, err := BuildEditStatements(target, []RowEdit{e}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	noKey := target
	noKey.Key = nil
	if _, err := BuildEditStatements(noKey, []RowEdit{{Op: EditInsert}}); err != ErrNoPrimaryKey {
		t.Errorf("no primary key err = %v", err)
	}
}

func TestResultSource(t *testing.T) {
	oid, attnums, reason := resultSource([]uint32{10, 0, 10, 10}, []uint16{1, 0, 3, 1})
	if oid != 10 || reason != "" {
		t.Fatalf("oid %d reason %q", oid, reason)
	}
	// The expression and the repeated column 1 can't be edited.
	if want := []int{1, 0, 3, 0}; !equalInts(attnums, want) {
		t.Errorf("attnums = %v, want %v", attnums, want)
	}
	if _, _, reason := resultSource([]uint32{10, 11}, []uint16{1, 1}); reason == "" {
		t.Error("two tables not blocked")
	}
	if _, _, reason := resultSource([]uint32{0, 0}, []uint16{0, 0}); reason == "" {
		t.Error("expressions only not blocked")
	}
}

func TestEditColumnKind(t *testing.T) {
	cases := []struct{ typname, category, kind, reason string }{
		{"int4", "N", KindNumber, ""},
		{"numeric", "N", KindNumber, ""},
		{"bool", "B", KindBool, ""},
		{"jsonb", "U", KindJSON, ""},
		{"text", "S", KindText, ""},
		{"timestamptz", "D", KindText, ""},
		{"mood", "E", KindText, ""},
		{"_int4", "A", "", "array"},
		{"bytea", "U", "", "binary"},
	}
	for _, c := range cases {
		kind, reason := editColumnKind(c.typname, c.category)
		if kind != c.kind || reason != c.reason {
			t.Errorf("%s: kind %q reason %q", c.typname, kind, reason)
		}
	}
}

func TestManagerEditsRespectReadOnlyAndDriver(t *testing.T) {
	ro, roConn, _ := newTestManager(t, Connection{Name: "ro", Driver: Postgres, Host: "h", User: "u", ReadOnly: true})
	if _, err := ro.ApplyEdits(context.Background(), roConn.ID, "", EditBatch{TableOID: 1}); err != ErrReadOnly {
		t.Errorf("read-only apply err = %v", err)
	}
	m, conn, _ := newTestManager(t, Connection{Name: "pg", Driver: Postgres, Host: "h", User: "u"})
	if _, err := m.EditTarget(context.Background(), conn.ID, "", 1); err != ErrEditUnsupported {
		t.Errorf("fake session edit err = %v", err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
