package main

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// Section is a group of pages in the sidebar.
type Section struct {
	Title string
	Pages []NavItem
}

// NavItem is one sidebar link to a page (path relative to the site dir).
type NavItem struct {
	Title string
	Path  string // "guides/kubernetes.md"
}

var navLink = regexp.MustCompile(`^-\s+\[([^\]]+)\]\(([^)\s]+\.md)\)\s*$`)

// parseNav reads _nav.md: "## Section" lines and "- [Title](page.md)" lines.
func parseNav(src string) ([]Section, error) {
	var out []Section
	sc := bufio.NewScanner(strings.NewReader(src))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "## "):
			out = append(out, Section{Title: strings.TrimSpace(line[3:])})
		case strings.HasPrefix(line, "- "):
			m := navLink.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("_nav.md:%d: expected - [Title](page.md), got %q", n, line)
			}
			if len(out) == 0 {
				return nil, fmt.Errorf("_nav.md:%d: link before the first ## section", n)
			}
			last := &out[len(out)-1]
			last.Pages = append(last.Pages, NavItem{Title: m[1], Path: m[2]})
		default:
			return nil, fmt.Errorf("_nav.md:%d: unexpected line %q", n, line)
		}
	}
	return out, sc.Err()
}

// order flattens the nav into reading order (for previous/next links).
func order(nav []Section) []NavItem {
	var out []NavItem
	for _, s := range nav {
		out = append(out, s.Pages...)
	}
	return out
}
