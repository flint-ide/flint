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

const (
	mainBase = "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	mainHead = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
)

func TestFileDiffFromLocalClone(t *testing.T) {
	f := prFake(t)
	c := f.client()
	local := fakeLocal{objects: map[string]string{"headblob1": mainHead}}
	d, err := c.FileDiff(context.Background(), t.TempDir(), 7, "main.go", local)
	if err != nil {
		t.Fatal(err)
	}
	if d.Head != mainHead || d.Base != mainBase {
		t.Fatalf("diff = %q / %q", d.Base, d.Head)
	}
	if !reflect.DeepEqual(d.Left, []LineRange{{1, 5}}) || !reflect.DeepEqual(d.Right, []LineRange{{1, 7}}) {
		t.Errorf("commentable lines = %v / %v", d.Left, d.Right)
	}
	for _, cl := range f.calls() {
		if strings.Contains(cl.String(), "blobs") || strings.Contains(cl.String(), "contents") {
			t.Errorf("the head blob was in the clone, yet gh fetched %v", cl)
		}
	}
}

func TestFileDiffDownloadsHead(t *testing.T) {
	f := prFake(t)
	f.on("git/blobs/headblob1", mainHead)
	d, err := f.client().FileDiff(context.Background(), t.TempDir(), 7, "main.go", fakeLocal{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != mainBase || d.Head != mainHead {
		t.Fatalf("diff = %q / %q", d.Base, d.Head)
	}
	if !hasCall(f, "api -H Accept: application/vnd.github.raw+json repos/acme/flint/git/blobs/headblob1") {
		t.Errorf("calls = %v", f.calls())
	}
}

func TestFileDiffAddedFile(t *testing.T) {
	f := prFake(t)
	f.on("git/blobs/headblob2", "# New\ntext")
	d, err := f.client().FileDiff(context.Background(), t.TempDir(), 7, "docs/new.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != "" || d.Head != "# New\ntext" || len(d.Left) != 0 || !reflect.DeepEqual(d.Right, []LineRange{{1, 2}}) {
		t.Fatalf("diff = %+v", d)
	}
}

func TestFileDiffWithoutPatchUsesMergeBase(t *testing.T) {
	f := prFake(t)
	f.on("git/blobs/headblob3", "package lib\n")
	f.on("compare/"+strings.Repeat("b", 40)+"..."+strings.Repeat("h", 40), "mb123\n")
	f.on("contents/lib/a.go?ref=mb123", "package lib // old\n")
	d, err := f.client().FileDiff(context.Background(), t.TempDir(), 7, "lib/b go.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != "package lib // old\n" || d.Head != "package lib\n" || d.PreviousPath != "lib/a.go" {
		t.Fatalf("diff = %+v", d)
	}
	if !hasCall(f, "api -H Accept: application/vnd.github.raw+json repos/acme/flint/contents/lib/a.go?ref=mb123") {
		t.Errorf("calls = %v", f.calls())
	}

	// With both commits in the clone, nothing is downloaded but the head blob.
	f2 := prFake(t)
	local := fakeLocal{
		objects:    map[string]string{"headblob3": "package lib\n", "mb9:lib/a.go": "old\n"},
		mergeBases: map[string]string{strings.Repeat("b", 40) + "..." + strings.Repeat("h", 40): "mb9"},
	}
	d, err = f2.client().FileDiff(context.Background(), t.TempDir(), 7, "lib/b go.go", local)
	if err != nil || d.Base != "old\n" {
		t.Fatalf("diff = %+v, %v", d, err)
	}
}

func TestFileDiffBinary(t *testing.T) {
	f := prFake(t)
	f.on("compare/", "mb123\n")
	f.on("contents/logo.png?ref=mb123", "\x89PNG\x00\x00")
	d, err := f.client().FileDiff(context.Background(), t.TempDir(), 7, "logo.png", nil)
	if err != nil || !d.Binary || d.Base != "" || d.Head != "" {
		t.Fatalf("diff = %+v, %v", d, err)
	}
}

func TestFileDiffUnknownFile(t *testing.T) {
	f := prFake(t)
	if _, err := f.client().FileDiff(context.Background(), t.TempDir(), 7, "nope.go", nil); err == nil {
		t.Fatal("a file the pull request doesn't change must be an error")
	}
}

func TestEscapePath(t *testing.T) {
	if got := escapePath("lib/b go.go"); got != "lib/b%20go.go" {
		t.Fatalf("escapePath = %q", got)
	}
}

func hasCall(f *fakeGH, want string) bool {
	for _, cl := range f.calls() {
		if cl.String() == want {
			return true
		}
	}
	return false
}
