//go:build darwin

package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Puts a real file into the user's Trash, so it only runs on request.
func TestMoveToTrash(t *testing.T) {
	if os.Getenv("FLINT_TEST_TRASH") == "" {
		t.Skip("set FLINT_TEST_TRASH=1 to move a temp file to the real Trash")
	}
	path := filepath.Join(t.TempDir(), "flint-trash-test.txt")
	mustWrite(t, path, []byte("x"), 0o644)
	if err := MoveToTrash(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still there: %v", err)
	}
	if err := MoveToTrash(path); err == nil {
		t.Fatal("trashing a missing file should fail")
	}
}
