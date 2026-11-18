package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bareWithCommit makes a bare repository with one commit and returns its
// path.
func bareWithCommit(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	initRepo(t, work)
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "README")
	mustGit(t, work, "commit", "-q", "-m", "first")
	bare := filepath.Join(base, "src.git")
	mustGit(t, base, "clone", "-q", "--bare", work, bare)
	return bare
}

func TestClone(t *testing.T) {
	bare := bareWithCommit(t)
	dest := filepath.Join(t.TempDir(), "out")
	var lines []string
	if err := Clone(t.Context(), CloneOptions{URL: bare, Dir: dest, Protocols: []string{"file"}, Progress: func(l string) { lines = append(lines, l) }}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "README")); err != nil || string(data) != "hi\n" {
		t.Fatalf("README %q %v", data, err)
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Cloning into") {
		t.Errorf("progress %q", lines)
	}
}

// Only the transports asked for: a local path is the file transport, and
// ext:: and options are never run.
func TestCloneRefusesOtherTransports(t *testing.T) {
	isolateGit(t)
	bare := bareWithCommit(t)
	marker := filepath.Join(t.TempDir(), "pwned")
	for _, tc := range []struct {
		url       string
		protocols []string
		want      string
	}{
		{bare, []string{"https", "ssh"}, "transport 'file' not allowed"},
		{"file://" + bare, []string{"https"}, "transport 'file' not allowed"},
		{"ext::sh -c touch% " + marker, []string{"https", "ssh"}, "transport 'ext' not allowed"},
		{"ext::sh -c touch% " + marker, []string{"file"}, "transport 'ext' not allowed"},
		{"-uhelp", []string{"https"}, "invalid URL"},
		{bare, []string{"ext"}, `transport "ext" is not allowed`},
		{bare, nil, "no transport allowed"},
	} {
		dest := filepath.Join(t.TempDir(), "out")
		err := Clone(t.Context(), CloneOptions{URL: tc.url, Dir: dest, Protocols: tc.protocols})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %v: err = %v, want %q", tc.url, tc.protocols, err, tc.want)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("ext:: ran a command")
	}
	if err := Clone(t.Context(), CloneOptions{URL: bare, Dir: "relative", Protocols: []string{"file"}}); err == nil {
		t.Error("a relative destination was accepted")
	}
}

func TestCloneCancelled(t *testing.T) {
	bare := bareWithCommit(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Clone(ctx, CloneOptions{URL: bare, Dir: filepath.Join(t.TempDir(), "out"), Protocols: []string{"file"}})
	if err == nil {
		t.Fatal("a cancelled clone worked")
	}
}
