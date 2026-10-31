package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGH is a stand-in gh on PATH: a shell script that records every invocation
// (arguments and stdin) and answers from rules the test registers. The tests never
// reach GitHub: PATH holds only the fake and the system directories, and newFake
// checks that "gh" resolves to the fake before anything runs.
type fakeGH struct {
	t     *testing.T
	dir   string
	rules int
}

// fakeScript matches the space-joined arguments plus stdin (GraphQL requests come on
// stdin) against each rule's substring, in order, and prints that rule's stdout /
// stderr with its exit code.
// Shell builtins (read) instead of cat where possible: process spawns dominate its cost.
const fakeScript = `#!/bin/sh
dir="$FAKE_GH_DIR"
id=$(mktemp "$dir/calls/call.XXXXXX") || exit 99
for a in "$@"; do printf '%s\0' "$a"; done > "$id.args"
stdin=$(cat)
printf '%s' "$stdin" > "$id.stdin"
echo "$id" >> "$dir/log"
joined="$* $stdin"
for rule in "$dir"/rules/*; do
  IFS= read -r pattern < "$rule/match"
  case "$joined" in
    *"$pattern"*)
      cat "$rule/stdout"
      cat "$rule/stderr" >&2
      read -r code < "$rule/code"
      exit "$code"
      ;;
  esac
done
echo "fake gh: no rule for: $joined" >&2
exit 97
`

func newFake(t *testing.T) *fakeGH {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"bin", "calls", "rules"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "bin", "gh")
	if err := os.WriteFile(bin, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GH_DIR", dir)
	t.Setenv("PATH", filepath.Join(dir, "bin")+":/usr/bin:/bin")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if got, err := exec.LookPath("gh"); err != nil || got != bin {
		t.Fatalf("gh resolves to %q (%v), want the fake %q", got, err, bin)
	}
	return &fakeGH{t: t, dir: dir}
}

// client returns a Client that finds gh on the (fake) PATH without the login shell.
func (f *fakeGH) client() *Client {
	return newClient(func() (string, error) { return exec.LookPath("gh") })
}

// on answers invocations whose joined arguments contain match.
func (f *fakeGH) on(match, stdout string) { f.onExit(match, stdout, "", 0) }

func (f *fakeGH) onExit(match, stdout, stderr string, code int) {
	f.t.Helper()
	f.rules++
	rule := filepath.Join(f.dir, "rules", fmt.Sprintf("%03d", f.rules))
	files := map[string]string{"match": match, "stdout": stdout, "stderr": stderr, "code": fmt.Sprint(code)}
	if err := os.MkdirAll(rule, 0o755); err != nil {
		f.t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(rule, name), []byte(content), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// onFixture answers with a file from testdata.
func (f *fakeGH) onFixture(match, fixture string) {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		f.t.Fatal(err)
	}
	f.on(match, string(b))
}

type call struct {
	args  []string
	stdin string
}

func (c call) String() string { return strings.Join(c.args, " ") }

// calls returns the recorded invocations in order.
func (f *fakeGH) calls() []call {
	f.t.Helper()
	log, err := os.ReadFile(filepath.Join(f.dir, "log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out []call
	for id := range strings.SplitSeq(strings.TrimSpace(string(log)), "\n") {
		args, err := os.ReadFile(id + ".args")
		if err != nil {
			f.t.Fatal(err)
		}
		stdin, err := os.ReadFile(id + ".stdin")
		if err != nil {
			f.t.Fatal(err)
		}
		out = append(out, call{args: strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00"), stdin: string(stdin)})
	}
	return out
}

// reset forgets recorded calls (rules stay).
func (f *fakeGH) reset() {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.dir, "log")); err != nil && !os.IsNotExist(err) {
		f.t.Fatal(err)
	}
}

// fakeLocal is a Local clone with a fixed set of objects.
type fakeLocal struct {
	objects    map[string]string
	mergeBases map[string]string
}

func (l fakeLocal) Object(_ context.Context, spec string) ([]byte, bool) {
	s, ok := l.objects[spec]
	return []byte(s), ok
}

func (l fakeLocal) MergeBase(_ context.Context, a, b string) (string, bool) {
	s, ok := l.mergeBases[a+"..."+b]
	return s, ok
}

const repoJSON = `{"name":"flint","nameWithOwner":"acme/flint","owner":{"login":"acme"},"url":"https://github.com/acme/flint","defaultBranchRef":{"name":"main"}}`
