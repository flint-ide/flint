package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func record(t *testing.T, h *History, connID, database, sql string) HistoryEntry {
	t.Helper()
	e, err := h.Record(connID, HistoryEntry{Database: database, SQL: sql, At: 1})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func fileLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

func TestHistoryPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(dir, 10)
	record(t, h, "c1", "app", "SELECT 1")
	record(t, h, "c1", "app", "SELECT 2")
	record(t, h, "c1", "other", "SELECT 3")
	record(t, h, "c2", "app", "SELECT 4")

	info, err := os.Stat(filepath.Join(dir, "c1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("history file mode = %v, want 0600", info.Mode().Perm())
	}

	reopened := OpenHistory(dir, 10)
	got, err := reopened.List("c1", HistoryFilter{Database: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SQL != "SELECT 2" || got[1].SQL != "SELECT 1" {
		t.Errorf("app history (newest first) = %+v", got)
	}
	all, _ := reopened.List("c1", HistoryFilter{AllDatabases: true})
	if len(all) != 3 || all[0].SQL != "SELECT 3" {
		t.Errorf("all databases = %+v", all)
	}
	other, _ := reopened.List("c2", HistoryFilter{AllDatabases: true})
	if len(other) != 1 || other[0].SQL != "SELECT 4" {
		t.Errorf("c2 = %+v", other)
	}
	// IDs stay unique and increasing across a reload.
	next := record(t, reopened, "c1", "app", "SELECT 5")
	if next.ID <= all[0].ID {
		t.Errorf("id %d not after %d", next.ID, all[0].ID)
	}
}

func TestHistoryBoundedAndCompacted(t *testing.T) {
	dir := t.TempDir()
	const limit = 20
	h := OpenHistory(dir, limit)
	for i := range 100 {
		record(t, h, "c", "", fmt.Sprintf("SELECT %d", i))
	}
	got, _ := h.List("c", HistoryFilter{AllDatabases: true})
	if len(got) != limit || got[0].SQL != "SELECT 99" || got[limit-1].SQL != "SELECT 80" {
		t.Fatalf("kept %d entries: first %q last %q", len(got), got[0].SQL, got[len(got)-1].SQL)
	}
	// The file is compacted once it holds 25% more lines than the limit.
	if n := fileLines(t, filepath.Join(dir, "c.jsonl")); n > limit+limit/4 {
		t.Errorf("file has %d lines, want at most %d", n, limit+limit/4)
	}
	reloaded, _ := OpenHistory(dir, limit).List("c", HistoryFilter{AllDatabases: true})
	if len(reloaded) != limit || reloaded[0].SQL != "SELECT 99" {
		t.Errorf("reloaded %d, first %q", len(reloaded), reloaded[0].SQL)
	}
}

func TestHistorySkipsTornLines(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(dir, 10)
	record(t, h, "c", "", "SELECT 1")
	f, err := os.OpenFile(filepath.Join(dir, "c.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"id":5,"sql":"SELEC`) // a crash mid-append
	f.Close()

	h2 := OpenHistory(dir, 10)
	got, _ := h2.List("c", HistoryFilter{AllDatabases: true})
	if len(got) != 1 {
		t.Errorf("entries = %+v", got)
	}
}

func TestHistoryFilterIsFuzzy(t *testing.T) {
	h := OpenHistory(t.TempDir(), 100)
	record(t, h, "c", "", "SELECT *\n  FROM orders WHERE id = 1")
	record(t, h, "c", "", "SELECT * FROM users")
	record(t, h, "c", "", "UPDATE orders SET paid = true")

	got, _ := h.List("c", HistoryFilter{AllDatabases: true, Filter: "from ord"})
	if len(got) != 1 || !strings.Contains(got[0].SQL, "orders WHERE") {
		t.Errorf("filter across line breaks = %+v", got)
	}
	got, _ = h.List("c", HistoryFilter{AllDatabases: true, Filter: "upd ord"})
	if len(got) != 1 || !strings.HasPrefix(got[0].SQL, "UPDATE") {
		t.Errorf("fuzzy filter = %+v", got)
	}
	got, _ = h.List("c", HistoryFilter{AllDatabases: true, Limit: 2})
	if len(got) != 2 {
		t.Errorf("limit = %d", len(got))
	}
}

func TestHistoryClearAndInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(dir, 10)
	record(t, h, "c", "", "SELECT 1")
	if err := h.Clear("c"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still there: %v", err)
	}
	if got, _ := h.List("c", HistoryFilter{AllDatabases: true}); len(got) != 0 {
		t.Errorf("after clear = %+v", got)
	}
	for _, bad := range []string{"", "../x", "a/b", "x.y"} {
		if _, err := h.Record(bad, HistoryEntry{SQL: "x"}); err == nil {
			t.Errorf("Record(%q) accepted", bad)
		}
	}
}

func TestHistoryRedactsPasswordsOnly(t *testing.T) {
	h := OpenHistory(t.TempDir(), 10)
	cases := map[string]string{
		"CREATE ROLE app LOGIN PASSWORD 's3cr''et'":                   "CREATE ROLE app LOGIN PASSWORD '***'",
		"alter user bob with password 'x'":                            "alter user bob with password '***'",
		"CREATE USER u IDENTIFIED WITH sha256_password BY 'pw'":       "CREATE USER u IDENTIFIED WITH sha256_password BY '***'",
		"CREATE USER u IDENTIFIED BY 'pw' DEFAULT DATABASE d":         "CREATE USER u IDENTIFIED BY '***' DEFAULT DATABASE d",
		"SELECT 'password' AS word, password_hash FROM users -- note": "SELECT 'password' AS word, password_hash FROM users -- note",
	}
	for in, want := range cases {
		e := record(t, h, "c", "", in)
		if e.SQL != want {
			t.Errorf("redact(%q) = %q, want %q", in, e.SQL, want)
		}
	}
}

func TestHistoryClipsHugeStatements(t *testing.T) {
	h := OpenHistory(t.TempDir(), 10)
	big := strings.Repeat("ż", maxHistorySQL) // 2 bytes per rune
	e := record(t, h, "c", "", big)
	if !e.Truncated || len(e.SQL) > maxHistorySQL || !strings.HasPrefix(big, e.SQL) {
		t.Errorf("clipped to %d bytes, truncated=%v", len(e.SQL), e.Truncated)
	}
}

func TestHistoryConcurrentRecords(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(dir, 1000)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() { record(t, h, "c", "", fmt.Sprintf("SELECT %d", i)) })
	}
	wg.Wait()
	got, _ := OpenHistory(dir, 1000).List("c", HistoryFilter{AllDatabases: true})
	ids := map[int64]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if len(got) != 50 || len(ids) != 50 {
		t.Errorf("entries = %d, unique ids = %d", len(got), len(ids))
	}
}

func TestManagerRecordsQueries(t *testing.T) {
	m, conn, fake := newTestManager(t, Connection{Name: "pg", Driver: Postgres, Host: "h", User: "u", Database: "app"})
	id := conn.ID
	m.SetHistory(OpenHistory(t.TempDir(), 10))
	ctx := context.Background()
	if _, err := m.Query(ctx, id, "", "", "SELECT 1", 10); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.queryErr = errors.New("boom")
	fake.mu.Unlock()
	_, _ = m.Query(ctx, id, "", "", "SELECT broken", 10)

	got, err := m.QueryHistory(id, HistoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Error != "boom" || got[1].SQL != "SELECT 1" || got[1].Error != "" {
		t.Fatalf("history = %+v", got)
	}
	if got[0].Database != "app" {
		t.Errorf("database = %q, want the profile's resolved default", got[0].Database)
	}
}
