package acpdriver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfine(t *testing.T) {
	cwd := t.TempDir()
	h := &handler{s: &session{id: "s1", cwd: cwd}}
	inside := filepath.Join(cwd, "a.go")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.confine("s1", inside); err != nil {
		t.Errorf("inside path rejected: %v", err)
	}
	for _, bad := range []string{"/etc/hosts", filepath.Join(cwd, "..", "x"), "rel/a.go"} {
		if _, err := h.confine("s1", bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if _, err := h.confine("other", inside); err == nil {
		t.Error("foreign session id must be rejected")
	}
}
