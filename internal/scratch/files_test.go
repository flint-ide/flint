package scratch

import (
	"os"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateFileNumbersDuplicates(t *testing.T) {
	s := openTemp(t)
	a, err := s.CreateFile("requests.http", "GET https://example.com\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateFile("requests.http", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "requests.http" || b.Name != "requests-2.http" {
		t.Fatalf("names: %q %q", a.Name, b.Name)
	}
	data, err := os.ReadFile(a.Path)
	if err != nil || string(data) != "GET https://example.com\n" {
		t.Fatalf("content: %q %v", data, err)
	}
	info, err := os.Stat(a.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info.Mode(), err)
	}
	files, err := s.Files()
	if err != nil || len(files) != 2 {
		t.Fatalf("files: %v %v", files, err)
	}
}

func TestCreateFileRejectsBadNames(t *testing.T) {
	s := openTemp(t)
	for _, name := range []string{"", "  ", "../x.http", "a/b.json", `a\b`, ".env"} {
		if _, err := s.CreateFile(name, ""); err == nil {
			t.Errorf("%q: expected an error", name)
		}
	}
}

func TestRenameAndDeleteStayInFolder(t *testing.T) {
	s := openTemp(t)
	f, err := s.CreateFile("a.json", "{}")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateFile("b.json", "")
	if _, err := s.RenameFile(f.Path, "b.json"); err == nil {
		t.Error("rename onto an existing file must fail")
	}
	renamed, err := s.RenameFile(f.Path, "c.yaml")
	if err != nil || renamed.Name != "c.yaml" {
		t.Fatalf("rename: %v %v", renamed, err)
	}
	outside := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFile(outside); err == nil {
		t.Error("deleting a file outside the scratch folder must fail")
	}
	if _, err := s.RenameFile(outside, "y.txt"); err == nil {
		t.Error("renaming a file outside the scratch folder must fail")
	}
	if err := s.DeleteFile(other.Path); err != nil {
		t.Fatal(err)
	}
	files, _ := s.Files()
	if len(files) != 1 || files[0].Name != "c.yaml" {
		t.Fatalf("files after delete: %v", files)
	}
}

func TestFilesEmptyWithoutFolder(t *testing.T) {
	files, err := openTemp(t).Files()
	if err != nil || files == nil || len(files) != 0 {
		t.Fatalf("got %v %v", files, err)
	}
}
