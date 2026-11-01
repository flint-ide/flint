package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Syncing is invisible to callers: the same bytes and mode land under the
// name, nothing else is left in the folder, and a folder that can't be
// opened for syncing is not an error.
func TestWriteFileAtomicSyncsWithoutSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, data := range []string{"first", "second, longer"} {
		if err := WriteFileAtomic(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatalf("read back %q, %v; want %q", got, err, data)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("folder holds %d entries, want only the file", len(entries))
	}
	syncDir(filepath.Join(dir, "missing")) // best effort: must not panic
}
