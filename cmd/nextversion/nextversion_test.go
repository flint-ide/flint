package main

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want version
		ok   bool
	}{
		{"v1.2.3", version{1, 2, 3}, true},
		{"v0.10.0", version{0, 10, 0}, true},
		{"1.2.3", version{}, false},
		{"v1.2", version{}, false},
		{"v1.2.3-rc.1", version{}, false},
		{"v01.2.3", version{}, false},
		{"latest", version{}, false},
	}
	for _, c := range cases {
		got, ok := parseVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLatest(t *testing.T) {
	got, ok := latest([]string{"v0.9.0", "nightly", "v0.10.0", "v0.2.5", "v1.0.0-rc.1"})
	if !ok || got != (version{0, 10, 0}) {
		t.Fatalf("latest = %v, %v; want v0.10.0", got, ok)
	}
	if _, ok := latest([]string{"nightly"}); ok {
		t.Fatal("latest of no release tags should report none")
	}
}

func TestBumpFor(t *testing.T) {
	cases := []struct {
		name string
		msgs []string
		want bump
	}{
		{"nothing", nil, bumpNone},
		{"chores only", []string{"chore: tidy", "docs: typo", "ci: cache"}, bumpNone},
		{"fix", []string{"docs: x", "fix: crash on open"}, bumpPatch},
		{"perf", []string{"perf(search): faster"}, bumpPatch},
		{"feat beats fix", []string{"fix: a", "feat(editor): b"}, bumpMinor},
		{"bang", []string{"feat!: drop macOS 11"}, bumpMajor},
		{"scoped bang", []string{"refactor(api)!: rename"}, bumpMajor},
		{"footer", []string{"feat: x\n\nBREAKING CHANGE: settings moved"}, bumpMajor},
		{"footer hyphen", []string{"fix: x\n\nBREAKING-CHANGE: y"}, bumpMajor},
		{"not conventional", []string{"Update README.md", "feature: nope"}, bumpNone},
		{"uppercase type", []string{"Feat: new thing"}, bumpMinor},
	}
	for _, c := range cases {
		if got := bumpFor(c.msgs); got != c.want {
			t.Errorf("%s: bumpFor = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestNext(t *testing.T) {
	cases := []struct {
		prev version
		b    bump
		want version
	}{
		{version{1, 2, 3}, bumpPatch, version{1, 2, 4}},
		{version{1, 2, 3}, bumpMinor, version{1, 3, 0}},
		{version{1, 2, 3}, bumpMajor, version{2, 0, 0}},
		// Before 1.0 a breaking change bumps the minor version.
		{version{0, 4, 1}, bumpMajor, version{0, 5, 0}},
		{version{0, 0, 0}, bumpMinor, version{0, 1, 0}},
	}
	for _, c := range cases {
		if got := next(c.prev, c.b); got != c.want {
			t.Errorf("next(%v, %v) = %v; want %v", c.prev, c.b, got, c.want)
		}
	}
}

func TestParseBump(t *testing.T) {
	for in, want := range map[string]bump{"patch": bumpPatch, "minor": bumpMinor, "major": bumpMajor} {
		if got, err := parseBump(in); err != nil || got != want {
			t.Errorf("parseBump(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := parseBump("huge"); err == nil {
		t.Error("parseBump(huge) should fail")
	}
}

func TestSplitLog(t *testing.T) {
	got := splitLog("feat: a\n\nbody\x1e\nfix: b\x1e\n")
	if len(got) != 2 || got[0] != "feat: a\n\nbody" || got[1] != "fix: b" {
		t.Fatalf("splitLog = %q", got)
	}
}
