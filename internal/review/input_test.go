package review

import (
	"strings"
	"testing"
)

const modifiedDiff = `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@
 package main
-import "fmt"
+import (
+	"fmt"
+)
 func main() {
@@ -10,2 +11,3 @@ func main() {
 	a := 1
+	b := 2
 	return
`

// untracked is an untracked file whose reads are recorded in *limits.
func untracked(path, content string, limits *[]int) Change {
	return Change{Path: path, Kind: "untracked", Size: len(content), Read: func(limit int) (string, bool, bool) {
		if limits != nil {
			*limits = append(*limits, limit)
		}
		s := content[:min(limit, len(content))]
		return s, strings.Contains(s, "\x00"), true
	}}
}

func TestRenderNumbersNewLines(t *testing.T) {
	f := newFile(Change{Path: "main.go", Kind: "modified", Diff: modifiedDiff})
	want := []string{
		"## main.go (modified)",
		"@@ -1,4 +1,5 @@",
		"    1  package main",
		"      -import \"fmt\"",
		"    2 +import (",
		"    3 +\t\"fmt\"",
		"    4 +)",
		"    5  func main() {",
		"@@ -10,2 +11,3 @@ func main() {",
		"   11  \ta := 1",
		"   12 +\tb := 2",
		"   13  \treturn",
	}
	body, whole := f.body(1 << 20)
	got := strings.Split(strings.TrimRight(f.header+body, "\n"), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") || !whole {
		t.Errorf("render:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if f.added != 4 || f.removed != 1 {
		t.Errorf("stats +%d -%d", f.added, f.removed)
	}
}

// An unpaired rename (git mv a z, then z rewritten) gives two sections; only
// the one of the reviewed path is its diff.
func TestRenderStopsAtTheNextSection(t *testing.T) {
	diff := "diff --git a/z.txt b/z.txt\nnew file mode 100644\n--- /dev/null\n+++ b/z.txt\n@@ -0,0 +1,2 @@\n+new one\n+new two\n" +
		"diff --git a/a.txt b/a.txt\ndeleted file mode 100644\n--- a/a.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-old one\n-old two\n"
	f := newFile(Change{Path: "z.txt", OrigPath: "a.txt", Kind: "renamed", Diff: diff})
	body, _ := f.body(1 << 20)
	if strings.Contains(body, "old one") || strings.Contains(body, "/dev/null") || strings.Contains(body, "a.txt") {
		t.Errorf("another section leaked in:\n%s", body)
	}
	if !strings.Contains(body, "    2 +new two") || f.removed != 0 {
		t.Errorf("body:\n%s", body)
	}
}

func TestRenderUntrackedAndSpecialFiles(t *testing.T) {
	f := newFile(untracked("new.txt", "one\ntwo\n", nil))
	if body, whole := f.body(1 << 20); f.header+body != "## new.txt (new file)\n@@ new file @@\n    1 +one\n    2 +two\n" || !whole {
		t.Errorf("untracked: %q", f.header+body)
	}
	if body, _ := newFile(untracked("img.png", "PNG\x00\x01", nil)).body(1 << 20); !strings.Contains(body, "binary") {
		t.Errorf("binary: %q", body)
	}
	unreadable := Change{Path: "link", Kind: "untracked", Size: 10, Read: func(int) (string, bool, bool) { return "", false, false }}
	if body, _ := newFile(unreadable).body(1 << 20); !strings.Contains(body, "not readable") {
		t.Errorf("unreadable: %q", body)
	}
	if got := newFile(Change{Path: "old.go", Kind: "deleted"}).header; got != "## old.go (deleted)\n" {
		t.Errorf("deleted: %q", got)
	}
	f = newFile(Change{Path: "b.go", OrigPath: "a.go", Kind: "renamed", Diff: "diff --git a/a.go b/b.go\nsimilarity index 100%\nrename from a.go\nrename to b.go\n"})
	if body, _ := f.body(1 << 20); f.header != "## b.go (renamed from a.go)\n" || body != "(renamed without changes)\n" {
		t.Errorf("rename: %q %q", f.header, body)
	}
	f = newFile(Change{Path: "x.bin", Kind: "modified", Diff: "diff --git a/x.bin b/x.bin\nBinary files a/x.bin and b/x.bin differ\n"})
	if body, _ := f.body(1 << 20); !strings.Contains(body, "binary") {
		t.Errorf("binary diff: %q", body)
	}
	if f := newFile(Change{Path: "evil\n## other.go", Kind: "modified"}); strings.Count(f.header, "\n") != 1 {
		t.Errorf("a line break in a path starts a fake section: %q", f.header)
	}
}

func TestBuildInputRedactsAndSkipsSecretFiles(t *testing.T) {
	var reads []int
	in := BuildInput([]Change{
		untracked(".env", "DB_PASSWORD=hunter2\n", &reads),
		untracked("conf.go", "token := \"ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\"\n", nil),
		{Path: "nul.go", Kind: "modified", Diff: "diff --git a/nul.go b/nul.go\n@@ -1 +1 @@\n-a\n+x\x00y\n"},
	}, 64<<10)
	if strings.Contains(in.Text, "hunter2") || strings.Contains(in.Text, ".env") || len(reads) != 0 {
		t.Errorf("secret file sent or read (%v):\n%s", reads, in.Text)
	}
	if strings.Contains(in.Text, "ghp_0123456789") {
		t.Errorf("token not redacted:\n%s", in.Text)
	}
	if strings.Contains(in.Text, "\x00") {
		t.Errorf("NUL byte sent: %q", in.Text)
	}
	if strings.Join(in.Files, ",") != "conf.go,nul.go" || in.Shortened {
		t.Errorf("files = %v", in.Files)
	}
	if len(in.Notes) != 1 || !strings.Contains(in.Notes[0], ".env") {
		t.Errorf("notes = %v", in.Notes)
	}
}

func TestBuildInputStaysWithinTheLimit(t *testing.T) {
	big := strings.Repeat("x := 1\n", 4000)
	var reads []int
	changes := []Change{
		untracked("big.go", big, &reads),
		untracked("small.go", "a\nb\n", &reads),
		untracked("big2.go", big, &reads),
		{Path: "main.go", Kind: "modified", Diff: modifiedDiff},
	}
	for _, limit := range []int{8 << 10, 16 << 10, 64 << 10} {
		reads = nil
		in := BuildInput(changes, limit)
		if len(in.Text) > limit {
			t.Errorf("%d KB: input is %d bytes", limit>>10, len(in.Text))
		}
		if !strings.Contains(in.Text, "    2 +b") || !strings.Contains(in.Text, "   12 +\tb := 2") {
			t.Errorf("%d KB: small diffs shortened:\n%s", limit>>10, in.Text[:min(600, len(in.Text))])
		}
		if !in.Shortened || !strings.Contains(strings.Join(in.Notes, " "), "KB limit") {
			t.Errorf("%d KB: notes = %v", limit>>10, in.Notes)
		}
		// Files are read only as far as their share of the limit.
		for _, n := range reads {
			if n > limit {
				t.Errorf("%d KB: read %d bytes of a file", limit>>10, n)
			}
		}
		if i, j := strings.Index(in.Text, "## big.go"), strings.Index(in.Text, "## small.go"); i < 0 || j < i {
			t.Errorf("order: %d %d", i, j)
		}
	}
}

func TestBuildInputSummarizesAndLeavesOutWhenThereIsNoRoom(t *testing.T) {
	var changes []Change
	for i := range 300 {
		changes = append(changes, untracked("pkg/file"+strings.Repeat("x", i%40)+string(rune('a'+i%26))+".go", strings.Repeat("line\n", 200), nil))
	}
	in := BuildInput(changes, 8<<10)
	if len(in.Text) > 8<<10 {
		t.Errorf("input is %d bytes", len(in.Text))
	}
	if !strings.Contains(in.Text, "left out to fit the size limit") || !strings.Contains(in.Text, "more changed files left out") {
		t.Errorf("no summaries:\n%s", in.Text[len(in.Text)-400:])
	}
	if len(in.Files) == 0 || len(in.Files) >= 300 {
		t.Errorf("files = %d", len(in.Files))
	}
	if !strings.Contains(strings.Join(in.Notes, " "), "left out (") {
		t.Errorf("notes = %v", in.Notes)
	}
}

// A cut through a secret must not leave half of it unredacted.
func TestCutRedactsASecretItSplits(t *testing.T) {
	token := "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"
	content := strings.Repeat("// filler line\n", 30) + "var t = \"" + token + "\"\n" + strings.Repeat("// more\n", 200)
	f := newFile(untracked("t.go", content, nil))
	for budget := 400; budget < 1200; budget += 7 {
		body, _ := f.body(budget)
		if len(body) > budget {
			t.Fatalf("budget %d: body is %d bytes", budget, len(body))
		}
		if strings.Contains(body, "ghp_0123") {
			t.Fatalf("budget %d: token prefix sent:\n%s", budget, body)
		}
	}
}

func TestShortenKeepsWholeHunksFirst(t *testing.T) {
	f := newFile(Change{Path: "main.go", Kind: "modified", Diff: modifiedDiff})
	budget := len(f.hunks[0]) + len(shortenedNote) + 5
	got, whole := f.body(budget)
	if whole || !strings.Contains(got, "    5  func main() {") || strings.Contains(got, "b := 2") || !strings.HasSuffix(got, shortenedNote) {
		t.Errorf("shortened:\n%s", got)
	}
}

func TestLines(t *testing.T) {
	if got := Lines("a\r\nb\n"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Lines = %q", got)
	}
	if got := Lines(""); len(got) != 0 {
		t.Errorf("empty = %q", got)
	}
	if got := Lines("a\n\n"); len(got) != 2 {
		t.Errorf("trailing blank = %q", got)
	}
}
