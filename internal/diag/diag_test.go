package diag

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flint.log")
	r, err := openRotating(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := []byte(strings.Repeat("x", 1<<20) + "\n")
	for range 6 {
		if _, err := r.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	info, _ := os.Stat(path)
	old, err := os.Stat(path + ".1")
	if err != nil || info.Size() >= maxLogBytes || old.Size() > maxLogBytes {
		t.Fatalf("rotation: cur %d old %v err %v", info.Size(), old, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", info.Mode().Perm())
	}
}

func TestIssueLinkFitsAURL(t *testing.T) {
	link := IssueLink("Bug: x", strings.Repeat("line\n", 5000))
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	body := u.Query().Get("body")
	if len(body) > maxURLBody+200 || !strings.Contains(body, "clipboard") || u.Query().Get("labels") != "bug" {
		t.Fatalf("body %d bytes", len(body))
	}
}
