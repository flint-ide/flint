//go:build linux || darwin

package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A script step's sandbox, for real: the helper (main_test.go) runs as the
// script, from its worktree, confined by BuildScript's policy. Skipped
// where the system has no sandbox, as the extension tests are.

// scriptPlace is a worktree, a temporary and a cache folder, a folder of
// the user's that is neither, and a home with a key and Flint's folder in
// it, all in the test's temporary folder.
type scriptPlace struct {
	worktree, tmp, cache, outside, home, config string
	key, settings, secret                       string
	program                                     string
}

func newScriptPlace(t *testing.T) scriptPlace {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pl := scriptPlace{
		worktree: filepath.Join(base, "code", "app.worktrees", "fix"), tmp: filepath.Join(base, "tmp"),
		cache: filepath.Join(base, "cache"), outside: filepath.Join(base, "code", "app"),
		home: filepath.Join(base, "home"), config: filepath.Join(base, "home", ".config", "flint"),
	}
	for _, dir := range []string{pl.worktree, pl.tmp, pl.cache, pl.outside, pl.config, filepath.Join(pl.home, ".ssh")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pl.key = filepath.Join(pl.home, ".ssh", "id_ed25519")
	pl.settings = filepath.Join(pl.config, "settings.json")
	pl.secret = filepath.Join(pl.outside, "main.go")
	for file, text := range map[string]string{pl.key: "key", pl.settings: "{}", pl.secret: "package main"} {
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pl.program = filepath.Join(pl.worktree, "program")
	copyFile(t, self, pl.program)
	return pl
}

func (pl scriptPlace) policy(t *testing.T, network bool) Policy {
	t.Helper()
	p, err := BuildScript(ScriptRequest{
		GOOS: runtime.GOOS, Home: pl.home, ConfigDir: pl.config,
		Worktree: pl.worktree, TempDirs: []string{pl.tmp}, Writable: []string{pl.cache}, Network: network,
		Resolve: filepath.EvalSymlinks, ReadDir: DirNames,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) != 0 {
		t.Fatalf("notes: %v", p.Notes)
	}
	return p
}

func TestScriptSandbox(t *testing.T) {
	s := Probe()
	if !s.Available {
		t.Skipf("no sandbox on this system: %s", s.Reason)
	}
	pl := newScriptPlace(t)
	addr := listener(t)
	denied := "operation not permitted" // macOS
	if runtime.GOOS == "linux" {
		denied = "permission denied"
	}
	ops := []op{
		{"write", filepath.Join(pl.worktree, "out.txt")},                  // its worktree
		{"read", filepath.Join(pl.worktree, "out.txt")},                   //
		{"mkdir", filepath.Join(pl.tmp, "build")},                         // the temporary folder
		{"write", filepath.Join(pl.cache, "entry")},                       // a folder the user added
		{"read", pl.secret},                                               // the user's other files: read
		{"write", filepath.Join(pl.outside, "new.go")},                    // but not written
		{"write", pl.secret},                                              //
		{"write", filepath.Join(pl.home, "new")},                          //
		{"read", pl.key},                                                  // keys
		{"read", pl.settings},                                             // Flint's own folder
		{"write", "/dev/null"},                                            //
		{"tcp", addr},                                                     // no network
		{"sh", "echo built > " + filepath.Join(pl.worktree, "child.txt")}, // its children run, confined alike
		{"sh", "echo x > " + filepath.Join(pl.outside, "child.txt")},      //
	}
	results := tryAll(t, place{program: pl.program}, pl.policy(t, false), s, ops...)
	netDenied := denied
	if runtime.GOOS == "linux" && !s.seccomp && s.landlockABI < 4 {
		netDenied = "ok" // Support.Gaps: this kernel cannot close the network
	}
	expect(t, ops, results, "ok", "ok", "ok", "ok", "ok", denied, denied, denied, denied, denied, "ok", netDenied, "ok", "exit status")
	if results[4].Out != "package main" {
		t.Errorf("read %q", results[4].Out)
	}
	if data, _ := os.ReadFile(pl.secret); string(data) != "package main" {
		t.Errorf("the user's file changed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(pl.outside, "child.txt")); err == nil {
		t.Error("a child wrote outside the worktree")
	}

	on := []op{{"tcp", addr}, {"write", filepath.Join(pl.outside, "new.go")}}
	results = tryAll(t, place{program: pl.program}, pl.policy(t, true), s, on...)
	expect(t, on, results, "ok", denied)
}
