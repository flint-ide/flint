package release

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"v1.2.3", Version{1, 2, 3}, true},
		{"v0.10.0", Version{0, 10, 0}, true},
		{"1.2.3", Version{}, false},
		{"v1.2", Version{}, false},
		{"v1.2.3-rc.1", Version{}, false},
		{"v01.2.3", Version{}, false},
		{"v1.2.3\nv9.9.9", Version{}, false},
		{"latest", Version{}, false},
	}
	for _, c := range cases {
		got, ok := ParseVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLatest(t *testing.T) {
	tags := []string{"v0.9.0", "nightly", "v0.10.0", "v0.2.5", "v1.0.0-rc.1"}
	if got, ok := Latest(tags); !ok || got != (Version{0, 10, 0}) {
		t.Fatalf("Latest = %v, %v; want v0.10.0", got, ok)
	}
	if _, ok := Latest([]string{"nightly"}); ok {
		t.Fatal("Latest of no release tags should report none")
	}
	if got, ok := LatestBefore(tags, Version{0, 10, 0}); !ok || got != (Version{0, 9, 0}) {
		t.Fatalf("LatestBefore(v0.10.0) = %v, %v; want v0.9.0", got, ok)
	}
	if _, ok := LatestBefore(tags, Version{0, 2, 5}); ok {
		t.Fatal("LatestBefore the first release should report none")
	}
}

func TestParseCommit(t *testing.T) {
	cases := []struct {
		msg  string
		want Commit
	}{
		{"feat: add x", Commit{Type: "feat", Subject: "feat: add x", Description: "add x"}},
		{"Fix(git)!: drop y\n\nbody", Commit{Type: "fix", Scope: "git", Breaking: true, Subject: "Fix(git)!: drop y", Description: "drop y"}},
		{"build(deps): bump z", Commit{Type: "build", Scope: "deps", Subject: "build(deps): bump z", Description: "bump z"}},
		{"feat: x\n\nBREAKING CHANGE: settings moved", Commit{Type: "feat", Breaking: true, Subject: "feat: x", Description: "x"}},
		{"refactor: x\n\nBREAKING-CHANGE: y", Commit{Type: "refactor", Breaking: true, Subject: "refactor: x", Description: "x"}},
		{"Update README.md\n\nBREAKING CHANGE: no type, no effect", Commit{Subject: "Update README.md", Description: "Update README.md"}},
		{"feature:no space", Commit{Subject: "feature:no space", Description: "feature:no space"}},
		{"macOS: menu", Commit{Type: "macos", Subject: "macOS: menu", Description: "menu"}},
	}
	for _, c := range cases {
		if got := ParseCommit(c.msg); got != c.want {
			t.Errorf("ParseCommit(%q) = %+v; want %+v", c.msg, got, c.want)
		}
	}
}

func TestSplitLog(t *testing.T) {
	got := SplitLog("feat: a\n\nbody\x1e\nfix: b\x1e\n")
	if len(got) != 2 || got[0] != "feat: a\n\nbody" || got[1] != "fix: b" {
		t.Fatalf("SplitLog = %q", got)
	}
}
