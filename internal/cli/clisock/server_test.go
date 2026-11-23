//go:build darwin || linux

package clisock

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/agenthost/hostmode"
	"flint/internal/cli"
)

// shortDir is a run directory whose socket path fits the kernel's limit
// (t.TempDir is too long on macOS).
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "fcli")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return filepath.Join(d, "run")
}

// testServer serves a few commands: task list (reads), task new (acts,
// echoes its description and folder) and run wait (blocks until its
// context ends, then reports on ended).
type testServer struct {
	srv    *Server
	paths  Paths
	caller atomic.Pointer[cli.Caller]
	pid    atomic.Int64
	ended  chan struct{}
	waits  chan struct{}
}

func startTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{paths: PathsIn(shortDir(t)), ended: make(chan struct{}, 1), waits: make(chan struct{}, 1)}
	ts.caller.Store(&cli.Caller{})
	reg, err := cli.NewRegistry([]cli.Command{
		{Path: "task list", Summary: "x", Kind: cli.Reads, Run: func(context.Context, *cli.Call) (cli.Result, error) {
			return cli.Result{Text: "one task", JSON: []string{"t1"}}, nil
		}},
		{
			Path: "task new", Summary: "x", Kind: cli.Acts, Folder: true,
			Flags: []cli.Flag{{Name: "description", Value: "<text>", Required: true, File: true, Max: 100}},
			Run: func(_ context.Context, c *cli.Call) (cli.Result, error) {
				return cli.Result{Text: c.String("description") + " in " + c.Folder}, nil
			},
		},
		{Path: "run wait", Summary: "x", Kind: cli.Reads, Run: func(ctx context.Context, _ *cli.Call) (cli.Result, error) {
			ts.waits <- struct{}{}
			<-ctx.Done()
			ts.ended <- struct{}{}
			return cli.Result{}, ctx.Err()
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts.srv, err = Start(Options{Paths: ts.paths, Registry: reg, Classify: func(pid int) cli.Caller {
		ts.pid.Store(int64(pid))
		return *ts.caller.Load()
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *testServer) run(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := RunIn(t.Context(), ts.paths, args, "/work/proj", strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

func TestSocketRoundTrip(t *testing.T) {
	ts := startTestServer(t)
	if out, errs, code := ts.run(t, "", "task", "list"); code != 0 || out != "one task\n" || errs != "" {
		t.Fatalf("task list = %d %q %q", code, out, errs)
	}
	if out, _, code := ts.run(t, "", "task", "list", "--json"); code != 0 || !strings.Contains(out, `"t1"`) {
		t.Fatalf("task list --json = %d %q", code, out)
	}
	// A command that acts asks who connected: this test process.
	if out, _, code := ts.run(t, "", "task", "new", "--description", "fix it"); code != 0 || out != "fix it in /work/proj\n" {
		t.Fatalf("task new = %d %q", code, out)
	}
	if got := ts.pid.Load(); got != int64(os.Getpid()) {
		t.Errorf("Classify got pid %d, want %d", got, os.Getpid())
	}
	if _, errs, code := ts.run(t, "", "task", "new"); code != cli.ExitUsage || !strings.Contains(errs, "--description or --description-file is required") {
		t.Errorf("usage = %d %q", code, errs)
	}
}

func TestSocketRefusesAnAgent(t *testing.T) {
	ts := startTestServer(t)
	ts.caller.Store(&cli.Caller{Agent: true, What: "claude"})
	_, errs, code := ts.run(t, "", "task", "new", "--description", "x")
	if code != cli.ExitFailed || !strings.Contains(errs, "this command was run by an agent (claude)") {
		t.Fatalf("task new by an agent = %d %q", code, errs)
	}
	if out, _, code := ts.run(t, "", "task", "list"); code != 0 || out == "" {
		t.Errorf("an agent's read = %d %q", code, out)
	}
}

func TestSocketFilesAreReadByTheClient(t *testing.T) {
	ts := startTestServer(t)
	file := filepath.Join(t.TempDir(), "desc.md")
	if err := os.WriteFile(file, []byte("from a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _, code := ts.run(t, "", "task", "new", "--description-file", file); code != 0 || out != "from a file in /work/proj\n" {
		t.Errorf("--description-file = %d %q", code, out)
	}
	if out, _, code := ts.run(t, "from stdin", "task", "new", "--description-file=-"); code != 0 || out != "from stdin in /work/proj\n" {
		t.Errorf("--description-file - = %d %q", code, out)
	}
	if _, errs, code := ts.run(t, "", "task", "new", "--description-file", t.TempDir()); code != cli.ExitFailed || !strings.Contains(errs, "is not a regular file") {
		t.Errorf("a folder = %d %q", code, errs)
	}
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), cli.MaxText+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := ts.run(t, "", "task", "new", "--description-file", big); code != cli.ExitFailed || !strings.Contains(errs, "larger than") {
		t.Errorf("a big file = %d %q", code, errs)
	}
	// Within the client's bound but over the flag's: the server says so.
	mid := filepath.Join(t.TempDir(), "mid")
	if err := os.WriteFile(mid, bytes.Repeat([]byte("x"), 101), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := ts.run(t, "", "task", "new", "--description-file", mid); code != cli.ExitUsage || !strings.Contains(errs, "longer than 100 bytes") {
		t.Errorf("over the flag's bound = %d %q", code, errs)
	}
}

func TestSocketCancelsACommandWhenTheClientLeaves(t *testing.T) {
	ts := startTestServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() {
		var out, errb bytes.Buffer
		done <- RunIn(ctx, ts.paths, []string{"run", "wait"}, "/", nil, &out, &errb)
	}()
	select {
	case <-ts.waits:
	case <-time.After(10 * time.Second):
		t.Fatal("the wait never started")
	}
	cancel() // Ctrl-C: the client goes away
	if code := <-done; code != cli.ExitFailed {
		t.Errorf("client exit %d", code)
	}
	select {
	case <-ts.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the server's command did not end with its client")
	}
}

func TestSocketNotRunning(t *testing.T) {
	p := PathsIn(shortDir(t))
	var out, errb bytes.Buffer
	if code := RunIn(t.Context(), p, []string{"task", "list"}, "/", nil, &out, &errb); code != 1 || errb.String() != NotRunning+"\n" {
		t.Fatalf("no run directory: %d %q", code, errb.String())
	}
	// A token left by a Flint that crashed, and nobody listening.
	if err := hostmode.PrepareRunDir(p.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := hostmode.WriteToken(p.Token); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	if code := RunIn(t.Context(), p, []string{"task", "list"}, "/", nil, &out, &errb); code != 1 || errb.String() != NotRunning+"\n" {
		t.Fatalf("a stale token: %d %q", code, errb.String())
	}
}

func TestSocketFilesAndLifecycle(t *testing.T) {
	ts := startTestServer(t)
	for path, want := range map[string]os.FileMode{ts.paths.Dir: 0o700, ts.paths.Sock: 0o600, ts.paths.Token: 0o600} {
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s has mode %v, want %v", path, fi.Mode().Perm(), want)
		}
	}
	// One server per run directory.
	if _, err := Start(Options{Paths: ts.paths, Registry: ts.srv.o.Registry, Classify: ts.srv.o.Classify}); !errors.Is(err, hostmode.ErrAlreadyRunning) {
		t.Errorf("a second server = %v", err)
	}
	// A token that does not match is refused.
	if err := os.WriteFile(ts.paths.Token, []byte(strings.Repeat("0", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := ts.run(t, "", "task", "list"); code != 1 || !strings.Contains(errs, "the token does not match") {
		t.Errorf("a wrong token = %d %q", code, errs)
	}
	ts.srv.Close()
	for _, path := range []string{ts.paths.Sock, ts.paths.Token} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is left after Close: %v", path, err)
		}
	}
	// The next one starts in the same directory.
	again, err := Start(Options{Paths: ts.paths, Registry: ts.srv.o.Registry, Classify: ts.srv.o.Classify})
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}

func TestSocketBoundsARequest(t *testing.T) {
	ts := startTestServer(t)
	args := append([]string{"task", "list"}, make([]string, cli.MaxArgs)...)
	if _, errs, code := ts.run(t, "", args...); code != 1 || !strings.Contains(errs, "more than 64 words") {
		t.Errorf("too many words = %d %q", code, errs)
	}
	if _, errs, code := ts.run(t, "", "task", "new", "--description", strings.Repeat("x", cli.MaxText+1)); code != 1 || !strings.Contains(errs, "longer than") {
		t.Errorf("a long word = %d %q", code, errs)
	}
}

func TestReadFiles(t *testing.T) {
	args, files, err := readFiles([]string{"workflow", "run", "x", "--", "--task-file", "y"}, nil)
	if err != nil || len(files) != 0 || len(args) != 6 {
		t.Errorf("after -- = %q %v %v", args, files, err)
	}
	for _, bad := range [][]string{
		{"--task-file"},
		{"--task-file", "-", "--note-file", "-"},
		{"--task-file=-", "--task-file=-"},
		{"--a-file=-", "--b-file=/dev/null", "--c-file=/dev/null", "--d-file=/dev/null", "--e-file=/dev/null"},
	} {
		if _, _, err := readFiles(bad, strings.NewReader("x")); err == nil {
			t.Errorf("readFiles(%q) succeeded", bad)
		}
	}
}
