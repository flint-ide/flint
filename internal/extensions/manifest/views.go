package manifest

import (
	"fmt"
	"slices"
)

// Declarative views (docs/design/extensions.md, "Component catalog"): what a
// panel or a tab view shows is data the extension answers view/resolve with,
// drawn by Flint's own components.

// PanelViews are the kinds of view a panel may declare besides "webview".
var PanelViews = []string{"tree", "list", "table", "form", "chart"}

// TabViewKinds are the kinds of view a tab view may declare besides "webview".
var TabViewKinds = []string{"table", "form", "chart"}

// ViewKind returns the declared kind of the panel or tab view id ("tree",
// "table", "webview", …) and whether it is a tab view.
func (m *Manifest) ViewKind(id string) (kind string, tab, ok bool) {
	for _, p := range m.Contributes.Panels {
		if p.ID == id {
			return p.View, false, true
		}
	}
	if t, found := m.TabView(id); found {
		return t.View, true, true
	}
	return "", false, false
}

// checkDeclarative: a declarative view of a panel or tab view is one this
// Flint draws there, and has no webview file.
func checkDeclarative(what, id, view string, kinds []string, w *Webview) error {
	if !slices.Contains(kinds, view) {
		return fmt.Errorf(`%s %s: view %q is not supported (this Flint draws %s and "webview")`, what, id, view, quotedList(kinds))
	}
	if w != nil {
		return fmt.Errorf(`%s %s: "webview" is only for a "webview" view, not %q`, what, id, view)
	}
	return nil
}

func quotedList(list []string) string {
	out := ""
	for i, s := range list {
		if i > 0 {
			out += ", "
		}
		out += `"` + s + `"`
	}
	return out
}

// ParseProperty reads one property of the settings schema subset (schema.go)
// outside a manifest: a form view's field uses the same keywords and the same
// checks as a setting.
func ParseProperty(key string, data []byte) (Property, error) {
	if !keyPattern.MatchString(key) {
		return Property{}, fmt.Errorf("%q: use letters, digits, dashes and underscores", truncate(key, 40))
	}
	return parseProperty(key, data)
}
