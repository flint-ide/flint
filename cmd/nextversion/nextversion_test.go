package main

import (
	"testing"

	"flint/internal/release"
)

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
	v := func(major, minor, patch int) release.Version {
		return release.Version{Major: major, Minor: minor, Patch: patch}
	}
	cases := []struct {
		prev release.Version
		b    bump
		want release.Version
	}{
		{v(1, 2, 3), bumpPatch, v(1, 2, 4)},
		{v(1, 2, 3), bumpMinor, v(1, 3, 0)},
		{v(1, 2, 3), bumpMajor, v(2, 0, 0)},
		// Before 1.0 a breaking change bumps the minor version.
		{v(0, 4, 1), bumpMajor, v(0, 5, 0)},
		{v(0, 0, 0), bumpMinor, v(0, 1, 0)},
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
