package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGit keeps the user's global/system git config (hooks, signing, aliases,
// pull/push defaults) out of the tests.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// mustGit runs git in dir and fails the test on error, returning trimmed stdout.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := tryGit(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// tryGit runs git in dir and returns combined output and the error.
func tryGit(dir string, args ...string) (string, error) {
	cmd := exec.Command(gitBinary(), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// configure sets identity and disables signing for the repository in dir.
func configure(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "config", "user.name", "Test User")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	mustGit(t, dir, "config", "tag.gpgsign", "false")
}

// initRepo creates an empty repository with branch main in dir.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	isolateGit(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "-q", "-b", "main", dir)
	configure(t, dir)
}

// openRepo opens dir as a Repo, failing the test on error.
func openRepo(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	return r
}

// newEmptyRepo returns a repository without commits.
func newEmptyRepo(t *testing.T) *Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	return openRepo(t, dir)
}

// newRepo returns a repository with one commit containing README.md.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	r := newEmptyRepo(t)
	writeFile(t, r.Root, "README.md", "hello\n")
	commitAll(t, r.Root, "initial")
	return r
}

// writeFile writes content to dir/rel, creating parent directories.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile returns the content of dir/rel.
func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fileExists reports whether dir/rel exists.
func fileExists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

// removeFile deletes dir/rel.
func removeFile(dir, rel string) error {
	return os.Remove(filepath.Join(dir, filepath.FromSlash(rel)))
}

// commitAll stages everything and commits, returning the full hash.
func commitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	return mustGit(t, dir, "rev-parse", "HEAD")
}

// commitFile writes rel and commits it, returning the full hash.
func commitFile(t *testing.T, dir, rel, content, msg string) string {
	t.Helper()
	writeFile(t, dir, rel, content)
	return commitAll(t, dir, msg)
}

// realPath resolves symlinks (macOS /var -> /private/var) for path comparisons.
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", p, err)
	}
	return r
}

// samePath fails the test unless got and want resolve to the same directory.
func samePath(t *testing.T, what, got, want string) {
	t.Helper()
	if realPath(t, got) != realPath(t, want) {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

// newBareRemote creates an empty bare repository and adds it as remote "origin" of r.
func newBareRemote(t *testing.T, r *Repo) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	mustGit(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", bare)
	mustGit(t, r.Root, "remote", "add", "origin", bare)
	return bare
}

// cloneRepo clones src into a new temp dir and returns the opened clone.
func cloneRepo(t *testing.T, src string) *Repo {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "clone")
	mustGit(t, t.TempDir(), "clone", "-q", src, dst)
	configure(t, dst)
	return openRepo(t, dst)
}

// statusFile finds path in the status, failing the test when missing.
func statusFile(t *testing.T, st Status, path string) FileChange {
	t.Helper()
	for _, f := range st.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("status has no entry for %q: %+v", path, st.Files)
	return FileChange{}
}

// currentBranch returns the checked-out branch name of dir.
func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	return mustGit(t, dir, "branch", "--show-current")
}
