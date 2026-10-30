package db

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestFirstKeyword(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT 1", "SELECT"},
		{"select * from t", "SELECT"},
		{"  \n\tinsert into t values (1)", "INSERT"},
		{"-- leading comment\nSELECT 1", "SELECT"},
		{"-- one\n-- two\n  update t set x = 1", "UPDATE"},
		{"/* block */ DELETE FROM t", "DELETE"},
		{"/* multi\nline */\n-- and line\nWITH x AS (SELECT 1) SELECT * FROM x", "WITH"},
		{"((SELECT 1))", "SELECT"},
		{"( /* c */ select 1)", "SELECT"},
		{"show_tables", "SHOW_TABLES"},
		{"SELECT;", "SELECT"},
		{"explain(select 1)", "EXPLAIN"},
		{"-- only a comment", ""},
		{"/* unterminated", ""},
		{"", ""},
		{"   ", ""},
		{"123", ""},
	}
	for _, tt := range tests {
		if got := firstKeyword(tt.sql); got != tt.want {
			t.Errorf("firstKeyword(%q) = %q, want %q", tt.sql, got, tt.want)
		}
	}
}

func TestIsReadStatement(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"SELECT 1", true},
		{"with a as (select 1) select * from a", true},
		{"SHOW TABLES", true},
		{"DESCRIBE t", true},
		{"desc t", true},
		{"EXPLAIN SELECT 1", true},
		{"EXISTS TABLE t", true},
		{"VALUES (1)", true},
		{"TABLE t", true},
		{"-- comment\n(select 1)", true},
		{"INSERT INTO t VALUES (1)", false},
		{"ALTER TABLE t DELETE WHERE 1", false},
		{"DROP TABLE t", false},
		{"TRUNCATE t", false},
		{"OPTIMIZE TABLE t", false},
		{"SYSTEM FLUSH LOGS", false},
		{"SELECTx", false},
		{"", false},
		{"/* SELECT */ DROP TABLE t", false},
	}
	for _, tt := range tests {
		if got := isReadStatement(tt.sql); got != tt.want {
			t.Errorf("isReadStatement(%q) = %v, want %v", tt.sql, got, tt.want)
		}
	}
}

func TestGroupTables(t *testing.T) {
	if got := groupTables(nil); got != nil {
		t.Errorf("groupTables(nil) = %v, want nil", got)
	}
	rows := []schemaRow{
		{"public", "users", "table", "id", "int8"},
		{"public", "users", "table", "email", "text"},
		{"public", "v_users", "view", "id", "int8"},
		{"audit", "users", "table", "at", "timestamptz"}, // same name, different schema
	}
	want := []Table{
		{Schema: "public", Name: "users", Kind: "table", Columns: []Column{{"id", "int8"}, {"email", "text"}}},
		{Schema: "public", Name: "v_users", Kind: "view", Columns: []Column{{"id", "int8"}}},
		{Schema: "audit", Name: "users", Kind: "table", Columns: []Column{{"at", "timestamptz"}}},
	}
	if got := groupTables(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("groupTables() = %+v, want %+v", got, want)
	}
}

func TestClickhouseKind(t *testing.T) {
	tests := map[string]string{
		"MergeTree":        "table",
		"MaterializedView": "materialized view",
		"View":             "view",
		"LiveView":         "view",
		"Dictionary":       "dictionary",
		"":                 "table",
	}
	for engine, want := range tests {
		if got := clickhouseKind(engine); got != want {
			t.Errorf("clickhouseKind(%q) = %q, want %q", engine, got, want)
		}
	}
}

func TestOpenSessionUnsupportedDriver(t *testing.T) {
	_, err := openSession(context.Background(), Connection{Driver: "mysql"}, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported driver") {
		t.Errorf("error = %v, want unsupported driver", err)
	}
}
