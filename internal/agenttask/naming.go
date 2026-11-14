// Package agenttask models agent tasks: a task description handed to a coding
// agent that works on its own branch in its own git worktree. It names the
// branch and folder, derives the task's state from the agent session's
// events, and keeps the per-repository list Flint remembers in state.json.
// Git and the agent session themselves are driven by internal/git and
// internal/agentsession.
package agenttask

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxSlug  = 40 // characters of the description in a branch name
	maxTitle = 80 // runes of a task's title
	// maxUnique bounds the "-2", "-3"… suffixes tried for a free name.
	maxUnique = 100
)

// fold spells common accented Latin letters in ASCII, so a description in
// Polish, German or French still gives a readable branch name.
var fold = map[rune]string{
	'ą': "a", 'ä': "a", 'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'å': "a",
	'ć': "c", 'ç': "c", 'č': "c",
	'ę': "e", 'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'ě': "e",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i",
	'ł': "l",
	'ń': "n", 'ñ': "n", 'ň': "n",
	'ó': "o", 'ö': "o", 'ò': "o", 'ô': "o", 'õ': "o", 'ø': "o",
	'ś': "s", 'š': "s", 'ß': "ss",
	'ú': "u", 'ü': "u", 'ù': "u", 'û': "u", 'ů': "u",
	'ý': "y", 'ÿ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
	'ř': "r", 'ť': "t", 'ď': "d",
}

// Slug turns the first line of a task description into a branch-name part:
// lowercase ASCII letters and digits joined by single hyphens, at most
// maxSlug characters (cut at a word boundary when there is one). It is
// "task" when nothing usable is left.
func Slug(description string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(description), "\n")
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(line) {
		s := ""
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			s = string(r)
		default:
			s = fold[r]
		}
		if s == "" {
			dash = b.Len() > 0
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(s)
	}
	slug := b.String()
	if len(slug) > maxSlug {
		cut := slug[:maxSlug]
		if i := strings.LastIndexByte(cut, '-'); i > 0 && slug[maxSlug] != '-' {
			cut = cut[:i]
		}
		slug = strings.TrimSuffix(cut, "-")
	}
	if slug == "" {
		return "task"
	}
	return slug
}

// BranchName is the branch of a task before it is made unique: prefix and
// the description's slug, and for a task of a group the agent's id too, so
// the branches of one comparison tell the agents apart
// ("agent/add-retry-codex").
func BranchName(prefix, description, agent string, grouped bool) string {
	name := prefix + Slug(description)
	if grouped {
		name += "-" + Slug(agent)
	}
	return name
}

// Title is the description's first non-empty line with runs of spaces
// collapsed, cut to maxTitle runes.
func Title(description string) string {
	for _, line := range strings.Split(description, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > maxTitle {
			line = string(r[:maxTitle-1]) + "…"
		}
		return line
	}
	return "Agent task"
}

// Unique returns name, or name-2, name-3… the first one taken reports free.
func Unique(name string, taken func(string) bool) (string, error) {
	for n := 1; n <= maxUnique; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", name, n)
		}
		if !taken(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("no free branch name for " + name + ": remove some old tasks or branches")
}

// WorktreePath is where the worktree of branch goes. With no folder set it
// follows the branch widget's convention, "<repo>.worktrees/<branch>" next to
// the main checkout; otherwise "<dir>/<repo>/<branch>" ("~/" is the home
// folder). Slashes in the branch become hyphens.
func WorktreePath(dir, home, mainCheckout, branch string) string {
	safe := strings.NewReplacer("/", "-", " ", "-").Replace(branch)
	repo := filepath.Base(mainCheckout)
	if dir == "" {
		return filepath.Join(filepath.Dir(mainCheckout), repo+".worktrees", safe)
	}
	return filepath.Join(ExpandHome(dir, home), repo, safe)
}

// ExpandHome replaces a leading "~" or "~/" with home.
func ExpandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return path
}
