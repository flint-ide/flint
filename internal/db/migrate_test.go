package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const legacyConnections = `[
 {"id":"a","name":"native default","driver":"clickhouse","host":"ch1","port":9000,"database":"logs","user":"u","protocol":"native","tls":false,"tlsInsecure":false,"readOnly":true},
 {"id":"b","name":"native tls","driver":"clickhouse","host":"ch2","port":9440,"database":"","user":"u","protocol":"native","tls":true,"tlsInsecure":true,"readOnly":false},
 {"id":"c","name":"custom","driver":"clickhouse","host":"ch3","port":19000,"database":"","user":"u","protocol":"native","tls":false,"tlsInsecure":false,"readOnly":false},
 {"id":"d","name":"already http","driver":"clickhouse","host":"ch4","port":18123,"database":"","user":"u","protocol":"http","tls":false,"tlsInsecure":false,"readOnly":false},
 {"id":"e","name":"no protocol field","driver":"clickhouse","host":"ch5","port":0,"database":"","user":"u","tls":false,"tlsInsecure":false,"readOnly":false},
 {"id":"f","name":"pg","driver":"postgres","host":"pg","port":5432,"database":"app","user":"u","protocol":"","tls":false,"tlsInsecure":false,"readOnly":false}
]`

func TestNativeConnectionsMigrateToHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte(legacyConnections), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir, newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	ports := map[string]int{}
	for _, c := range store.List() {
		ports[c.ID] = c.Port
	}
	want := map[string]int{"a": 8123, "b": 8443, "c": 8123, "d": 18123, "e": 8123, "f": 5432}
	for id, port := range want {
		if ports[id] != port {
			t.Errorf("connection %s port = %d, want %d", id, ports[id], port)
		}
	}

	// Nothing else about the profiles changes.
	a, _ := store.Get("a")
	if a.Name != "native default" || a.Host != "ch1" || a.Database != "logs" || !a.ReadOnly || a.User != "u" {
		t.Errorf("profile a = %+v", a)
	}
	if b, _ := store.Get("b"); !b.TLS || !b.TLSInsecure {
		t.Errorf("TLS options lost: %+v", b)
	}

	// The saved file no longer mentions the protocol, and the original is kept.
	saved, _ := os.ReadFile(path)
	if strings.Contains(string(saved), `"protocol"`) || !json.Valid(saved) {
		t.Errorf("saved file = %s", saved)
	}
	backup, err := os.ReadFile(filepath.Join(dir, nativeBackupFile))
	if err != nil || string(backup) != legacyConnections {
		t.Errorf("backup = %q, %v", backup, err)
	}

	// The notice names the moved connections and the custom port, and is one-time.
	notice := store.TakeMigrationNotice()
	for _, want := range []string{"native default: port 9000 → 8123", "native tls: port 9440 → 8443", "custom native port 19000 → 8123", nativeBackupFile} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice lacks %q:\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "already http") || strings.Contains(notice, "pg") {
		t.Errorf("notice lists untouched profiles:\n%s", notice)
	}
	if again := store.TakeMigrationNotice(); again != "" {
		t.Errorf("notice shown twice: %q", again)
	}
}

func TestMigrationRunsOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "connections.json"), []byte(legacyConnections), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir, newFakeSecrets()); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, nativeBackupFile))
	store, err := OpenStore(dir, newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := store.Get("a"); c.Port != 8123 {
		t.Errorf("second open changed the port again: %d", c.Port)
	}
	if second, _ := os.ReadFile(filepath.Join(dir, nativeBackupFile)); string(second) != string(first) {
		t.Error("backup was overwritten by the second open")
	}
	if notice := store.TakeMigrationNotice(); notice == "" {
		t.Error("the notice written at migration time must survive until the UI takes it")
	}
}

func TestNoMigrationWithoutNativeProfiles(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir, newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Connection{Driver: ClickHouse, Host: "h", User: "u"}, nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir, newFakeSecrets())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 1 || got[0].Port != 8123 {
		t.Errorf("list = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, nativeBackupFile)); err == nil {
		t.Error("a backup was written although nothing was migrated")
	}
	if reopened.TakeMigrationNotice() != "" {
		t.Error("notice without a migration")
	}
}
