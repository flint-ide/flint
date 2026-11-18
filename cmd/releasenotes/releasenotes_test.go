package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/update"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*/notes.md")

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// v0.7.0 as it was released: one pull request (#83) with every commit, and
// the body its author would have written with the template's Release notes
// section. notes.md is the body for the release page.
func TestGoldenV070(t *testing.T) {
	var commits []Commit
	readJSON(t, "testdata/v0.7.0/commits.json", &commits)
	f, err := os.Open("testdata/v0.7.0/prs.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	prs, err := readPRs(f)
	if err != nil {
		t.Fatal(err)
	}
	got := Render(Input{Repo: "flint-ide/flint", Version: "v0.7.0", Previous: "v0.6.0", Commits: commits, PRs: prs})
	golden := "testdata/v0.7.0/notes.md"
	if *updateGolden {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("notes differ from %s (go test ./cmd/releasenotes -update rewrites it):\n%s", golden, got)
	}

	// The template's comment is gone; the sections are in order.
	if strings.Contains(got, "<!--") || strings.Contains(got, "What users will notice") {
		t.Error("the template comment leaked into the notes")
	}
	order := []string{"## Highlights", "### Agent definitions, model tiers and a backlog delegated to agents", "#### Defined agents", "#### Model tiers", "#### Backlog", "## Upgrade notes", "## All changes", "### Features", "**Full Changelog**: https://github.com/flint-ide/flint/compare/v0.6.0...v0.7.0"}
	last := -1
	for _, s := range order {
		i := strings.Index(got, s)
		if i <= last {
			t.Errorf("%q missing or out of order", s)
		}
		last = i
	}

	// Flint's "What's New" page reads the same body: it must list the pull
	// request as a feature, and nothing from the prose.
	features, fixes, other := update.ParseNotes(got)
	want83 := update.NoteItem{Text: "Agent definitions, model tiers and a backlog delegated to agents", URL: "https://github.com/flint-ide/flint/pull/83"}
	if len(features) != 1 || features[0] != want83 || len(fixes) != 0 || len(other) != 0 {
		t.Errorf("What's New: features %+v, fixes %+v, other %+v", features, fixes, other)
	}
}

func TestNotesOf(t *testing.T) {
	const comment = "<!--\nWhat users will notice.\n### Upgrade notes\n-->\n"
	cases := []struct {
		name, body, notes, upgrade string
	}{
		{"no section", "## What and why\n\nRefactor.\n", "", ""},
		{"empty", "## Release notes\n\n" + comment + "\n## Other\n", "", ""},
		{"none", "## Release notes\n" + comment + "None.\n", "", ""},
		{"n/a", "## Release notes\r\n\r\n_N/A_\r\n", "", ""},
		{"dash", "## Release notes\n-\n", "", ""},
		{"text", "## What\nx\n## Release notes\n" + comment + "\nSearch is **faster**.\n\n## How it was tested\nBy hand.\n", "Search is **faster**.", ""},
		{"case and colon", "# RELEASE NOTES:\nIt works.", "It works.", ""},
		{"first section only", "## Release notes\nOne.\n## Release notes\nTwo.\n", "One.", ""},
		{"heading in a fence is not one", "```\n## Release notes\n```\n## Release notes\nReal.\n", "Real.", ""},
		{"comment inline", "## Release notes\nShown <!-- hidden --> text.\n", "Shown  text.", ""},
		{"unclosed comment", "## Release notes\nShown.\n<!-- the rest\n## More\n", "Shown.", ""},
		{"comment in code stays", "## Release notes\n```html\n<!-- x -->\n```\n", "```html\n<!-- x -->\n```", ""},
		{
			"demoted headings", "## Release notes\n### Big\ntext\n#### Small\n###### Tiny\n## Next section\n",
			"#### Big\ntext\n##### Small\n###### Tiny", "",
		},
		{"deep headings rise", "## Release notes\n##### Deep\n###### Deeper\n", "#### Deep\n##### Deeper", ""},
		{"setext", "## Release notes\nTitle\n=====\n\ntext\n\nSub\n---\n", "#### Title\n\ntext\n\n##### Sub", ""},
		{"setext under a paragraph", "## Release notes\nline one\nline two\n===\n", "line one\nline two\n\n===", ""},
		{"rule", "## Release notes\ntext\n\n---\n\nmore\n", "text\n\n---\n\nmore", ""},
		{
			"upgrade notes", "## Release notes\nNew thing.\n\n### Upgrade notes\n\n- Rename `a` to `b`.\n\n### Details\nMore.\n",
			"New thing.\n\n#### Details\nMore.", "+ Rename `a` to `b`.",
		},
		{"upgrade notes only", "## Release notes\n### Upgrade Notes:\nRun it again.\n", "", "Run it again."},
		{"upgrade notes none", "## Release notes\nX.\n### Upgrade notes\nnone\n", "X.", ""},
		{"upgrade headings", "## Release notes\n### Upgrade notes\n#### Settings\nx\n", "", "### Settings\nx"},
		{"unclosed fence", "## Release notes\n```go\nfunc main() {\n## not a heading\n", "```go\nfunc main() {\n## not a heading\n```", ""},
		{"tilde fence", "## Release notes\n~~~~\n- item\n<b>\n", "~~~~\n- item\n<b>\n~~~~", ""},
		{"html", "## Release notes\n<details><summary>x</summary>\n\n<img src=x onerror=alert(1)>\n", "&lt;details>&lt;summary>x&lt;/summary>\n\n&lt;img src=x onerror=alert(1)>", ""},
		{"autolink", "## Release notes\nSee <https://example.com/a>.\n", "See https://example.com/a.", ""},
		{"html in code span stays", "## Release notes\nUse `<div>` and ``a ` <b>``.\n", "Use `<div>` and ``a ` <b>``.", ""},
		{
			"mentions", "## Release notes\nThanks @alice, @flint-ide/core and (@bob-2). Mail a@b.com, see https://x.dev/@carol, `@dave` in code.\n",
			"Thanks `@alice`, `@flint-ide/core` and (`@bob-2`). Mail a@b.com, see https://x.dev/@carol, `@dave` in code.", "",
		},
		{"mention at line start and in a heading", "## Release notes\n### By @erin\n@frank did it\n", "#### By `@erin`\n`@frank` did it", ""},
		{
			"list markers", "## Release notes\n* one\n- two\n  - nested\n+ three\n1. four\n* * *\n- [ ] task\n",
			"+ one\n+ two\n  + nested\n+ three\n1. four\n* * *\n+ [ ] task", "",
		},
		{"blank lines collapse", "## Release notes\n\n\na\n\n\n\nb\n\n", "a\n\nb", ""},
		{"controls and bidi", "## Release notes\na\x00b\u202ec\x1b[31m\n", "abc[31m", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notes, upgrade := notesOf(c.body)
			if notes != c.notes || upgrade != c.upgrade {
				t.Errorf("notesOf:\nnotes   %q\nwant    %q\nupgrade %q\nwant    %q", notes, c.notes, upgrade, c.upgrade)
			}
		})
	}
}

func TestNotesOfBounds(t *testing.T) {
	long := "## Release notes\n```\n" + strings.Repeat("a line of code\n", 2000)
	notes, _ := notesOf(long)
	if len(notes) > maxHighlight+100 {
		t.Errorf("notes are %d bytes", len(notes))
	}
	if !strings.HasSuffix(notes, "```\n\n*The rest is in the pull request.*") {
		t.Errorf("a cut fence must be closed before the note: …%q", notes[len(notes)-80:])
	}
	// One line longer than the limit, with multi-byte characters.
	notes, _ = notesOf("## Release notes\n" + strings.Repeat("é", maxPRBody))
	if len(notes) > maxHighlight+100 || !strings.Contains(notes, "é") || strings.ContainsRune(notes, '\uFFFD') {
		t.Errorf("long line: %d bytes", len(notes))
	}
}

func TestTitle(t *testing.T) {
	cases := map[string]string{
		"agent definitions":           "Agent definitions",
		"éclair":                      "Éclair",
		"  two\n lines\t":             "Two lines",
		"# not a heading":             `\# not a heading`,
		"> not a quote":               `\> not a quote`,
		"- not a list":                `\- not a list`,
		"1. not ordered":              `1\. not ordered`,
		"```not a fence":              "\\```not a fence",
		"thanks @alice":               "Thanks `@alice`",
		"fix <details> rendering":     "Fix &lt;details> rendering",
		"keep `<T>` in code":          "Keep `<T>` in code",
		"":                            "(no title)",
		strings.Repeat("x", 500):      strings.Repeat("X", 1) + strings.Repeat("x", maxTitle-2) + "…",
		"bidi \u202egnp.exe controls": "Bidi gnp.exe controls",
	}
	for in, want := range cases {
		if got := title(in); got != want {
			t.Errorf("title(%q) = %q; want %q", in, got, want)
		}
	}
}

func pr(n int, title, login, body string) PullRequest {
	return PullRequest{Number: n, Title: title, Author: login0(login), Body: body}
}

func login0(l string) login { return login{Login: l} }

func TestRender(t *testing.T) {
	in := Input{
		Repo: "flint-ide/flint", Version: "v1.3.0", Previous: "v1.2.0",
		Commits: []Commit{
			{SHA: "aaaaaaa1111", Message: "feat: search in files", PR: 10},
			{SHA: "bbbbbbb2222", Message: "fix: crash on open", PR: 11},
			{SHA: "ccccccc3333", Message: "refactor: tidy\n\nBREAKING CHANGE: settings.x is gone", PR: 12},
			{SHA: "ddddddd4444", Message: "docs: typo"},
			{SHA: "eeeeeee5555", Message: "fix: direct to main"},
			{SHA: "fffffff6666", Message: "perf(git): faster status", PR: 13},
			{SHA: "0000000aaaa", Message: "build(deps): bump x", PR: 14},
			{SHA: "1111111bbbb", Message: "Tidy the welcome screen", PR: 15},
			{SHA: "2222222cccc", Message: "feat: from a pull request we couldn't read", PR: 99},
			{SHA: "3333333dddd", Message: "feat: part of an untyped pull request", PR: 16},
			{SHA: "4444444eeee", Message: "chore: commit of #16", PR: 16},
			{SHA: "5555555ffff", Message: "feat!: drop macOS 11"},
		},
		PRs: []PullRequest{
			pr(10, "feat(search): search in files", "alice", "## Release notes\nPress ⇧⌘F.\n"),
			pr(11, "fix: crash on open", "bob", "## Release notes\nnone\n"),
			pr(12, "refactor: settings", "bob", "## Release notes\n### Upgrade notes\nMove `x` to `y`.\n"),
			pr(13, "perf(git): faster status", "carol", ""),
			{Number: 14, Title: "build(deps): bump x", Author: login0("app/dependabot")},
			pr(15, "Tidy the welcome screen", "bad login!", ""),
			pr(16, "Untyped title", "dave", ""),
			pr(20, "docs: merged elsewhere", "erin", ""),
		},
	}
	got := Render(in)
	want := `## Highlights

### Search in files

Press ⇧⌘F.

Pull request: [#10](https://github.com/flint-ide/flint/pull/10)

## Upgrade notes

Move ` + "`x` to `y`" + `.

Pull request: [#12](https://github.com/flint-ide/flint/pull/12)

## All changes

### Breaking changes

* Settings by @bob in https://github.com/flint-ide/flint/pull/12
* Drop macOS 11 (5555555)

### Features

* Search in files by @alice in https://github.com/flint-ide/flint/pull/10
* From a pull request we couldn't read (2222222)
* Untyped title by @dave in https://github.com/flint-ide/flint/pull/16

### Fixes

* Crash on open by @bob in https://github.com/flint-ide/flint/pull/11
* Direct to main (eeeeeee)

### Performance

* Faster status by @carol in https://github.com/flint-ide/flint/pull/13

### Other changes

* Tidy the welcome screen in https://github.com/flint-ide/flint/pull/15

Not listed: 3 maintenance changes (docs 2, dependencies 1).

**Full Changelog**: https://github.com/flint-ide/flint/compare/v1.2.0...v1.3.0
`
	if got != want {
		t.Errorf("Render:\n%s\nwant:\n%s", got, want)
	}

	// The What's New page sorts the list by its headings and links the pull requests.
	features, fixes, other := update.ParseNotes(got)
	if len(features) != 3 || features[0].URL != "https://github.com/flint-ide/flint/pull/10" || features[0].Text != "Search in files" {
		t.Errorf("What's New features: %+v", features)
	}
	if len(fixes) != 2 || fixes[0].Text != "Crash on open" {
		t.Errorf("What's New fixes: %+v", fixes)
	}
	if len(other) != 4 { // breaking changes, performance, other
		t.Errorf("What's New other: %+v", other)
	}
}

func TestRenderFirstReleaseAndNothing(t *testing.T) {
	got := Render(Input{Repo: "o/r", Version: "v0.1.0", Commits: []Commit{{SHA: "abc", Message: "chore: init"}}})
	want := "## All changes\n\nNot listed: 1 maintenance change (chore 1).\n\n**Full Changelog**: https://github.com/o/r/commits/v0.1.0\n"
	if got != want {
		t.Errorf("first release:\n%q\nwant\n%q", got, want)
	}
	got = Render(Input{Repo: "o/r", Version: "v0.1.1", Previous: "v0.1.0"})
	if !strings.Contains(got, "Nothing to list.") || strings.Contains(got, "## Highlights") || strings.Contains(got, "## Upgrade notes") {
		t.Errorf("empty release:\n%s", got)
	}
}

func TestRenderBounds(t *testing.T) {
	var in Input
	in.Repo, in.Version, in.Previous = "o/r", "v2.0.0", "v1.0.0"
	big := "## Release notes\n" + strings.Repeat("Some words about it, @someone. <b>\n", 150) + "### Upgrade notes\n" + strings.Repeat("Do this.\n", 300)
	for i := 1; i <= 600; i++ {
		in.PRs = append(in.PRs, pr(i, "feat: "+strings.Repeat("long title ", 30), "alice", big))
		in.Commits = append(in.Commits, Commit{SHA: fmt.Sprintf("%040d", i), Message: "feat: x", PR: i})
	}
	got := Render(in)
	if len(got) > maxBody {
		t.Fatalf("body is %d bytes", len(got))
	}
	for _, want := range []string{"## Highlights", "more pull requests did not fit here", "## Upgrade notes", "Read the upgrade notes in [#", "upgrade notes of ", "### Features", "more changes did not fit here", "**Full Changelog**"} {
		if !strings.Contains(got, want) {
			t.Errorf("bounded body lacks %q", want)
		}
	}
	if strings.Contains(got, "<b>") || strings.Contains(got, " @someone") {
		t.Error("HTML or a mention got through")
	}
}

func TestReadPRs(t *testing.T) {
	prs, err := readPRs(strings.NewReader(`[{"number":7,"title":"t","body":null,"html_url":"x","user":{"login":"bob"},"extra":1},{"number":8,"author":{"login":"app/renovate"}}]`))
	if err != nil || len(prs) != 2 || prs[0].author() != "bob" || prs[1].author() != "renovate[bot]" {
		t.Fatalf("readPRs = %+v, %v", prs, err)
	}
	for name, in := range map[string]string{
		"malformed": `[{"number":1,`,
		"object":    `{"number":1}`,
		"no number": `[{"title":"x"}]`,
		"bad type":  `[{"number":"1"}]`,
	} {
		if _, err := readPRs(strings.NewReader(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := readPRs(bytes.NewReader(make([]byte, maxInput+10))); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too big: %v", err)
	}
	if prs, err := readPRs(strings.NewReader("[]")); err != nil || len(prs) != 0 {
		t.Errorf("empty: %v, %v", prs, err)
	}
}

// A real repository: a merge commit of a pull request, a squashed one and a
// direct commit, the release tags, and the command's own flags.
func TestRunOnARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(msg string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		g("add", "f")
		g("commit", "-q", "-m", msg)
	}
	g("init", "-q", "-b", "main")
	commit("feat: first")
	g("tag", "v0.1.0")
	commit("fix: direct")
	g("checkout", "-q", "-b", "topic")
	commit("feat: on a branch")
	commit("docs: on a branch")
	g("checkout", "-q", "main")
	g("merge", "-q", "--no-ff", "-m", "Merge pull request #5 from someone/topic\n\nfeat: a branch", "topic")
	commit("feat: squashed (#6)")

	t.Chdir(dir)
	runIt := func(o options, stdin string) (string, error) {
		var out, errOut bytes.Buffer
		o.repo = cmpOr(o.repo, "o/r")
		o.to = cmpOr(o.to, "HEAD")
		err := run(o, strings.NewReader(stdin), &out, &errOut)
		return out.String(), err
	}

	out, err := runIt(options{version: "v0.2.0", listPRs: true}, "")
	if err != nil || out != "5\n6\n" {
		t.Fatalf("-list-prs = %q, %v", out, err)
	}
	prs := `[{"number":5,"title":"feat: a branch","author":{"login":"alice"},"body":"## Release notes\nBranches."}]`
	out, err = runIt(options{version: "v0.2.0", prs: "-"}, prs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"### A branch\n\nBranches.",
		"* A branch by @alice in https://github.com/o/r/pull/5\n",
		"* Squashed (#6) (", // #6 wasn't given: its commit stands for itself
		"* Direct (",
		"compare/v0.1.0...v0.2.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("notes lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Not listed") {
		t.Errorf("the docs commit of #5 was counted on its own:\n%s", out)
	}

	// The release itself tagged: the release before is still v0.1.0.
	g("tag", "v0.2.0")
	if out, err = runIt(options{version: "v0.2.0", to: "v0.2.0"}, ""); err != nil || !strings.Contains(out, "compare/v0.1.0...v0.2.0") {
		t.Errorf("rebuild of v0.2.0: %v\n%s", err, out)
	}
	// The first release: no tag before it.
	if out, err = runIt(options{version: "v0.1.0", to: "v0.1.0"}, ""); err != nil || !strings.Contains(out, "commits/v0.1.0") || !strings.Contains(out, "* First (") {
		t.Errorf("first release: %v\n%s", err, out)
	}

	for name, o := range map[string]options{
		"bad version": {version: "1.0"},
		"bad repo":    {version: "v1.0.0", repo: "not a repo"},
		"bad ref":     {version: "v1.0.0", to: "--output=/tmp/x"},
		"no ref":      {version: "v1.0.0", to: "nope"},
		"bad from":    {version: "v1.0.0", from: "HEAD~1"},
		"bad json":    {version: "v1.0.0", prs: "-"},
	} {
		if _, err := runIt(o, "{"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func cmpOr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// The pull request template, filled in by nobody, has no notes: its comment
// (with its "### Upgrade notes" example) is removed.
func TestTemplateHasNoNotes(t *testing.T) {
	data, err := os.ReadFile("../../.github/PULL_REQUEST_TEMPLATE.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## Release notes") {
		t.Fatal("the template has no Release notes section")
	}
	if notes, upgrade := notesOf(string(data)); notes != "" || upgrade != "" {
		t.Errorf("template: notes %q, upgrade %q", notes, upgrade)
	}
	filled := strings.Replace(string(data), "-->\n\n## Checklist", "-->\n\nSearch is faster.\n\n### Upgrade notes\n\nNothing to do.\n\n## Checklist", 1)
	if notes, upgrade := notesOf(filled); notes != "Search is faster." || upgrade != "Nothing to do." {
		t.Errorf("filled template: notes %q, upgrade %q", notes, upgrade)
	}
}
