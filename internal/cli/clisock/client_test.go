package clisock

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder whose name looks like a quoted command line still opens.
func TestJoinedKeepsRealFolders(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if got := joined([]string{"workflow list"}); got == "" {
		t.Fatal("a quoted command line was not told")
	}
	if err := os.Mkdir(filepath.Join(dir, "workflow list"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := joined([]string{"workflow list"}); got != "" {
		t.Errorf("an existing folder is refused: %q", got)
	}
	if got := joined([]string{filepath.Join(dir, "workflow list")}); got != "" {
		t.Errorf("an absolute folder is refused: %q", got)
	}
}
