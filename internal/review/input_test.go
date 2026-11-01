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

func TestRenderNumbersNewLines(t *testing.T) {
	r := renderFile(Change{Path: "main.go", Kind: "modified", Diff: modifiedDiff})
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
	got := strings.Split(strings.TrimRight(r.header+r.full(), "\n"), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("render:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r.added != 4 || r.removed != 1 {
		t.Errorf("stats +%d -%d", r.added, r.removed)
	}
}

func TestRenderUntrackedAndSpecialFiles(t *testing.T) {
	r := renderFile(Change{Path: "new.txt", Kind: "untracked", Content: "one\ntwo\n"})
	if got := r.header + r.full(); got != "## new.txt (new file)\n@@ new file @@\n    1 +one\n    2 +two\n" {
		t.Errorf("untracked: %q", got)
	}
	if got := renderFile(Change{Path: "img.png", Kind: "untracked", Binary: true}).full(); !strings.Contains(got, "binary") {
		t.Errorf("binary: %q", got)
	}
	if got := renderFile(Change{Path: "old.go", Kind: "deleted"}).header; got != "## old.go (deleted)\n" {
		t.Errorf("deleted: %q", got)
	}
	r = renderFile(Change{Path: "b.go", OrigPath: "a.go", Kind: "renamed", Diff: "diff --git a/a.go b/b.go\nsimilarity index 100%\nrename from a.go\nrename to b.go\n"})
	if r.header != "## b.go (renamed from a.go)\n" || r.full() != "(renamed without changes)\n" {
		t.Errorf("rename: %q %q", r.header, r.full())
	}
	r = renderFile(Change{Path: "x.bin", Kind: "modified", Diff: "diff --git a/x.bin b/x.bin\nBinary files a/x.bin and b/x.bin differ\n"})
	if !strings.Contains(r.full(), "binary") {
		t.Errorf("binary diff: %q", r.full())
	}
}

func TestBuildInputRedactsAndSkipsSecretFiles(t *testing.T) {
	in := BuildInput([]Change{
		{Path: ".env", Kind: "untracked", Content: "DB_PASSWORD=hunter2\n"},
		{Path: "conf.go", Kind: "untracked", Content: "token := \"ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\"\n"},
	}, 64<<10)
	if strings.Contains(in.Text, "hunter2") || strings.Contains(in.Text, ".env") {
		t.Errorf("secret file sent:\n%s", in.Text)
	}
	if strings.Contains(in.Text, "ghp_0123456789") {
		t.Errorf("token not redacted:\n%s", in.Text)
	}
	if len(in.Files) != 1 || in.Files[0] != "conf.go" {
		t.Errorf("files = %v", in.Files)
	}
	if len(in.Notes) != 1 || !strings.Contains(in.Notes[0], ".env") {
		t.Errorf("notes = %v", in.Notes)
	}
}

func TestBuildInputFitsTheLimit(t *testing.T) {
	big := strings.Repeat("x := 1\n", 4000) // ~28 KB rendered as ~50 KB
	small := "a\nb\n"
	changes := []Change{
		{Path: "big.go", Kind: "untracked", Content: big},
		{Path: "small.go", Kind: "untracked", Content: small},
		{Path: "big2.go", Kind: "untracked", Content: big},
	}
	in := BuildInput(changes, 16<<10)
	if len(in.Text) > 16<<10+2048 {
		t.Errorf("input is %d bytes", len(in.Text))
	}
	if !strings.Contains(in.Text, "    2 +b") {
		t.Errorf("small file shortened:\n%s", in.Text[:min(400, len(in.Text))])
	}
	if strings.Count(in.Text, "diff shortened") != 2 {
		t.Errorf("want both big files shortened, got:\n%s", tail(in.Text))
	}
	if !in.Shortened || len(in.Notes) == 0 || !strings.Contains(strings.Join(in.Notes, " "), "16 KB") {
		t.Errorf("notes = %v shortened=%v", in.Notes, in.Shortened)
	}
	// Order is kept.
	if i, j := strings.Index(in.Text, "## big.go"), strings.Index(in.Text, "## small.go"); i < 0 || j < i {
		t.Errorf("order: %d %d", i, j)
	}
}

func TestBuildInputSummarizesWhenNoRoomIsLeft(t *testing.T) {
	var changes []Change
	for i := range 60 {
		changes = append(changes, Change{Path: "f" + string(rune('a'+i%26)) + strings.Repeat("x", i) + ".go", Kind: "untracked", Content: strings.Repeat("line\n", 200)})
	}
	in := BuildInput(changes, 8<<10)
	if !strings.Contains(in.Text, "diff left out") {
		t.Errorf("no summaries:\n%s", tail(in.Text))
	}
	if len(in.Files) != 60 {
		t.Errorf("files = %d", len(in.Files))
	}
}

func TestShortenKeepsWholeHunksFirst(t *testing.T) {
	r := renderFile(Change{Path: "main.go", Kind: "modified", Diff: modifiedDiff})
	first := len(r.hunks[0])
	got := r.body(first + 10)
	if !strings.Contains(got, "    5  func main() {") || strings.Contains(got, "b := 2") {
		t.Errorf("shortened:\n%s", got)
	}
	if !strings.Contains(got, "1 of 2 hunks left out") {
		t.Errorf("note missing:\n%s", got)
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

func tail(s string) string {
	if len(s) > 600 {
		return s[len(s)-600:]
	}
	return s
}
