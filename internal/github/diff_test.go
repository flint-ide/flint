package github

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"flint/internal/github/githubtest"
)

func TestReversePatch(t *testing.T) {
	tests := []struct{ name, base, head string }{
		{"edit in the middle", "a\nb\nc\nd\ne\nf\ng\nh\n", "a\nb\nc\nD\ne\nf\ng\nh\n"},
		{"insert at the top", "b\nc\n", "a\nb\nc\n"},
		{"append at the end", "a\nb\n", "a\nb\nc\n"},
		{"delete everything", "a\nb\n", ""},
		{"new file", "", "a\nb\n"},
		{"newline added at the end", "a\nb", "a\nb\n"},
		{"newline removed at the end", "a\nb\n", "a\nb"},
		{"both without newline", "a\nb", "a\nc"},
		{"two hunks", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n", "1\nx\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\ny\n15\n"},
		{"pure deletion", "1\n2\n3\n4\n5\n6\n7\n8\n9\n", "1\n2\n3\n4\n8\n9\n"},
		{"empty lines and CRLF", "a\r\n\r\nb\r\n", "a\r\n\r\nc\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := gitPatch(t, tt.base, tt.head)
			got, err := reversePatch(tt.head, patch)
			if err != nil {
				t.Fatalf("reversePatch: %v\npatch:\n%s", err, patch)
			}
			if got != tt.base {
				t.Fatalf("got %q, want %q\npatch:\n%s", got, tt.base, patch)
			}
		})
	}
}

func TestReversePatchRejectsMismatch(t *testing.T) {
	patch := "@@ -1,2 +1,2 @@\n a\n-b\n+c"
	if _, err := reversePatch("a\nzzz\n", patch); !errors.Is(err, errPatchMismatch) {
		t.Fatalf("err = %v, want errPatchMismatch", err)
	}
	if _, err := reversePatch("a\n", "@@ -1,2 +5,2 @@\n a\n-b\n+c"); err == nil {
		t.Fatal("a hunk past the end of the file must fail")
	}
}

// gitPatch diffs two texts with git and strips the file headers, which is the
// "patch" GitHub's API returns for a file.
func gitPatch(t *testing.T, base, head string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(git, "diff", "--no-index", "--no-color", "-U3", "a", "b")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, _ := cmd.Output() // exit 1 means "different"
	s := string(out)
	i := strings.Index(s, "\n@@")
	if i < 0 {
		t.Fatalf("no hunks in git diff output:\n%s", s)
	}
	return strings.TrimSuffix(s[i+1:], "\n")
}

// localClone is a Local backed by a map (blobs by sha or "rev:path").
type localClone struct {
	objects map[string]string
	base    string
}

func (l localClone) Object(_ context.Context, spec string) ([]byte, bool) {
	v, ok := l.objects[spec]
	return []byte(v), ok
}

func (l localClone) MergeBase(context.Context, string, string) (string, bool) {
	return l.base, l.base != ""
}

func TestFileDiffFromLocalClone(t *testing.T) {
	fx := newFixture(t)
	local := localClone{objects: map[string]string{githubtest.MainBlob: githubtest.MainHead}}
	d, err := fx.c.FileDiff(ctx(), fx.dir, 7, "main.go", local)
	if err != nil {
		t.Fatal(err)
	}
	if d.Head != githubtest.MainHead || d.Base != githubtest.MainBase {
		t.Fatalf("diff = %q / %q", d.Base, d.Head)
	}
	if !reflect.DeepEqual(d.Right, []LineRange{{1, 7}}) || !reflect.DeepEqual(d.Left, []LineRange{{1, 5}}) {
		t.Errorf("ranges = %v %v", d.Left, d.Right)
	}
	for _, p := range paths(fx.fake) {
		if strings.Contains(p, "/git/blobs/") {
			t.Error("downloaded a blob the clone has")
		}
	}
}

func TestFileDiffDownloadsHead(t *testing.T) {
	fx := newFixture(t)
	d, err := fx.c.FileDiff(ctx(), fx.dir, 7, "main.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Head != githubtest.MainHead || d.Base != githubtest.MainBase {
		t.Fatalf("diff = %q / %q", d.Base, d.Head)
	}
}

func TestFileDiffAddedFile(t *testing.T) {
	fx := newFixture(t)
	d, err := fx.c.FileDiff(ctx(), fx.dir, 7, "docs/rate-limits.md", nil)
	if err != nil || d.Base != "" || d.Head != githubtest.NewDoc {
		t.Fatalf("diff = %+v, %v", d, err)
	}
}

func TestFileDiffWithoutPatchUsesMergeBase(t *testing.T) {
	fx := newFixture(t)
	d, _ := fx.c.Details(ctx(), fx.dir, 7, false)
	// Pretend GitHub sent no patch (a large file): the base comes from the merge base.
	data, _ := fx.c.prData(ctx(), fx.dir, 7, false)
	f := data.files["main.go"]
	f.Patch = ""
	data.files["main.go"] = f
	out, err := fx.c.buildDiff(ctx(), fx.auth, d, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Base != githubtest.MainBase {
		t.Fatalf("base = %q", out.Base)
	}
	var sawCompare bool
	for _, p := range paths(fx.fake) {
		sawCompare = sawCompare || strings.Contains(p, "/compare/")
	}
	if !sawCompare {
		t.Error("merge base not asked from GitHub")
	}
}

func TestFileDiffBinary(t *testing.T) {
	fx := newFixture(t)
	local := localClone{objects: map[string]string{githubtest.MainBlob: "\x00\x01"}}
	d, err := fx.c.FileDiff(ctx(), fx.dir, 7, "main.go", local)
	if err != nil || !d.Binary || d.Head != "" {
		t.Fatalf("diff = %+v, %v", d, err)
	}
}

func TestFileDiffUnknownFile(t *testing.T) {
	fx := newFixture(t)
	if _, err := fx.c.FileDiff(ctx(), fx.dir, 7, "nope.go", nil); err == nil {
		t.Fatal("unknown file diffed")
	}
}

func TestEscapePath(t *testing.T) {
	if got := escapePath("lib/b go.go"); got != "lib/b%20go.go" {
		t.Fatalf("escapePath = %q", got)
	}
}
