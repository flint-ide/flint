package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}

func TestListDir(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"beta", "Alpha", ".git", "zeta"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"b.txt", "A.txt", "c.go", ".DS_Store", ".env"} {
		mustWrite(t, filepath.Join(root, f), []byte("x"), 0o644)
	}
	if err := os.Symlink(filepath.Join(root, "zeta"), filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "c.go"), filepath.Join(root, "link-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}

	entries, err := ListDir(root)
	if err != nil {
		t.Fatalf("ListDir() error = %v", err)
	}
	want := []struct {
		name  string
		isDir bool
	}{
		{"Alpha", true}, {"beta", true}, {"link-dir", true}, {"zeta", true},
		{".env", false}, {"A.txt", false}, {"b.txt", false}, {"c.go", false},
		{"dangling", false}, {"link-file", false},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries %+v, want %d", len(entries), entries, len(want))
	}
	for i, w := range want {
		e := entries[i]
		if e.Name != w.name || e.IsDir != w.isDir {
			t.Errorf("entry[%d] = {%s dir=%v}, want {%s dir=%v}", i, e.Name, e.IsDir, w.name, w.isDir)
		}
		if e.Path != filepath.Join(root, e.Name) {
			t.Errorf("entry[%d].Path = %q", i, e.Path)
		}
	}
}

func TestListDirErrors(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantSub string
	}{
		{"relative path", "some/rel", "must be absolute"},
		{"missing dir", filepath.Join(t.TempDir(), "missing"), "list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ListDir(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("ListDir(%q) error = %v, want containing %q", tt.path, err, tt.wantSub)
			}
		})
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	bigText := strings.Repeat("ż", 5000) // 10000 bytes, valid UTF-8, longer than the 8000-byte sniff window
	tests := []struct {
		name       string
		data       []byte
		wantBinary bool
		wantText   string
	}{
		{"plain text", []byte("hello\nświat 😀\n"), false, "hello\nświat 😀\n"},
		{"empty file", []byte{}, false, ""},
		{"NUL byte", []byte("abc\x00def"), true, ""},
		{"invalid utf8", []byte{'a', 0xff, 0xfe, 'b'}, true, ""},
		{"large valid utf8", []byte(bigText), false, bigText},
		// Invalid bytes anywhere make the file binary: loading would corrupt it on save.
		{"invalid utf8 past head", append([]byte(strings.Repeat("a", 9000)), 0xff), true, ""},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "f"+string(rune('a'+i)))
			mustWrite(t, path, tt.data, 0o644)
			fc, err := ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if fc.Binary != tt.wantBinary {
				t.Errorf("Binary = %v, want %v", fc.Binary, tt.wantBinary)
			}
			if fc.Content != tt.wantText {
				t.Errorf("Content mismatch (len %d vs %d)", len(fc.Content), len(tt.wantText))
			}
			if fc.Size != int64(len(tt.data)) || fc.Path != path || fc.TooLarge {
				t.Errorf("metadata = %+v", fc)
			}
		})
	}
}

func TestReadFileTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.bin")
	mustWrite(t, path, nil, 0o644)
	size := int64(MaxFileSize + 1)
	if err := os.Truncate(path, size); err != nil { // sparse file: no real disk usage
		t.Fatal(err)
	}
	fc, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !fc.TooLarge || fc.Size != size || fc.Content != "" || fc.Binary {
		t.Errorf("got %+v, want TooLarge with size %d", fc, size)
	}
}

func TestReadFileExactlyMaxSizeIsLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "max.txt")
	mustWrite(t, path, []byte(strings.Repeat("a", MaxFileSize)), 0o644)
	fc, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if fc.TooLarge || len(fc.Content) != MaxFileSize {
		t.Errorf("TooLarge=%v len=%d, want loaded", fc.TooLarge, len(fc.Content))
	}
}

func TestReadFileErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		path    string
		wantSub string
	}{
		{"relative", "rel.txt", "must be absolute"},
		{"directory", dir, "is a directory"},
		{"missing", filepath.Join(dir, "missing.txt"), "stat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadFile(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("ReadFile(%q) error = %v, want containing %q", tt.path, err, tt.wantSub)
			}
		})
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("new file gets 0644", func(t *testing.T) {
		path := filepath.Join(dir, "new.txt")
		if err := WriteFile(path, "hello"); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		assertFile(t, path, "hello", 0o644)
	})
	t.Run("existing file keeps mode", func(t *testing.T) {
		path := filepath.Join(dir, "script.sh")
		mustWrite(t, path, []byte("old"), 0o600)
		if err := os.Chmod(path, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := WriteFile(path, "#!/bin/sh\n"); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		assertFile(t, path, "#!/bin/sh\n", 0o750)
	})
	t.Run("relative path rejected", func(t *testing.T) {
		if err := WriteFile("rel.txt", "x"); err == nil {
			t.Error("error = nil, want error")
		}
	})
	t.Run("stat error other than not-exist", func(t *testing.T) {
		blocker := filepath.Join(dir, "blocker")
		mustWrite(t, blocker, nil, 0o644)
		if err := WriteFile(filepath.Join(blocker, "child.txt"), "x"); err == nil {
			t.Error("error = nil, want error")
		}
	})
}

func assertFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != perm {
		t.Errorf("perm = %o, want %o", got, perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", data, content)
	}
}

func TestWriteFileFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	link := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(link, "new"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by a regular file (err=%v)", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("target = %q, want new", got)
	}
}

func TestWriteFileKeepsHardLinks(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(a, "new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(b); string(got) != "new" {
		t.Errorf("hard link sees %q, want new", got)
	}
}

func TestReadFileLargeInvalidUTF8IsBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cp1250.txt")
	data := append(bytes.Repeat([]byte("a"), 20_000), 0xB9, 0xE6) // "ąć" in Windows-1250
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	fc, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fc.Binary {
		t.Error("large non-UTF-8 file must not load as text (save would corrupt it)")
	}
}
