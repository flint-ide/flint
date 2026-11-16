package agenttask

import (
	"strings"
	"testing"
)

func TestUnion(t *testing.T) {
	a := map[string]FileChange{
		"b.go":     {Added: 3, Deleted: 1},
		"a/new.go": {Added: 10, Untracked: true},
	}
	b := map[string]FileChange{
		"b.go":   {Added: 7},
		"z.png":  {Binary: true},
		"new.go": {OrigPath: "old.go", Added: 1, Deleted: 1},
	}
	rows, truncated := Union([]map[string]FileChange{a, nil, b}, 10)
	if truncated {
		t.Error("truncated")
	}
	var paths []string
	for _, r := range rows {
		paths = append(paths, r.Path)
		if len(r.Changes) != 3 {
			t.Fatalf("%s: %d columns, want one per task", r.Path, len(r.Changes))
		}
		if r.Changes[1] != nil {
			t.Errorf("%s: a task without changes has one", r.Path)
		}
	}
	if got := strings.Join(paths, " "); got != "a/new.go b.go new.go z.png" {
		t.Errorf("files = %s", got)
	}
	both := rows[1]
	if both.Changes[0].Added != 3 || both.Changes[0].Deleted != 1 || both.Changes[2].Added != 7 {
		t.Errorf("b.go = %+v %+v", both.Changes[0], both.Changes[2])
	}
	if rows[0].Changes[2] != nil || !rows[0].Changes[0].Untracked {
		t.Errorf("a/new.go = %+v", rows[0].Changes)
	}
	if rows[2].Changes[2].OrigPath != "old.go" || !rows[3].Changes[2].Binary {
		t.Errorf("rename or binary lost: %+v %+v", rows[2].Changes[2], rows[3].Changes[2])
	}

	rows, truncated = Union([]map[string]FileChange{a, b}, 2)
	if !truncated || len(rows) != 2 || rows[0].Path != "a/new.go" || rows[1].Path != "b.go" {
		t.Errorf("limited = %+v truncated %v", rows, truncated)
	}
	rows, truncated = Union(nil, 5)
	if rows == nil || len(rows) != 0 || truncated {
		t.Errorf("no tasks = %#v %v", rows, truncated)
	}
}

// render prints lines the way "git diff" does.
func render(lines []DiffLine) string {
	var b strings.Builder
	for _, l := range lines {
		switch l.Kind {
		case LineHunk:
			b.WriteString(l.Text)
		case LineAdded:
			b.WriteString("+" + l.Text)
		case LineDeleted:
			b.WriteString("-" + l.Text)
		default:
			b.WriteString(" " + l.Text)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line" + string(rune('a'+i))
	}
	return out
}

func TestUnified(t *testing.T) {
	old := numbered(12)
	changed := append([]string{}, old...)
	changed[5] = "CHANGED"
	a, b := strings.Join(old, "\n")+"\n", strings.Join(changed, "\n")+"\n"

	lines, stat, truncated := Unified(a, b, 2, 100)
	want := "@@ -4,5 +4,5 @@\n lined\n linee\n-linef\n+CHANGED\n lineg\n lineh\n"
	if got := render(lines); got != want {
		t.Errorf("diff =\n%s\nwant\n%s", got, want)
	}
	if stat.Added != 1 || stat.Deleted != 1 || truncated {
		t.Errorf("stat = %+v truncated %v", stat, truncated)
	}
	// Line numbers: context has both, a deletion the old one, an addition the new one.
	if l := lines[1]; l.A != 4 || l.B != 4 {
		t.Errorf("context numbers = %+v", l)
	}
	if l := lines[3]; l.Kind != LineDeleted || l.A != 6 || l.B != 0 {
		t.Errorf("deleted = %+v", l)
	}
	if l := lines[4]; l.Kind != LineAdded || l.A != 0 || l.B != 6 {
		t.Errorf("added = %+v", l)
	}

	// Equal texts: no lines (and not nil: the UI lists them).
	if lines, stat, _ := Unified(a, a, 3, 100); lines == nil || len(lines) != 0 || stat.Added+stat.Deleted != 0 {
		t.Errorf("equal texts = %v %+v", lines, stat)
	}

	// A new file is all additions and a deleted one all deletions, with or
	// without a final line break; an empty text has no lines.
	for _, text := range []string{"x\ny", "x\ny\n"} {
		lines, stat, _ = Unified("", text, 3, 100)
		if got := render(lines); got != "@@ -0,0 +1,2 @@\n+x\n+y\n" || stat.Added != 2 || stat.Deleted != 0 {
			t.Errorf("new file %q =\n%s%+v", text, got, stat)
		}
		lines, stat, _ = Unified(text, "", 3, 100)
		if got := render(lines); got != "@@ -1,2 +0,0 @@\n-x\n-y\n" || stat.Added != 0 || stat.Deleted != 2 {
			t.Errorf("deleted file %q =\n%s%+v", text, got, stat)
		}
	}
	// An empty line in a new file is an addition, not context of nothing.
	lines, stat, _ = Unified("", "x\n\ny\n", 3, 100)
	if got := render(lines); got != "@@ -0,0 +1,3 @@\n+x\n+\n+y\n" || stat.Added != 3 {
		t.Errorf("new file with an empty line =\n%s%+v", got, stat)
	}
	if lines, _, _ := Unified("x\n", "x", 3, 100); len(lines) != 0 {
		t.Errorf("only the final line break differs: %v", lines)
	}
	if lines, _, _ := Unified("", "", 3, 100); len(lines) != 0 {
		t.Errorf("two empty texts: %v", lines)
	}
}

func TestUnifiedJoinsNearHunksAndSplitsFarOnes(t *testing.T) {
	old := numbered(20)
	near := append([]string{}, old...)
	near[2], near[5] = "X", "Y" // two unchanged lines between: one block with context 1
	lines, _, _ := Unified(strings.Join(old, "\n"), strings.Join(near, "\n"), 1, 100)
	if n := countKind(lines, LineHunk); n != 1 {
		t.Errorf("near changes gave %d blocks:\n%s", n, render(lines))
	}
	far := append([]string{}, old...)
	far[2], far[15] = "X", "Y"
	lines, stat, _ := Unified(strings.Join(old, "\n"), strings.Join(far, "\n"), 1, 100)
	if n := countKind(lines, LineHunk); n != 2 || stat.Added != 2 || stat.Deleted != 2 {
		t.Errorf("far changes gave %d blocks, %+v:\n%s", n, stat, render(lines))
	}
	// Every line of both texts appears at most once, in order.
	lastA, lastB := 0, 0
	for _, l := range lines {
		if l.A != 0 {
			if l.A <= lastA {
				t.Fatalf("old line %d after %d", l.A, lastA)
			}
			lastA = l.A
		}
		if l.B != 0 {
			if l.B <= lastB {
				t.Fatalf("new line %d after %d", l.B, lastB)
			}
			lastB = l.B
		}
	}
}

func countKind(lines []DiffLine, kind string) int {
	n := 0
	for _, l := range lines {
		if l.Kind == kind {
			n++
		}
	}
	return n
}

func TestUnifiedBounds(t *testing.T) {
	lines, stat, truncated := Unified("", strings.Repeat("x\n", 500), 3, 50)
	if len(lines) != 50 || !truncated {
		t.Errorf("%d lines, truncated %v", len(lines), truncated)
	}
	if stat.Added < 500 {
		t.Errorf("the counts cover the whole diff: %+v", stat)
	}
	long := strings.Repeat("é", 3000)
	lines, _, _ = Unified("a", long, 0, 10)
	for _, l := range lines {
		if n := len([]rune(l.Text)); n > maxLineRunes+1 {
			t.Errorf("a line of %d runes was kept", n)
		}
	}
}
