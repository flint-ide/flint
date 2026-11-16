package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	tagPath = "/repos/flint-ide/flint/releases/tags/"

	// Release notes are capped: a release has a few dozen entries, and the
	// page that shows them must stay readable whatever GitHub answers.
	// Together they also keep the notes within what a session saves of a tab.
	maxNoteItems   = 100
	maxNoteTextLen = 240
)

// ErrNoRelease: GitHub has no published release for the tag asked for.
var ErrNoRelease = errors.New("no published release found on GitHub")

// NoteItem is one entry of the release notes.
type NoteItem struct {
	Text string `json:"text"` // "Pull asks which project to update"
	URL  string `json:"url"`  // its pull request, under https://github.com/flint-ide/flint/, or empty
}

// Notes is what changed in one release, sorted the way the "What's New" page
// shows it.
type Notes struct {
	Tag         string     `json:"tag"`
	Name        string     `json:"name"`
	URL         string     `json:"url"` // release page
	PublishedAt time.Time  `json:"publishedAt"`
	Features    []NoteItem `json:"features"`
	Fixes       []NoteItem `json:"fixes"`
	Other       []NoteItem `json:"other"`
}

// Empty reports whether the release says nothing about its changes.
func (n Notes) Empty() bool { return len(n.Features)+len(n.Fixes)+len(n.Other) == 0 }

var (
	// "* feat(git): pick a project by @someone in https://github.com/…/pull/7"
	noteHeading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	noteBullet  = regexp.MustCompile(`^\s*[*-]\s+(.+)$`)
	// The tail GitHub generates: a login (never a "/", so "@types/node" in a
	// title is left alone) followed by the pull request.
	noteAuthor = regexp.MustCompile(`\s+by\s+@[\w.\[\]-]+\s+in\s+https://\S+\s*$`)
	noteLink   = regexp.MustCompile(`\s+in\s+(https://\S+)\s*$`)
	// A Conventional Commits subject: type, optional scope, optional "!".
	noteSubject = regexp.MustCompile(`^([A-Za-z]+)(\([^)]*\))?!?:\s*(.+)$`)
	// The only links an entry keeps: a pull request or an issue of Flint.
	notePull = regexp.MustCompile(`^https://github\.com/flint-ide/flint/(pull|issues)/\d+$`)

	featureHeading = regexp.MustCompile(`\b(features?|new|added)\b`)
	fixHeading     = regexp.MustCompile(`\b(fix(es|ed)?|bugs?|bugfix(es)?)\b`)
)

// noteTypes are the Conventional Commits types an entry may start with, and
// the section each belongs to. Any other "word:" is part of the text
// ("macOS: menu shortcut").
var noteTypes = map[string]string{
	"feat": "feature", "feature": "feature",
	"fix": "fix", "perf": "fix",
	"build": "other", "chore": "other", "ci": "other", "deps": "other", "docs": "other",
	"refactor": "other", "revert": "other", "style": "other", "test": "other",
}

// ParseNotes sorts the body of a GitHub release (the generated "What's
// Changed" list, or hand-written notes with headings) into new features, bug
// fixes and everything else. An entry is placed by its Conventional Commits
// type (feat; fix and perf), and without one by the heading it stands under.
func ParseNotes(body string) (features, fixes, other []NoteItem) {
	section, count := "", 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r \t")
		if h := noteHeading.FindStringSubmatch(line); h != nil {
			section = strings.ToLower(strings.TrimSpace(h[1]))
			continue
		}
		m := noteBullet.FindStringSubmatch(line)
		// "New Contributors" lists people, not changes.
		if m == nil || strings.Contains(section, "contributor") || count >= maxNoteItems {
			continue
		}
		kind, item := parseNote(m[1])
		if !strings.ContainsFunc(item.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			continue // empty, or a "* * *" rule
		}
		if kind == "" {
			kind = sectionKind(section)
		}
		count++
		switch kind {
		case "feature":
			features = append(features, item)
		case "fix":
			fixes = append(fixes, item)
		default:
			other = append(other, item)
		}
	}
	return features, fixes, other
}

// parseNote reads one list entry: its kind ("feature", "fix", "other", or ""
// when the text has no Conventional Commits type) and what to show.
func parseNote(entry string) (kind string, item NoteItem) {
	text := entry
	if m := noteLink.FindStringSubmatch(entry); m != nil {
		item.URL = pullURL(m[1])
	}
	if noteAuthor.MatchString(text) {
		text = noteAuthor.ReplaceAllString(text, "")
	} else if item.URL != "" {
		text = noteLink.ReplaceAllString(text, "") // the link moved to URL
	}
	if m := noteSubject.FindStringSubmatch(text); m != nil {
		if known, ok := noteTypes[strings.ToLower(m[1])]; ok {
			// A commit subject starts in lower case; a written sentence is left as it is.
			kind, text = known, capitalize(m[3])
		}
	}
	item.Text = cleanText(text, maxNoteTextLen)
	return kind, item
}

// sectionKind places an entry without a type by its heading.
func sectionKind(section string) string {
	switch {
	case featureHeading.MatchString(section):
		return "feature"
	case fixHeading.MatchString(section):
		return "fix"
	}
	return "other"
}

// pullURL returns link when it is a pull request or an issue of the Flint
// repository, else "".
func pullURL(link string) string {
	if !notePull.MatchString(link) || SafeURL(link) != link {
		return ""
	}
	return link
}

func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// apiNotes is the part of a release document the notes are read from.
type apiNotes struct {
	apiRelease
	Body string `json:"body"`
}

// Notes fetches the release notes of tag (one anonymous GET, as Latest).
func (c *Client) Notes(ctx context.Context, tag string) (Notes, error) {
	if !IsRelease(tag) {
		return Notes{}, fmt.Errorf("not a release version: %q", cleanText(tag, 40))
	}
	resp, err := c.get(ctx, tagPath+url.PathEscape(tag))
	if err != nil {
		return Notes{}, err
	}
	defer resp.Body.Close()
	var api apiNotes
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&api); err != nil {
		return Notes{}, fmt.Errorf("decode release: %w", err)
	}
	if api.Draft || api.TagName != tag {
		return Notes{}, ErrNoRelease
	}
	n := Notes{
		Tag:         api.TagName,
		Name:        cleanText(api.Name, maxNameLen),
		URL:         SafeURL(api.HTMLURL),
		PublishedAt: api.PublishedAt,
	}
	n.Features, n.Fixes, n.Other = ParseNotes(api.Body)
	return n, nil
}
