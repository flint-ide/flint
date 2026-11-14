package manifest

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Webviews (docs/design/extensions.md, "Webviews"): a panel or tab view whose
// UI is one self-contained HTML file of the extension, shown by Flint in a
// sandboxed frame with no network.

// MaxWebviewHTML is the largest webview file Flint loads.
const MaxWebviewHTML = 32 << 20

const maxTabViews = 50

// Webview names a view's HTML file.
type Webview struct {
	// HTML is a clean relative path of an .html file inside the extension's folder.
	HTML string `json:"html"`
}

// TabView is a kind of editor tab the extension opens (window/openTab, or
// flint.openTab from one of its webviews).
type TabView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Icon is a name from Flint's icon set; empty shows the title only.
	Icon string `json:"icon,omitempty"`
	// View is "webview", or a declarative kind (views.go: "table", "form", "chart").
	View    string   `json:"view"`
	Webview *Webview `json:"webview,omitempty"`
	// Restore: open tabs of this view are saved with the session.
	Restore bool `json:"restore,omitempty"`
}

// TabView returns the contributed tab view id, if there is one.
func (m *Manifest) TabView(id string) (TabView, bool) {
	i := slices.IndexFunc(m.Contributes.TabViews, func(t TabView) bool { return t.ID == id })
	if i < 0 {
		return TabView{}, false
	}
	return m.Contributes.TabViews[i], true
}

// WebviewOf returns the webview of a panel or tab view, if it is one.
func (m *Manifest) WebviewOf(view string) (*Webview, bool) {
	for _, p := range m.Contributes.Panels {
		if p.ID == view && p.Webview != nil {
			return p.Webview, true
		}
	}
	if t, ok := m.TabView(view); ok && t.Webview != nil {
		return t.Webview, true
	}
	return nil, false
}

// HasWebviews reports whether the extension shows its own UI (the install
// dialog says so).
func (m *Manifest) HasWebviews() bool {
	return slices.ContainsFunc(m.Contributes.Panels, func(p Panel) bool { return p.Webview != nil }) ||
		slices.ContainsFunc(m.Contributes.TabViews, func(t TabView) bool { return t.Webview != nil })
}

func checkWebview(kind, id string, w *Webview) error {
	if w == nil {
		return fmt.Errorf(`%s %s: a "webview" view needs "webview": { "html": "…" }`, kind, id)
	}
	h := w.HTML
	switch {
	case h == "" || len(h) > 512 || strings.ContainsFunc(h, unicode.IsControl):
		return fmt.Errorf("%s %s: webview.html is missing, too long or contains control characters", kind, id)
	case strings.HasPrefix(h, "/") || strings.Contains(h, "\\") || path.Clean(h) != h || h == ".." || strings.HasPrefix(h, "../"):
		return fmt.Errorf("%s %s: webview.html %q: use a clean relative path inside the extension's folder", kind, id, truncate(h, 40))
	case !strings.HasSuffix(strings.ToLower(h), ".html"):
		return fmt.Errorf("%s %s: webview.html %q is not an .html file", kind, id, truncate(h, 40))
	}
	return nil
}

func (m *Manifest) checkTabViews(own func(kind, id string) error) error {
	if len(m.Contributes.TabViews) > maxTabViews {
		return fmt.Errorf("contributes.tabViews: at most %d", maxTabViews)
	}
	for _, t := range m.Contributes.TabViews {
		if err := own("tab view", t.ID); err != nil {
			return err
		}
		if err := checkText("tab view title", t.Title, 60, true); err != nil {
			return err
		}
		if t.Icon != "" && !iconPattern.MatchString(t.Icon) {
			return fmt.Errorf("tab view %s: icon %q is not a name from Flint's icon set", t.ID, t.Icon)
		}
		if t.View != "webview" {
			if err := checkDeclarative("tab view", t.ID, t.View, TabViewKinds, t.Webview); err != nil {
				return err
			}
			continue
		}
		if err := checkWebview("tab view", t.ID, t.Webview); err != nil {
			return err
		}
	}
	return nil
}

// ReadWebview reads a view's HTML file from the extension's folder dir. The
// file must stay inside dir (symbolic links included), be at most
// MaxWebviewHTML bytes and valid UTF-8.
func (m *Manifest) ReadWebview(dir, view string) (string, error) {
	w, ok := m.WebviewOf(view)
	if !ok {
		return "", fmt.Errorf("%s has no webview %q", m.Name, view)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(filepath.FromSlash(w.HTML))
	if err != nil {
		return "", fmt.Errorf("webview %s: %w", view, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("webview %s: %s is not a file", view, w.HTML)
	}
	if st.Size() > MaxWebviewHTML {
		return "", fmt.Errorf("webview %s: %s is larger than %d MiB", view, w.HTML, MaxWebviewHTML>>20)
	}
	data := make([]byte, st.Size())
	if _, err := f.ReadAt(data, 0); err != nil && st.Size() > 0 {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("webview " + view + ": " + w.HTML + " is not UTF-8 text")
	}
	return string(data), nil
}
