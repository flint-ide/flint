package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIncludesPartials(t *testing.T) {
	src := writeSite(t, map[string]string{
		"_nav.md":                      "## Reference\n- [Home](index.md)\n- [Settings](reference/settings.md)\n",
		"index.md":                     "# Flint\n",
		"reference/settings.md":        "# Settings\n\n| Key | Meaning |\n|---|---|\n| `theme` | Colors. |\n<!-- include-rows _settings/*.md -->\n\n## Keys\n\n<!-- include-sections _settings/*.md -->\n\n## After\n",
		"reference/_settings/zeta.md":  "| `zeta` | Last. See [zeta](#zeta). |\n\n### zeta\n\nZeta details, see [home](../index.md).\n",
		"reference/_settings/alpha.md": "| `alpha` | First, no section. |\n",
	})
	out := filepath.Join(t.TempDir(), "site")
	n, err := build(src, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("built %d pages, want 2 (partials are not pages)", n)
	}
	data, err := os.ReadFile(filepath.Join(out, "reference", "settings.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{"<td><code>alpha</code></td>", "<td><code>zeta</code></td>", `<h3 id="zeta">zeta</h3>`, `href="../index.html"`} {
		if !strings.Contains(html, want) {
			t.Errorf("settings.html lacks %s", want)
		}
	}
	if strings.Index(html, "<code>alpha</code>") > strings.Index(html, "<code>zeta</code>") {
		t.Error("partials are not in file-name order")
	}
	if strings.Contains(html, "include-") {
		t.Error("a directive is left in the page")
	}
}

func TestIncludeOfNothingFails(t *testing.T) {
	src := writeSite(t, map[string]string{
		"_nav.md":  "## Start\n- [Home](index.md)\n",
		"index.md": "# Flint\n\n<!-- include-sections _none/*.md -->\n",
	})
	if _, err := build(src, filepath.Join(t.TempDir(), "site")); err == nil || !strings.Contains(err.Error(), "matches no file") {
		t.Errorf("build() = %v", err)
	}
}
