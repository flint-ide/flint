package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNav(t *testing.T) {
	nav, err := parseNav("## Get started\n- [Home](index.md)\n\n## Guides\n- [Kubernetes](guides/kubernetes.md)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(nav) != 2 || nav[1].Pages[0].Path != "guides/kubernetes.md" || len(order(nav)) != 2 {
		t.Fatalf("nav %+v", nav)
	}
	for _, bad := range []string{"- [Home](index.md)\n", "## A\n- Home\n", "## A\nstray text\n"} {
		if _, err := parseNav(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRender(t *testing.T) {
	p, err := render("guides/a.md", "# Kube & co\n\nSee [AWS](aws.md#log-in) and [home](../index.md).\n\n> **Warning:** careful\n\n## Log in\n\n### Details\n\n<script>alert(1)</script>\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Kube & co" || strings.Contains(p.HTML, "<h1") {
		t.Errorf("title %q, html %s", p.Title, p.HTML)
	}
	for _, want := range []string{`href="aws.html#log-in"`, `href="../index.html"`, `<blockquote class="callout warning"><p><strong>Warning:</strong>`, `<h2 id="log-in">`} {
		if !strings.Contains(p.HTML, want) {
			t.Errorf("missing %q in %s", want, p.HTML)
		}
	}
	if strings.Contains(p.HTML, "<script>") {
		t.Error("raw HTML rendered")
	}
	if len(p.Headings) != 2 || p.Headings[1].Level != 3 || p.Headings[0].ID != "log-in" {
		t.Errorf("headings %+v", p.Headings)
	}
	if rootPrefix("guides/a.md") != "../" || rootPrefix("index.md") != "" {
		t.Error("root prefix")
	}
}

func writeSite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuild(t *testing.T) {
	src := writeSite(t, map[string]string{
		"_nav.md":          "## Start\n- [Home](index.md)\n## Guides\n- [Git](guides/git.md)\n",
		"index.md":         "# Flint\n\nRead the [Git guide](guides/git.md#blame).\n",
		"guides/git.md":    "# Git\n\n## Blame\n\n![Blame](../images/blame.png)\n\nBack [home](../index.md).\n",
		"images/blame.png": "png",
	})
	out := filepath.Join(t.TempDir(), "site")
	n, err := build(src, out)
	if err != nil || n != 2 {
		t.Fatalf("build: %d pages, %v", n, err)
	}
	for _, f := range []string{"index.html", "guides/git.html", "404.html", "search.json", ".nojekyll", "images/blame.png", "assets/style.css", "assets/search.js", "assets/theme.js"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	page, _ := os.ReadFile(filepath.Join(out, "guides/git.html"))
	for _, want := range []string{`href="../assets/style.css"`, `class="active" aria-current="page">Git<`, `<span>Previous</span>Home`, `href="#blame"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("guides/git.html lacks %q", want)
		}
	}
	var index []searchEntry
	data, _ := os.ReadFile(filepath.Join(out, "search.json"))
	if err := json.Unmarshal(data, &index); err != nil || len(index) != 2 || index[0].Section == "" {
		t.Fatalf("search index %s (%v)", data, err)
	}
	// search.js reads these exact keys.
	for _, key := range []string{`"title":`, `"url":`, `"section":`, `"headings":[{"level":2,"id":"blame","text":"Blame"}]`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("search.json lacks %s: %s", key, data)
		}
	}
}

func TestBuildFailsOnBrokenLinksAndMissingPages(t *testing.T) {
	src := writeSite(t, map[string]string{
		"_nav.md":   "## Start\n- [Home](index.md)\n- [Gone](gone.md)\n",
		"index.md":  "# Flint\n\n[missing](nope.md) and [bad anchor](#nowhere) and [image](logo.png)\n\n![shot](images/none.png)\n",
		"orphan.md": "# Orphan\n",
	})
	_, err := build(src, filepath.Join(t.TempDir(), "site"))
	if err == nil {
		t.Fatal("broken site built")
	}
	for _, want := range []string{"missing gone.md", "orphan.md is not in _nav.md", "broken link nope.html", "#nowhere (no such heading)", "logo.png (not a page)", "missing image images/none.png"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}
