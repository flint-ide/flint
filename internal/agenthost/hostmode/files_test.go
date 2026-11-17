package hostmode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shortDir is a temp dir short enough for a socket path on every platform.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "fh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestPrepareDirCreatesItPrivate(t *testing.T) {
	dir := filepath.Join(shortDir(t), "a", "run")
	if err := prepareDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Errorf("dir = %v, %v", fi, err)
	}
}

func TestPrepareDirTightensAnExistingOneOfOurs(t *testing.T) {
	dir := filepath.Join(shortDir(t), "run")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareDir(dir); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestPrepareDirRefusesASymlinkAndAFile(t *testing.T) {
	root := shortDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, file} {
		if err := prepareDir(p); err == nil {
			t.Errorf("prepareDir(%s) accepted it", filepath.Base(p))
		}
	}
}

func TestTokenIsFreshPrivateAndHex(t *testing.T) {
	path := filepath.Join(shortDir(t), "host.token")
	a, err := writeToken(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file = %v, %v", fi, err)
	}
	disk, _ := os.ReadFile(path)
	if string(disk) != a {
		t.Error("the file does not hold the returned token")
	}
	if len(a) != 64 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("token %q is not 256 bits of hex", a)
	}
	b, err := writeToken(path) // a stale one from an earlier host is replaced
	if err != nil || b == a {
		t.Errorf("second token = %q, %v; want a fresh one", b, err)
	}
}

// A planted symlink must not make the host write its token somewhere else, or
// leave a token another user can read.
func TestTokenDoesNotFollowAPlantedSymlink(t *testing.T) {
	dir := shortDir(t)
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "host.token")
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if _, err := writeToken(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "precious" {
		t.Errorf("the symlink target was overwritten: %q", b)
	}
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Errorf("token file = %v, %v", fi, err)
	}
}

func TestTokenRefusesAPlantedDirectory(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "host.token")
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := writeToken(path); err == nil {
		t.Error("a non-empty directory in the token's place was accepted")
	}
}

func TestReadTokenChecksTheFile(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "host.token")
	if _, err := readToken(path); !errors.Is(err, ErrNotRunning) {
		t.Errorf("missing token = %v, want ErrNotRunning", err)
	}
	want, err := writeToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readToken(path); err != nil || got != want {
		t.Errorf("readToken = %q, %v", got, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err == nil || errors.Is(err, ErrNotRunning) {
		t.Errorf("a world-readable token was accepted: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err == nil {
		t.Error("an oversized token file was accepted")
	}
}

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(shortDir(t), "host.lock")
	first, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("lock mode = %v", fi.Mode().Perm())
	}
	if _, err := acquireLock(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second lock = %v, want ErrAlreadyRunning", err)
	}
	first.release()
	again, err := acquireLock(path)
	if err != nil {
		t.Fatalf("lock after release = %v", err)
	}
	again.release()
}

func TestRemoveStaleSocketOnlyRemovesSockets(t *testing.T) {
	dir := shortDir(t)
	sock := filepath.Join(dir, "host.sock")
	if err := os.WriteFile(sock, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A regular file is not ours to delete silently: say so.
	if err := removeStaleSocket(sock); err == nil {
		t.Error("a regular file was removed as a stale socket")
	}
	_ = os.Remove(sock)
	if err := removeStaleSocket(sock); err != nil {
		t.Errorf("nothing to remove = %v", err)
	}
}
