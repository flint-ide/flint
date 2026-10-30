package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDir(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		want := t.TempDir()
		t.Setenv("FLINT_CONFIG_DIR", want)
		got, err := Dir()
		if err != nil {
			t.Fatalf("Dir() error = %v", err)
		}
		if got != want {
			t.Errorf("Dir() = %q, want %q", got, want)
		}
	})
	t.Run("default under user config dir", func(t *testing.T) {
		t.Setenv("FLINT_CONFIG_DIR", "")
		got, err := Dir()
		if err != nil {
			t.Fatalf("Dir() error = %v", err)
		}
		if filepath.Base(got) != appDirName {
			t.Errorf("Dir() = %q, want suffix %q", got, appDirName)
		}
		base, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("UserConfigDir: %v", err)
		}
		if want := filepath.Join(base, appDirName); got != want {
			t.Errorf("Dir() = %q, want %q", got, want)
		}
	})
}

type doc struct {
	Name  string         `json:"name"`
	Count int            `json:"count"`
	Tags  []string       `json:"tags"`
	Extra map[string]int `json:"extra"`
}

func TestLoadJSONMissingFileLeavesValueUntouched(t *testing.T) {
	v := doc{Name: "keep"}
	if err := LoadJSON(filepath.Join(t.TempDir(), "nope.json"), &v); err != nil {
		t.Fatalf("LoadJSON() error = %v, want nil", err)
	}
	if v.Name != "keep" {
		t.Errorf("value mutated: %+v", v)
	}
}

func TestLoadJSONErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		path    string
		wantSub string
	}{
		{"invalid json", bad, "decode"},
		{"path is directory", dir, "read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v doc
			err := LoadJSON(tt.path, &v)
			if err == nil {
				t.Fatal("LoadJSON() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err, tt.wantSub)
			}
		})
	}
}

func TestSaveLoadJSONRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "doc.json")
	in := doc{Name: "zażółć", Count: 42, Tags: []string{"a", "b"}, Extra: map[string]int{"x": 1}}
	if err := SaveJSON(path, in); err != nil {
		t.Fatalf("SaveJSON() error = %v", err)
	}
	var out doc
	if err := LoadJSON(path, &out); err != nil {
		t.Fatalf("LoadJSON() error = %v", err)
	}
	if out.Name != in.Name || out.Count != in.Count || len(out.Tags) != 2 || out.Extra["x"] != 1 {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", out, in)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("SaveJSON perm = %o, want 600", perm)
	}
}

func TestSaveJSONUnencodable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.json")
	if err := SaveJSON(path, make(chan int)); err == nil {
		t.Fatal("SaveJSON(chan) error = nil, want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not exist after failed encode, stat err = %v", err)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	tests := []struct {
		name string
		perm os.FileMode
	}{
		{"owner only", 0o600},
		{"world readable", 0o644},
		{"executable", 0o755},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f.txt")
			if err := WriteFileAtomic(path, []byte("hello"), tt.perm); err != nil {
				t.Fatalf("WriteFileAtomic() error = %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != tt.perm {
				t.Errorf("perm = %o, want %o", got, tt.perm)
			}
			data, _ := os.ReadFile(path)
			if string(data) != "hello" {
				t.Errorf("content = %q", data)
			}
			assertNoTempFiles(t, dir)
		})
	}
}

func TestWriteFileAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old content that is longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", data, "new")
	}
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomicErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("parent is a file", func(t *testing.T) {
		if err := WriteFileAtomic(filepath.Join(blocker, "child"), []byte("x"), 0o600); err == nil {
			t.Error("error = nil, want error")
		}
	})
	t.Run("target is a directory", func(t *testing.T) {
		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(filepath.Join(sub, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := WriteFileAtomic(sub, []byte("x"), 0o600); err == nil {
			t.Error("error = nil, want error replacing non-empty directory")
		}
		assertNoTempFiles(t, dir)
	})
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".flint-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Errorf("leftover temp files: %v", matches)
	}
}
