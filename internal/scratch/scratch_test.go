package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return s, dir
}

func TestCreateKinds(t *testing.T) {
	tests := []struct {
		kind Kind
		ext  string
	}{
		{SQL, ".sql"},
		{Markdown, ".md"},
		{Text, ".txt"},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			s, dir := openStore(t)
			before := time.Now().UnixMilli()
			sc, err := s.Create("  My pad  ", tt.kind, "conn-1", "")
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if sc.Name != "My pad" || sc.Kind != tt.kind || sc.ConnID != "conn-1" || len(sc.ID) != 16 {
				t.Errorf("created = %+v", sc)
			}
			if sc.UpdatedAt < before {
				t.Errorf("UpdatedAt = %d, want >= %d", sc.UpdatedAt, before)
			}
			path := filepath.Join(dir, "scratch", sc.ID+tt.ext)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("content file missing: %v", err)
			}
			if info.Size() != 0 || info.Mode().Perm() != 0o600 {
				t.Errorf("content file size=%d perm=%o", info.Size(), info.Mode().Perm())
			}
		})
	}
}

func TestCreateUnknownKind(t *testing.T) {
	s, _ := openStore(t)
	if _, err := s.Create("x", "python", "", ""); err == nil || !strings.Contains(err.Error(), "unknown scratch kind") {
		t.Errorf("error = %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("unknown kind was stored")
	}
}

func TestCreateDefaultName(t *testing.T) {
	s, _ := openStore(t)
	for _, name := range []string{"", "   \t"} {
		sc, err := s.Create(name, Text, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(sc.Name, "scratch ") {
			t.Errorf("Create(%q).Name = %q, want default", name, sc.Name)
		}
		if _, err := time.Parse("2006-01-02 15:04", strings.TrimPrefix(sc.Name, "scratch ")); err != nil {
			t.Errorf("default name %q has no timestamp: %v", sc.Name, err)
		}
	}
}

func TestContentRoundtrip(t *testing.T) {
	s, _ := openStore(t)
	sc, _ := s.Create("q", SQL, "", "")

	got, err := s.Content(sc.ID)
	if err != nil || got != "" {
		t.Fatalf("Content(new) = %q, %v", got, err)
	}

	text := "SELECT 'zażółć 😀';\n"
	time.Sleep(2 * time.Millisecond)
	if err := s.SaveContent(sc.ID, text); err != nil {
		t.Fatalf("SaveContent() error = %v", err)
	}
	if got, err = s.Content(sc.ID); err != nil || got != text {
		t.Errorf("Content() = %q, %v; want %q", got, err, text)
	}
	if after := s.List()[0]; after.UpdatedAt <= sc.UpdatedAt {
		t.Errorf("SaveContent did not bump UpdatedAt: %d <= %d", after.UpdatedAt, sc.UpdatedAt)
	}
}

func TestContentMissingFileIsEmpty(t *testing.T) {
	s, dir := openStore(t)
	sc, _ := s.Create("q", Markdown, "", "")
	if err := os.Remove(filepath.Join(dir, "scratch", sc.ID+".md")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Content(sc.ID); err != nil || got != "" {
		t.Errorf("Content() = %q, %v; want empty, nil", got, err)
	}
}

func TestUnknownID(t *testing.T) {
	s, _ := openStore(t)
	checks := map[string]error{}
	_, checks["Content"] = s.Content("nope")
	checks["SaveContent"] = s.SaveContent("nope", "x")
	checks["Rename"] = s.Rename("nope", "x")
	checks["SetConnection"] = s.SetConnection("nope", "c", "")
	for op, err := range checks {
		if err == nil || !strings.Contains(err.Error(), "unknown scratch") {
			t.Errorf("%s(unknown) error = %v", op, err)
		}
	}
	if err := s.Delete("nope"); err != nil {
		t.Errorf("Delete(unknown) = %v, want nil", err)
	}
}

func TestListNewestFirst(t *testing.T) {
	s, _ := openStore(t)
	a, _ := s.Create("a", Text, "", "")
	time.Sleep(2 * time.Millisecond)
	b, _ := s.Create("b", Text, "", "")
	time.Sleep(2 * time.Millisecond)
	c, _ := s.Create("c", Text, "", "")

	assertOrder(t, s.List(), c.ID, b.ID, a.ID)

	time.Sleep(2 * time.Millisecond)
	if err := s.SaveContent(a.ID, "touched"); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, s.List(), a.ID, c.ID, b.ID)
}

func assertOrder(t *testing.T, list []Scratch, ids ...string) {
	t.Helper()
	if len(list) != len(ids) {
		t.Fatalf("List() has %d items, want %d", len(list), len(ids))
	}
	for i, id := range ids {
		if list[i].ID != id {
			t.Errorf("List()[%d] = %s (%s), want %s", i, list[i].ID, list[i].Name, id)
		}
	}
}

func TestRename(t *testing.T) {
	s, _ := openStore(t)
	sc, _ := s.Create("old", Text, "", "")
	for _, name := range []string{"", "  "} {
		if err := s.Rename(sc.ID, name); err == nil || !strings.Contains(err.Error(), "must not be empty") {
			t.Errorf("Rename(%q) error = %v", name, err)
		}
	}
	if err := s.Rename(sc.ID, "  new name "); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if got := s.List()[0].Name; got != "new name" {
		t.Errorf("Name = %q, want %q", got, "new name")
	}
}

func TestSetConnection(t *testing.T) {
	s, dir := openStore(t)
	sc, _ := s.Create("q", SQL, "", "")
	if err := s.SetConnection(sc.ID, "pg-prod", "billing"); err != nil {
		t.Fatalf("SetConnection() error = %v", err)
	}
	if got := s.List()[0]; got.ConnID != "pg-prod" || got.Database != "billing" {
		t.Errorf("ConnID/Database = %q/%q", got.ConnID, got.Database)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List()[0]; got.ConnID != "pg-prod" || got.Database != "billing" {
		t.Errorf("after reload ConnID/Database = %q/%q, want the choice remembered", got.ConnID, got.Database)
	}
	if err := s.SetConnection(sc.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.List()[0]; got.ConnID != "" || got.Database != "" {
		t.Errorf("ConnID/Database = %q/%q, want cleared", got.ConnID, got.Database)
	}
}

func TestCreateWithDatabase(t *testing.T) {
	s, _ := openStore(t)
	sc, err := s.Create("q", SQL, "pg-prod", " billing ")
	if err != nil {
		t.Fatal(err)
	}
	if sc.ConnID != "pg-prod" || sc.Database != "billing" {
		t.Errorf("created = %+v", sc)
	}
}

func TestDelete(t *testing.T) {
	s, dir := openStore(t)
	keep, _ := s.Create("keep", Text, "", "")
	gone, _ := s.Create("gone", SQL, "", "")
	path := filepath.Join(dir, "scratch", gone.ID+".sql")

	if err := s.Delete(gone.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("content file still exists: %v", err)
	}
	if list := s.List(); len(list) != 1 || list[0].ID != keep.ID {
		t.Errorf("List() = %+v", list)
	}
	if _, err := s.Content(gone.ID); err == nil {
		t.Error("Content(deleted) error = nil")
	}

	// Deleting when the content file is already gone is fine.
	if err := os.Remove(filepath.Join(dir, "scratch", keep.ID+".txt")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(keep.ID); err != nil {
		t.Errorf("Delete(missing file) = %v", err)
	}
}

func TestReloadFromDisk(t *testing.T) {
	s, dir := openStore(t)
	a, _ := s.Create("a", SQL, "c1", "")
	time.Sleep(2 * time.Millisecond)
	b, _ := s.Create("b", Markdown, "", "")
	if err := s.SaveContent(a.ID, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(b.ID, "renamed"); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	want, got := s.List(), reopened.List()
	if len(got) != 2 {
		t.Fatalf("reloaded %d items", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if content, _ := reopened.Content(a.ID); content != "SELECT 1" {
		t.Errorf("reloaded content = %q", content)
	}
}

func TestOpenCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch", "index.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Error("Open(corrupt) error = nil")
	}
}

func TestListNeverNil(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.List(); got == nil {
		t.Error("List() = nil, want empty slice (JSON [] for the frontend)")
	}
}
