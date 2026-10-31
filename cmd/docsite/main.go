// Command docsite builds Flint's user documentation (site/*.md) into a
// static website for GitHub Pages: one page per Markdown file, the sidebar
// from site/_nav.md, a search index, and the theme in theme/. It uses only
// goldmark (already a Flint dependency) and the standard library, and fails
// on broken links between pages.
//
//	go run ./cmd/docsite                 # site/ → _site/
//	go run ./cmd/docsite -serve :8090    # build, then preview at http://localhost:8090
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed theme
var theme embed.FS

func main() {
	src := flag.String("src", "site", "Markdown sources")
	out := flag.String("out", "_site", "output directory (replaced)")
	serve := flag.String("serve", "", "after building, serve the site on this address (e.g. :8090)")
	flag.Parse()
	n, err := build(*src, *out)
	if err != nil {
		log.Fatalf("docsite: %v", err)
	}
	fmt.Printf("docsite: %d pages → %s\n", n, *out)
	if *serve != "" {
		fmt.Printf("docsite: serving http://localhost%s\n", *serve)
		log.Fatal(http.ListenAndServe(*serve, http.FileServer(http.Dir(*out))))
	}
}

// view is what the page template receives.
type view struct {
	Page     Page
	Root     string // "../" back to the site root
	Nav      []navView
	Prev     *NavItem
	Next     *NavItem
	Outline  []Heading
	Body     template.HTML
	EditLink string
}

type navView struct {
	Title string
	Pages []navLinkView
}

type navLinkView struct {
	Title  string
	Href   string
	Active bool
}

// searchEntry is one page in search.json.
type searchEntry struct {
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	Section  string    `json:"section"`
	Headings []Heading `json:"headings"`
	Text     string    `json:"text"`
}

// maxSearchText bounds each page's text in the search index.
const maxSearchText = 6000

func build(srcDir, outDir string) (int, error) {
	navSrc, err := os.ReadFile(filepath.Join(srcDir, "_nav.md"))
	if err != nil {
		return 0, err
	}
	nav, err := parseNav(string(navSrc))
	if err != nil {
		return 0, err
	}
	pages := map[string]Page{}
	images := map[string]string{} // site-relative path → source file
	err = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(srcDir, p)
		rel = filepath.ToSlash(rel)
		if imageTypes[strings.ToLower(path.Ext(rel))] {
			images[rel] = p
			return nil
		}
		if !strings.HasSuffix(p, ".md") || d.Name() == "_nav.md" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		page, err := render(rel, string(data))
		if err != nil {
			return err
		}
		pages[rel] = page
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := check(nav, pages, images); err != nil {
		return 0, err
	}
	tmpl, err := template.ParseFS(theme, "theme/page.html")
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(outDir); err != nil {
		return 0, err
	}
	reading := order(nav)
	sectionOf := map[string]string{}
	for _, s := range nav {
		for _, item := range s.Pages {
			sectionOf[item.Path] = s.Title
		}
	}
	var index []searchEntry
	for rel, p := range pages {
		v := view{Page: p, Root: rootPrefix(rel), Body: template.HTML(p.HTML), Outline: outline(p.Headings),
			EditLink: "https://github.com/ODudek/flint/edit/main/site/" + rel}
		for _, s := range nav {
			nv := navView{Title: s.Title}
			for _, item := range s.Pages {
				nv.Pages = append(nv.Pages, navLinkView{Title: item.Title, Href: v.Root + htmlPath(item.Path), Active: item.Path == rel})
			}
			v.Nav = append(v.Nav, nv)
		}
		if i := slices.IndexFunc(reading, func(n NavItem) bool { return n.Path == rel }); i >= 0 {
			if i > 0 {
				v.Prev = &NavItem{Title: reading[i-1].Title, Path: v.Root + htmlPath(reading[i-1].Path)}
			}
			if i+1 < len(reading) {
				v.Next = &NavItem{Title: reading[i+1].Title, Path: v.Root + htmlPath(reading[i+1].Path)}
			}
		}
		if err := writePage(tmpl, filepath.Join(outDir, filepath.FromSlash(htmlPath(rel))), v); err != nil {
			return 0, err
		}
		text := p.Text
		if r := []rune(text); len(r) > maxSearchText {
			text = string(r[:maxSearchText])
		}
		index = append(index, searchEntry{Title: p.Title, URL: htmlPath(rel), Section: sectionOf[rel], Headings: p.Headings, Text: text})
	}
	slices.SortFunc(index, func(a, b searchEntry) int { return strings.Compare(a.URL, b.URL) })
	if err := writeJSON(filepath.Join(outDir, "search.json"), index); err != nil {
		return 0, err
	}
	if err := copyTheme(outDir); err != nil {
		return 0, err
	}
	if err := copyImages(images, outDir); err != nil {
		return 0, err
	}
	notFound := view{Page: Page{Title: "Page not found"}, Root: "/flint/",
		Body: template.HTML(`<p>This page doesn't exist. Try the search above or go to the <a href="/flint/index.html">home page</a>.</p>`)}
	for _, s := range nav {
		nv := navView{Title: s.Title}
		for _, item := range s.Pages {
			nv.Pages = append(nv.Pages, navLinkView{Title: item.Title, Href: "/flint/" + htmlPath(item.Path)})
		}
		notFound.Nav = append(notFound.Nav, nv)
	}
	if err := writePage(tmpl, filepath.Join(outDir, "404.html"), notFound); err != nil {
		return 0, err
	}
	// GitHub Pages: serve files as they are (no Jekyll processing).
	return len(pages), os.WriteFile(filepath.Join(outDir, ".nojekyll"), nil, 0o644)
}

// check: every nav entry exists, every page is in the nav, every link resolves.
func check(nav []Section, pages map[string]Page, images map[string]string) error {
	var problems []string
	listed := map[string]bool{}
	for _, item := range order(nav) {
		listed[item.Path] = true
		if _, ok := pages[item.Path]; !ok {
			problems = append(problems, "_nav.md links to missing "+item.Path)
		}
	}
	if _, ok := pages["index.md"]; !ok {
		problems = append(problems, "missing index.md (the home page)")
	}
	for rel, p := range pages {
		if !listed[rel] {
			problems = append(problems, rel+" is not in _nav.md")
		}
		for _, bad := range brokenLinks(p, pages) {
			problems = append(problems, rel+": broken link "+bad)
		}
		for _, bad := range missingImages(p, images) {
			problems = append(problems, rel+": missing image "+bad)
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("%d problems:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	return nil
}

// outline keeps h2 headings for the "On this page" list (h3 only when there are few h2).
func outline(hs []Heading) []Heading {
	var h2 []Heading
	for _, h := range hs {
		if h.Level == 2 {
			h2 = append(h2, h)
		}
	}
	if len(h2) >= 3 {
		return h2
	}
	return hs
}

func writePage(tmpl *template.Template, file string, v view) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, v)
}

func writeJSON(file string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

// copyTheme copies the stylesheet and scripts to assets/.
// imageTypes are the files under site/ copied to the site as they are.
var imageTypes = map[string]bool{".png": true, ".gif": true, ".jpg": true, ".jpeg": true, ".svg": true, ".webp": true}

// copyImages copies the screenshots and other images next to the pages.
func copyImages(images map[string]string, outDir string) error {
	for rel, src := range images {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func copyTheme(outDir string) error {
	return fs.WalkDir(theme, "theme", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) == ".html" {
			return err
		}
		data, err := theme.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, "assets", filepath.FromSlash(strings.TrimPrefix(p, "theme/")))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
