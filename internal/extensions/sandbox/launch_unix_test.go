//go:build linux || darwin

package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// What the real sandbox tests of both systems share: an extension's folders
// in a temporary place, and running the helper (main_test.go) confined.

// place is a set of folders like an installed extension's.
type place struct {
	ext, data, tmp, outside string
	program                 string
	secret                  string
}

func newPlace(t *testing.T) place {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pl := place{ext: filepath.Join(base, "config", "extensions", "test.ext"), data: filepath.Join(base, "config", "extension-data", "test.ext"), tmp: filepath.Join(base, "tmp"), outside: filepath.Join(base, "outside")}
	for _, dir := range []string{pl.ext, pl.data, pl.tmp, pl.outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The program is this test binary, copied into the extension's folder.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pl.program = filepath.Join(pl.ext, "program")
	copyFile(t, self, pl.program)
	pl.secret = filepath.Join(pl.outside, "secret")
	for file, text := range map[string]string{pl.secret: "secret", filepath.Join(pl.ext, "own.txt"): "own"} {
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return pl
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func (pl place) request(change func(r *Request)) Request {
	env := map[string]string{"PATH": os.Getenv("PATH")}
	r := Request{
		GOOS: runtime.GOOS, Home: filepath.Join(filepath.Dir(pl.outside), "home"), ConfigDir: filepath.Dir(filepath.Dir(pl.ext)),
		ExtensionDir: pl.ext, DataDir: pl.data, TempDir: pl.tmp,
		Interpreters: Interpreters(pl.program, os.Getenv("PATH")),
		Env:          func(name string) string { return env[name] },
		Resolve:      filepath.EvalSymlinks,
	}
	if change != nil {
		change(&r)
	}
	return r
}

// start runs argv through the launcher, confined by p as s allows.
func start(t *testing.T, p Policy, s Support, argv []string, env ...string) ([]byte, error) {
	t.Helper()
	launcher, err := Launcher()
	if err != nil {
		t.Fatal(err)
	}
	args, more, err := Command(launcher, p, s, argv)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(append([]string{"PATH=" + os.Getenv("PATH")}, env...), more...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out, err
}

// tryAll runs the helper confined and returns what each op did.
func tryAll(t *testing.T, pl place, p Policy, s Support, ops ...op) []result {
	t.Helper()
	raw, _ := json.Marshal(ops)
	out, err := start(t, p, s, []string{pl.program}, helperEnv+"="+string(raw))
	if err != nil {
		t.Fatalf("the confined program did not run: %v", err)
	}
	var results []result
	if err := json.Unmarshal(out, &results); err != nil || len(results) != len(ops) {
		t.Fatalf("helper output %q: %v", out, err)
	}
	return results
}

// expect checks each result against "ok" or a part of the expected error.
func expect(t *testing.T, ops []op, results []result, want ...string) {
	t.Helper()
	for i, r := range results {
		switch {
		case want[i] == "ok" && r.Err != "":
			t.Errorf("%s %s: %s, want it to work", ops[i].Kind, ops[i].Arg, r.Err)
		case want[i] != "ok" && r.Err == "":
			t.Errorf("%s %s worked (%q), want an error with %q", ops[i].Kind, ops[i].Arg, r.Out, want[i])
		case want[i] != "ok" && !strings.Contains(r.Err, want[i]):
			t.Errorf("%s %s: %s, want an error with %q", ops[i].Kind, ops[i].Arg, r.Err, want[i])
		}
	}
}

// listener accepts TCP connections on the loopback until the test ends.
func listener(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l.Addr().String()
}
