package main

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"flint/internal/release"
)

// Bounds, in bytes. GitHub refuses a release body over 125,000 characters.
const (
	maxBody       = 120_000 // the whole release body
	maxNotesTotal = 90_000  // all highlights and upgrade notes together
	maxHighlight  = 10_000  // the notes of one pull request
	maxUpgrade    = 4_000   // the upgrade notes of one pull request
	maxPRBody     = 65_536  // what is read of a pull request's body
	maxTitle      = 200     // a title, in characters
)

// PullRequest is one merged pull request, as `gh pr view --json` (author) or
// the REST API (user) gives it. Other fields are ignored.
type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author login  `json:"author"`
	User   login  `json:"user"`
}

type login struct {
	Login string `json:"login"`
}

// Commit is one commit of the release (no merge commits).
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	PR      int    `json:"pr"` // the pull request that brought it to main; 0 when none
}

// Input is everything the notes are made of.
type Input struct {
	Repo     string // owner/name
	Version  string // vX.Y.Z
	Previous string // the release before, vX.Y.Z; "" for the first release
	Commits  []Commit
	PRs      []PullRequest
}

type kind int

const (
	kindBreaking kind = iota
	kindFeature
	kindFix
	kindPerf
	kindOther
	kindMaintenance
)

// The list's sections. Flint's "What's New" page sorts the items it lists by
// the words of these headings (features → New Features, fixes → Bug Fixes,
// the rest → Other Changes): keep them.
var sectionTitles = map[kind]string{
	kindBreaking: "Breaking changes",
	kindFeature:  "Features",
	kindFix:      "Fixes",
	kindPerf:     "Performance",
	kindOther:    "Other changes",
}

// maintenance is what a type that users don't see is counted as.
var maintenance = map[string]string{
	"chore": "chore", "docs": "docs", "ci": "CI", "test": "tests", "tests": "tests",
	"refactor": "refactoring", "build": "build", "style": "style",
}

const depsLabel = "dependencies"

// change is one line of the list, and for a pull request its notes.
type change struct {
	pos     int // order: its newest commit
	kind    kind
	label   string // for maintenance: what it counts as
	text    string // Markdown
	ref     string // " by @login in URL", " in URL" or " (abc1234)"
	pr      int
	notes   string
	upgrade string
}

var loginRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})(?:\[bot\])?$`)

// author is the pull request's author as a GitHub login, or "".
func (p PullRequest) author() string {
	l := cmp.Or(p.Author.Login, p.User.Login)
	if bot, ok := strings.CutPrefix(l, "app/"); ok { // gh's name for a GitHub App
		l = bot + "[bot]"
	}
	if !loginRe.MatchString(l) {
		return ""
	}
	return l
}

// typeRank picks the type of a pull request whose title has none from its
// commits.
func typeRank(t string) int {
	switch t {
	case "feat", "feature":
		return 4
	case "fix":
		return 3
	case "perf":
		return 2
	case "":
		return 0
	}
	return 1
}

func classify(c release.Commit, author string) (kind, string) {
	switch {
	case c.Breaking:
		return kindBreaking, ""
	case c.Scope == "deps" || c.Scope == "deps-dev" || author == "dependabot[bot]" || author == "renovate[bot]":
		return kindMaintenance, depsLabel
	}
	switch c.Type {
	case "feat", "feature":
		return kindFeature, ""
	case "fix":
		return kindFix, ""
	case "perf":
		return kindPerf, ""
	}
	if label, ok := maintenance[c.Type]; ok {
		return kindMaintenance, label
	}
	return kindOther, ""
}

// changes sorts the input into the release's changes, in the order they
// reached main.
func changes(in Input) []change {
	prs := map[int]PullRequest{}
	for _, p := range in.PRs {
		if _, dup := prs[p.Number]; !dup && p.Number > 0 {
			prs[p.Number] = p
		}
	}
	type prInfo struct {
		pos      int
		commits  []release.Commit
		breaking bool
	}
	info := map[int]*prInfo{}
	var out []change
	for i, c := range in.Commits {
		pc := release.ParseCommit(c.Message)
		if _, ok := prs[c.PR]; ok {
			pi := info[c.PR]
			if pi == nil {
				pi = &prInfo{}
				info[c.PR] = pi
			}
			pi.pos = i
			pi.commits = append(pi.commits, pc)
			pi.breaking = pi.breaking || pc.Breaking
			continue
		}
		// A commit without a (known) pull request stands for itself.
		k, label := classify(pc, "")
		out = append(out, change{pos: i, kind: k, label: label, text: title(pc.Description), ref: " (" + shortSHA(c.SHA) + ")"})
	}
	extra := len(in.Commits) // pull requests without commits in the range go last
	for _, n := range slices.Sorted(maps.Keys(prs)) {
		p := prs[n]
		pi := info[n]
		if pi == nil {
			pi = &prInfo{pos: extra}
			extra++
		}
		pc := release.ParseCommit(normalizeTitle(p.Title))
		if pc.Type == "" {
			best := ""
			for _, c := range pi.commits {
				if typeRank(c.Type) > typeRank(best) {
					best = c.Type
				}
			}
			pc.Type = best
		}
		pc.Breaking = pc.Breaking || pi.breaking
		a := p.author()
		k, label := classify(pc, a)
		ch := change{pos: pi.pos, kind: k, label: label, text: title(pc.Description), pr: n}
		url := pullURL(in.Repo, n)
		if a != "" {
			ch.ref = " by @" + a + " in " + url
		} else {
			ch.ref = " in " + url
		}
		ch.notes, ch.upgrade = notesOf(p.Body)
		out = append(out, ch)
	}
	slices.SortStableFunc(out, func(a, b change) int { return cmp.Compare(a.pos, b.pos) })
	return out
}

// notesOf reads the "## Release notes" section of a pull request's body: the
// highlight and the upgrade notes ("### Upgrade notes" inside it), cleaned.
func notesOf(body string) (notes, upgrade string) {
	lines := stripComments(normalize(body, maxPRBody))
	sec, _, ok := section(lines, func(level int, text string) bool {
		return level <= 3 && strings.EqualFold(strings.TrimSuffix(text, ":"), "release notes")
	})
	if !ok {
		return "", ""
	}
	up, rest, _ := section(sec, func(level int, text string) bool {
		t := strings.ToLower(strings.TrimSuffix(text, ":"))
		return t == "upgrade notes" || t == "upgrade note" || t == "upgrading"
	})
	if !isNone(rest) {
		notes = clean(rest, 4, maxHighlight)
	}
	if !isNone(up) {
		upgrade = clean(up, 3, maxUpgrade)
	}
	return notes, upgrade
}

// normalizeTitle is a title on one line, without control characters.
func normalizeTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || isInvisible(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var orderedRe = regexp.MustCompile(`^([0-9]+)([.)])`)

// title makes a title or commit subject the text of a list item or a heading:
// capitalised, bounded, and unable to start a block of its own.
func title(s string) string {
	s = normalizeTitle(s)
	if r := []rune(s); len(r) > maxTitle {
		s = string(r[:maxTitle-1]) + "…"
	}
	if s == "" {
		return "(no title)"
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	s = inline(string(r))
	switch {
	case strings.ContainsRune("#>+-*=|", rune(s[0])), strings.HasPrefix(s, "```"), strings.HasPrefix(s, "~~~"):
		s = `\` + s
	case orderedRe.MatchString(s):
		s = orderedRe.ReplaceAllString(s, `$1\$2`)
	}
	return s
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func pullURL(repo string, n int) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", repo, n)
}

// Render writes the release body.
func Render(in Input) string {
	all := changes(in)
	var b strings.Builder
	budget := maxNotesTotal

	// Highlights: the pull requests' own words.
	var highlights []string
	skipped := 0
	for _, c := range all {
		if c.notes == "" {
			continue
		}
		block := fmt.Sprintf("### %s\n\n%s\n\nPull request: [#%d](%s)\n\n", c.text, c.notes, c.pr, pullURL(in.Repo, c.pr))
		if len(block) > budget {
			skipped++
			continue
		}
		budget -= len(block)
		highlights = append(highlights, block)
	}
	if len(highlights) > 0 || skipped > 0 {
		b.WriteString("## Highlights\n\n")
		for _, h := range highlights {
			b.WriteString(h)
		}
		if skipped > 0 {
			fmt.Fprintf(&b, "The notes of %s did not fit here; see %s under All changes.\n\n", plural(skipped, "more pull request"), them(skipped))
		}
	}

	// Upgrade notes: what to do or know when updating.
	var upgrades []string
	skipped = 0
	for _, c := range all {
		if c.upgrade == "" {
			continue
		}
		block := fmt.Sprintf("%s\n\nPull request: [#%d](%s)\n\n", c.upgrade, c.pr, pullURL(in.Repo, c.pr))
		if len(block) > budget {
			block = fmt.Sprintf("Read the upgrade notes in [#%d](%s).\n\n", c.pr, pullURL(in.Repo, c.pr))
		}
		if len(block) > budget {
			skipped++
			continue
		}
		budget -= len(block)
		upgrades = append(upgrades, block)
	}
	if len(upgrades) > 0 || skipped > 0 {
		b.WriteString("## Upgrade notes\n\n")
		for _, u := range upgrades {
			b.WriteString(u)
		}
		if skipped > 0 {
			fmt.Fprintf(&b, "The upgrade notes of %s did not fit here; see %s under All changes.\n\n", plural(skipped, "more pull request"), them(skipped))
		}
	}

	// All changes, one line each, by kind; maintenance only counted.
	b.WriteString("## All changes\n\n")
	counts := map[string]int{}
	listed, dropped := 0, 0
	for k := kindBreaking; k < kindMaintenance; k++ {
		var lines []string
		size := b.Len() + len(sectionTitles[k]) + 10
		for _, c := range all {
			if c.kind != k {
				continue
			}
			line := "* " + c.text + c.ref + "\n"
			if size+len(line)+1_000 > maxBody { // room for the lines below
				dropped++
				continue
			}
			size += len(line)
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n%s\n", sectionTitles[k], strings.Join(lines, ""))
		listed += len(lines)
	}
	for _, c := range all {
		if c.kind == kindMaintenance {
			counts[c.label]++
		}
	}
	if dropped > 0 {
		fmt.Fprintf(&b, "%s did not fit here; see the full changelog.\n\n", plural(dropped, "more change"))
	}
	if m := maintenanceLine(counts); m != "" {
		b.WriteString(m + "\n\n")
	} else if listed == 0 && dropped == 0 {
		b.WriteString("Nothing to list.\n\n")
	}

	if in.Previous != "" {
		fmt.Fprintf(&b, "**Full Changelog**: https://github.com/%s/compare/%s...%s\n", in.Repo, in.Previous, in.Version)
	} else {
		fmt.Fprintf(&b, "**Full Changelog**: https://github.com/%s/commits/%s\n", in.Repo, in.Version)
	}
	return cutBytes(b.String(), maxBody)
}

// maintenanceLine counts the changes users don't see:
// "Not listed: 5 maintenance changes (docs 3, CI 2)."
func maintenanceLine(counts map[string]int) string {
	total := 0
	labels := make([]string, 0, len(counts))
	for l, n := range counts {
		total += n
		labels = append(labels, l)
	}
	if total == 0 {
		return ""
	}
	slices.SortFunc(labels, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = fmt.Sprintf("%s %d", l, counts[l])
	}
	return fmt.Sprintf("Not listed: %s (%s).", plural(total, "maintenance change"), strings.Join(parts, ", "))
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}
