package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeSecrets is an in-memory Secrets implementation with error injection.
type fakeSecrets struct {
	mu                     sync.Mutex
	m                      map[string]string
	getErr, setErr, delErr error
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{m: map[string]string{}} }

func (f *fakeSecrets) Get(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.m[id], nil
}

func (f *fakeSecrets) Set(id, pw string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.m[id] = pw
	return nil
}

func (f *fakeSecrets) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delErr != nil {
		return f.delErr
	}
	delete(f.m, id)
	return nil
}

func (f *fakeSecrets) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[id]
	return ok
}

func ptr(s string) *string { return &s }

func pgConn() Connection {
	return Connection{Driver: Postgres, Host: "localhost", Database: "app", User: "bob"}
}

func openTestStore(t *testing.T) (*Store, *fakeSecrets, string) {
	t.Helper()
	dir := t.TempDir()
	sec := newFakeSecrets()
	s, err := OpenStore(dir, sec)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	return s, sec, dir
}

func TestStoreSaveNewAssignsID(t *testing.T) {
	s, sec, _ := openTestStore(t)
	c, err := s.Save(pgConn(), ptr("s3cret"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(c.ID) != 16 {
		t.Errorf("ID = %q, want 16 hex chars", c.ID)
	}
	if c.Port != 5432 || c.Name != "app@localhost" {
		t.Errorf("Save did not normalize: %+v", c)
	}
	if pw, _ := s.Password(c.ID); pw != "s3cret" {
		t.Errorf("Password() = %q", pw)
	}
	if !sec.has(c.ID) {
		t.Error("secret not stored")
	}
	if got, ok := s.Get(c.ID); !ok || got != c {
		t.Errorf("Get() = %+v, %v", got, ok)
	}
	if _, ok := s.Get("missing"); ok {
		t.Error("Get(missing) ok = true")
	}
}

func TestStoreSaveUpdateKeepsIDAndSecret(t *testing.T) {
	s, _, _ := openTestStore(t)
	c, err := s.Save(pgConn(), ptr("first"))
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "renamed"
	updated, err := s.Save(c, nil) // nil password → keep existing
	if err != nil {
		t.Fatalf("Save(update) error = %v", err)
	}
	if updated.ID != c.ID || updated.Name != "renamed" {
		t.Errorf("updated = %+v", updated)
	}
	if n := len(s.List()); n != 1 {
		t.Errorf("List() has %d items, want 1", n)
	}
	if pw, _ := s.Password(c.ID); pw != "first" {
		t.Errorf("password changed to %q with nil password", pw)
	}
	if _, err := s.Save(c, ptr("second")); err != nil {
		t.Fatal(err)
	}
	if pw, _ := s.Password(c.ID); pw != "second" {
		t.Errorf("password = %q, want second", pw)
	}
}

func TestStoreSaveErrors(t *testing.T) {
	t.Run("invalid profile", func(t *testing.T) {
		s, sec, _ := openTestStore(t)
		bad := pgConn()
		bad.User = ""
		if _, err := s.Save(bad, ptr("x")); err == nil || !strings.Contains(err.Error(), "user is required") {
			t.Errorf("error = %v", err)
		}
		if len(s.List()) != 0 || len(sec.m) != 0 {
			t.Error("invalid profile left state behind")
		}
	})
	t.Run("keychain failure", func(t *testing.T) {
		s, sec, _ := openTestStore(t)
		sec.setErr = errors.New("locked")
		if _, err := s.Save(pgConn(), ptr("x")); err == nil || !strings.Contains(err.Error(), "keychain") {
			t.Errorf("error = %v", err)
		}
		if len(s.List()) != 0 {
			t.Error("profile saved despite keychain failure")
		}
	})
	t.Run("password read failure", func(t *testing.T) {
		s, sec, _ := openTestStore(t)
		sec.getErr = errors.New("denied")
		if _, err := s.Password("x"); err == nil || !errors.Is(err, sec.getErr) {
			t.Errorf("error = %v, want wrapped denied", err)
		}
	})
}

func TestStoreDelete(t *testing.T) {
	s, sec, _ := openTestStore(t)
	a, _ := s.Save(pgConn(), ptr("a"))
	b, _ := s.Save(pgConn(), ptr("b"))

	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if sec.has(a.ID) {
		t.Error("secret not removed")
	}
	if !sec.has(b.ID) {
		t.Error("other secret removed")
	}
	if list := s.List(); len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("List() = %+v", list)
	}
	if err := s.Delete("unknown"); err != nil {
		t.Errorf("Delete(unknown) = %v, want nil", err)
	}

	sec.delErr = errors.New("locked")
	if err := s.Delete(b.ID); err == nil {
		t.Error("Delete with keychain failure: error = nil")
	}
	if len(s.List()) != 0 {
		t.Error("profile should be removed even if keychain delete fails")
	}
}

func TestStorePersistence(t *testing.T) {
	s, sec, dir := openTestStore(t)
	a, _ := s.Save(pgConn(), ptr("pw-a"))
	ch := Connection{Driver: ClickHouse, Host: "ch", User: "default", TLS: true, ReadOnly: true}
	b, err := s.Save(ch, nil)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "pw-a") {
		t.Error("password leaked into connections.json")
	}

	reopened, err := OpenStore(dir, sec)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	list := reopened.List()
	if len(list) != 2 || list[0] != a || list[1] != b {
		t.Errorf("reloaded = %+v, want [%+v %+v]", list, a, b)
	}

	// List returns a copy.
	list[0].Name = "mutated"
	if got, _ := reopened.Get(a.ID); got.Name == "mutated" {
		t.Error("List() exposed internal slice")
	}
}

func TestOpenStoreCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "connections.json"), []byte("[{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir, newFakeSecrets()); err == nil {
		t.Error("OpenStore(corrupt) error = nil")
	}
}

func TestListNeverNil(t *testing.T) {
	s, err := OpenStore(t.TempDir(), newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.List(); got == nil {
		t.Error("List() = nil, want empty slice (JSON [] for the frontend)")
	}
}
