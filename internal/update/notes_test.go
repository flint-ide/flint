package update

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const generatedNotes = "## What's Changed\r\n" +
	"* feat(git): pull asks which project to update by @alice in https://github.com/flint-ide/flint/pull/80\r\n" +
	"* fix: no size arithmetic in slice allocations by @bob in https://github.com/flint-ide/flint/pull/74\r\n" +
	"* perf!: faster status by @bob in https://github.com/flint-ide/flint/pull/75\r\n" +
	"* build(deps): Bump the npm-minor group by @dependabot[bot] in https://github.com/flint-ide/flint/pull/72\r\n" +
	"* Tidy the welcome screen by @alice in https://evil.example/pull/1\r\n" +
	"\r\n## New Contributors\r\n" +
	"* @alice made their first contribution in https://github.com/flint-ide/flint/pull/80\r\n" +
	"\r\n**Full Changelog**: https://github.com/flint-ide/flint/compare/v1.2.0...v1.3.0\r\n"

func TestParseNotes(t *testing.T) {
	features, fixes, other := ParseNotes(generatedNotes)
	wantFeatures := []NoteItem{{Text: "Pull asks which project to update", URL: "https://github.com/flint-ide/flint/pull/80"}}
	wantFixes := []NoteItem{
		{Text: "No size arithmetic in slice allocations", URL: "https://github.com/flint-ide/flint/pull/74"},
		{Text: "Faster status", URL: "https://github.com/flint-ide/flint/pull/75"},
	}
	wantOther := []NoteItem{
		{Text: "Bump the npm-minor group", URL: "https://github.com/flint-ide/flint/pull/72"},
		{Text: "Tidy the welcome screen"}, // a link outside the repository is dropped
	}
	if !reflect.DeepEqual(features, wantFeatures) {
		t.Errorf("features = %+v", features)
	}
	if !reflect.DeepEqual(fixes, wantFixes) {
		t.Errorf("fixes = %+v", fixes)
	}
	if !reflect.DeepEqual(other, wantOther) {
		t.Errorf("other = %+v", other)
	}
}

func TestParseNotesByHeading(t *testing.T) {
	features, fixes, other := ParseNotes("# v1.3.0\n\n### New Features\n- Release notes page\n\n### Bug Fixes\n- Toast position\n- fix: typed entry\n\n### Internal\n- Cleanup\n\nJust a paragraph.\n")
	if len(features) != 1 || features[0].Text != "Release notes page" {
		t.Errorf("features = %+v", features)
	}
	if len(fixes) != 2 || fixes[0].Text != "Toast position" || fixes[1].Text != "Typed entry" {
		t.Errorf("fixes = %+v", fixes)
	}
	if len(other) != 1 || other[0].Text != "Cleanup" {
		t.Errorf("other = %+v", other)
	}
}

func TestParseNotesKeepsTheTextOfAnEntry(t *testing.T) {
	body := "### Bug Fixes\n" +
		"- macOS: menu shortcut broken\n" + // not a commit type: stays, and stays a fix
		"- Fix crash caused by @types/node in https://github.com/flint-ide/flint/pull/9\n" +
		"- Crash on start in https://github.com/flint-ide/flint/compare/v1...v2\n" +
		"* * *\n" +
		"#42 was fixed\n" + // not a heading
		"- Still a fix\n" +
		"### Renewed internals\n" +
		"- Cleanup\n"
	features, fixes, other := ParseNotes(body)
	wantFixes := []NoteItem{
		{Text: "macOS: menu shortcut broken"},
		{Text: "Fix crash caused by @types/node", URL: "https://github.com/flint-ide/flint/pull/9"},
		{Text: "Crash on start in https://github.com/flint-ide/flint/compare/v1...v2"},
		{Text: "Still a fix"},
	}
	if !reflect.DeepEqual(fixes, wantFixes) {
		t.Errorf("fixes = %+v", fixes)
	}
	if len(features) != 0 || len(other) != 1 || other[0].Text != "Cleanup" {
		t.Errorf("features = %+v, other = %+v", features, other)
	}
}

func TestParseNotesIsBounded(t *testing.T) {
	body := strings.Repeat("* feat: "+strings.Repeat("x", 1000)+"\n", maxNoteItems+50)
	features, fixes, other := ParseNotes(body)
	if len(features) != maxNoteItems || len(fixes)+len(other) != 0 {
		t.Fatalf("items = %d/%d/%d, want %d features", len(features), len(fixes), len(other), maxNoteItems)
	}
	if n := len([]rune(features[0].Text)); n > maxNoteTextLen {
		t.Errorf("text length = %d, want at most %d", n, maxNoteTextLen)
	}
	if f, x, o := ParseNotes(""); f != nil || x != nil || o != nil {
		t.Error("an empty body has entries")
	}
}

func TestNotes(t *testing.T) {
	t.Run("reads the release of the tag", func(t *testing.T) {
		c, req := serve(t, http.StatusOK, `{"tag_name": "v1.3.0", "name": "Flint v1.3.0",
			"html_url": "https://github.com/flint-ide/flint/releases/tag/v1.3.0",
			"published_at": "2026-10-30T09:15:00Z",
			"body": "* feat: a by @x in https://github.com/flint-ide/flint/pull/1\n* fix: b"}`)
		n, err := c.Notes(context.Background(), "v1.3.0")
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.Path != "/repos/flint-ide/flint/releases/tags/v1.3.0" {
			t.Errorf("path = %q", req.URL.Path)
		}
		if n.Tag != "v1.3.0" || n.Name != "Flint v1.3.0" || n.URL != "https://github.com/flint-ide/flint/releases/tag/v1.3.0" || n.PublishedAt.IsZero() {
			t.Errorf("notes = %+v", n)
		}
		if len(n.Features) != 1 || len(n.Fixes) != 1 || n.Empty() {
			t.Errorf("entries = %+v / %+v", n.Features, n.Fixes)
		}
	})

	t.Run("no such release", func(t *testing.T) {
		c, _ := serve(t, http.StatusNotFound, `{}`)
		if _, err := c.Notes(context.Background(), "v9.9.9"); !errors.Is(err, ErrNoRelease) {
			t.Errorf("err = %v, want ErrNoRelease", err)
		}
	})

	t.Run("another release or a draft is not the answer", func(t *testing.T) {
		for _, body := range []string{`{"tag_name": "v2.0.0"}`, `{"tag_name": "v1.3.0", "draft": true}`} {
			c, _ := serve(t, http.StatusOK, body)
			if _, err := c.Notes(context.Background(), "v1.3.0"); !errors.Is(err, ErrNoRelease) {
				t.Errorf("%s: err = %v, want ErrNoRelease", body, err)
			}
		}
	})

	t.Run("asks only for release versions", func(t *testing.T) {
		c, req := serve(t, http.StatusOK, `{}`)
		if _, err := c.Notes(context.Background(), "../../other"); err == nil {
			t.Error("no error for a tag that is not a version")
		}
		if req.URL != nil {
			t.Error("a request was sent")
		}
	})
}
