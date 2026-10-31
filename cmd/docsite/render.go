package main

import (
	"bytes"
	"fmt"
	"html"
	"path"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// md renders GitHub-flavored Markdown without raw HTML (pages are plain content).
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// Page is a rendered Markdown file.
type Page struct {
	Path     string // "guides/kubernetes.md"
	Title    string
	HTML     string
	Headings []Heading // h2 and h3, for the page outline and search
	Text     string    // plain text, for search
}

// Heading is an anchor inside a page.
type Heading struct {
	Level int    `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

var (
	h1Line     = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	headingTag = regexp.MustCompile(`<h([23]) id="([^"]+)">(.*?)</h[23]>`)
	firstH1    = regexp.MustCompile(`(?s)^\s*<h1[^>]*>.*?</h1>\s*`)
	mdHref     = regexp.MustCompile(`href="([^"#:]*\.md)(#[^"]*)?"`)
	callout    = regexp.MustCompile(`<blockquote>\s*<p><strong>(Note|Tip|Warning):</strong>`)
	anyTag     = regexp.MustCompile(`<[^>]+>`)
	spaces     = regexp.MustCompile(`\s+`)
)

// render turns one Markdown file into a page: .md links become .html,
// "> **Note:**" blockquotes become callouts, and the H1 becomes the title.
func render(rel, src string) (Page, error) {
	m := h1Line.FindStringSubmatch(src)
	if m == nil {
		return Page{}, fmt.Errorf("%s: no # title", rel)
	}
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return Page{}, fmt.Errorf("%s: %w", rel, err)
	}
	out := firstH1.ReplaceAllString(b.String(), "") // the template shows the title
	out = mdHref.ReplaceAllStringFunc(out, func(s string) string {
		sm := mdHref.FindStringSubmatch(s)
		return `href="` + strings.TrimSuffix(sm[1], ".md") + ".html" + sm[2] + `"`
	})
	out = callout.ReplaceAllStringFunc(out, func(s string) string {
		kind := callout.FindStringSubmatch(s)[1]
		return `<blockquote class="callout ` + strings.ToLower(kind) + `"><p><strong>` + kind + `:</strong>`
	})
	p := Page{Path: rel, Title: stripTags(m[1]), HTML: out}
	for _, h := range headingTag.FindAllStringSubmatch(out, -1) {
		level := 2
		if h[1] == "3" {
			level = 3
		}
		p.Headings = append(p.Headings, Heading{Level: level, ID: h[2], Text: stripTags(h[3])})
	}
	p.Text = stripTags(out)
	return p, nil
}

func stripTags(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(anyTag.ReplaceAllString(s, " ")), " "))
}

// htmlPath is where a page is written: "guides/kubernetes.md" → "guides/kubernetes.html".
func htmlPath(rel string) string { return strings.TrimSuffix(rel, ".md") + ".html" }

// rootPrefix leads from a page back to the site root ("../" per directory).
func rootPrefix(rel string) string {
	depth := strings.Count(path.Clean(rel), "/")
	return strings.Repeat("../", depth)
}

var linkTarget = regexp.MustCompile(`href="([^"]+)"`)

// brokenLinks lists links of a page to pages or anchors that don't exist.
func brokenLinks(p Page, pages map[string]Page) []string {
	var bad []string
	for _, m := range linkTarget.FindAllStringSubmatch(p.HTML, -1) {
		href := html.UnescapeString(m[1])
		if strings.Contains(href, ":") || strings.HasPrefix(href, "//") {
			continue // external
		}
		file, anchor, _ := strings.Cut(href, "#")
		target := p
		if file != "" {
			if !strings.HasSuffix(file, ".html") {
				bad = append(bad, href+" (not a page)")
				continue
			}
			rel := path.Clean(path.Join(path.Dir(p.Path), strings.TrimSuffix(file, ".html")+".md"))
			t, ok := pages[rel]
			if !ok {
				bad = append(bad, href)
				continue
			}
			target = t
		}
		if anchor != "" && !hasAnchor(target, anchor) {
			bad = append(bad, href+" (no such heading)")
		}
	}
	return bad
}

var imageSource = regexp.MustCompile(`<img src="([^"]+)"`)

// missingImages lists the page's local images that aren't in site/.
func missingImages(p Page, images map[string]string) []string {
	var bad []string
	for _, m := range imageSource.FindAllStringSubmatch(p.HTML, -1) {
		src := html.UnescapeString(m[1])
		if strings.Contains(src, ":") || strings.HasPrefix(src, "//") {
			continue // remote
		}
		if _, ok := images[path.Clean(path.Join(path.Dir(p.Path), src))]; !ok {
			bad = append(bad, src)
		}
	}
	return bad
}

func hasAnchor(p Page, id string) bool {
	return strings.Contains(p.HTML, ` id="`+id+`"`)
}
