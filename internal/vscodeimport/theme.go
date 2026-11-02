package vscodeimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"flint/internal/settings"
)

// Bounds on what a theme lookup reads: extension folders scanned per
// extensions directory, include chain depth, and file sizes.
const (
	maxExtensions   = 2000
	maxIncludeDepth = 5
	maxFileSize     = 4 << 20
	maxPackageSize  = 1 << 20
)

// themeRef is a color theme contributed by an installed extension.
type themeRef struct {
	label string
	path  string // the theme file
	root  string // the extension folder; includes may not leave it
	base  string // settings.ThemeDark or settings.ThemeLight
}

type packageJSON struct {
	Contributes struct {
		Themes []struct {
			ID      string `json:"id"`
			Label   string `json:"label"`
			UITheme string `json:"uiTheme"`
			Path    string `json:"path"`
		} `json:"themes"`
	} `json:"contributes"`
}

// findTheme looks for the theme named name (its label or id, as
// workbench.colorTheme stores it) in the installed extensions (the newest
// version of each, obsolete ones left out). Unreadable extensions are skipped.
func findTheme(extDirs []string, name string) (themeRef, error) {
	var found themeRef
	for _, dir := range extDirs {
		for _, ext := range installedExtensions(dir) {
			if ref, ok := themeIn(ext.dir, name); ok {
				found = ref
			}
		}
	}
	if found.path == "" {
		return themeRef{}, fmt.Errorf("theme %q is built into the editor or not installed as an extension, so Flint can't read its colors", name)
	}
	return found, nil
}

func themeIn(extDir, name string) (themeRef, bool) {
	var pkg packageJSON
	if readJSONC(filepath.Join(extDir, "package.json"), maxPackageSize, &pkg) != nil {
		return themeRef{}, false
	}
	var nls map[string]string
	for _, t := range pkg.Contributes.Themes {
		label := t.Label
		if strings.HasPrefix(label, "%") && strings.HasSuffix(label, "%") && len(label) > 2 {
			if nls == nil {
				nls = map[string]string{}
				_ = readJSONC(filepath.Join(extDir, "package.nls.json"), maxPackageSize, &nls)
			}
			label = nls[strings.Trim(label, "%")]
		}
		if label != name && t.ID != name {
			continue
		}
		base := settings.ThemeDark
		if t.UITheme == "vs" || t.UITheme == "hc-light" {
			base = settings.ThemeLight
		}
		return themeRef{label: name, path: filepath.Join(extDir, filepath.FromSlash(t.Path)), root: extDir, base: base}, true
	}
	return themeRef{}, false
}

// vsTheme is a VS Code color theme file, after includes are merged.
type vsTheme struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Include     string            `json:"include"`
	Colors      map[string]any    `json:"colors"`
	TokenColors json.RawMessage   `json:"tokenColors"`
	colors      map[string]string // merged, string values only
	rules       []tokenRule       // merged, included rules first
}

type tokenRule struct {
	Scope    json.RawMessage `json:"scope"`
	Settings struct {
		Foreground string `json:"foreground"`
	} `json:"settings"`
}

// loadTheme reads a theme and the files it includes, which must stay inside
// root. Parts Flint can't read (TextMate .tmTheme token colors) are reported
// in skipped instead of failing the theme.
func loadTheme(path, root string, depth int) (vsTheme, []string, error) {
	if depth > maxIncludeDepth {
		return vsTheme{}, nil, errors.New("theme includes nest too deeply")
	}
	if strings.EqualFold(filepath.Ext(path), ".tmTheme") {
		return vsTheme{}, nil, fmt.Errorf("%s is a TextMate theme, which Flint can't read", filepath.Base(path))
	}
	resolved, err := inside(root, path)
	if err != nil {
		return vsTheme{}, nil, err
	}
	var t vsTheme
	if err := readJSONC(resolved, maxFileSize, &t); err != nil {
		return vsTheme{}, nil, err
	}
	merged := vsTheme{Name: t.Name, Type: t.Type, colors: map[string]string{}}
	var skipped []string
	if t.Include != "" {
		parent, more, err := loadTheme(filepath.Join(filepath.Dir(path), filepath.FromSlash(t.Include)), root, depth+1)
		if err != nil {
			return vsTheme{}, nil, fmt.Errorf("include %s: %w", t.Include, err)
		}
		skipped = append(skipped, more...)
		merged.colors, merged.rules = parent.colors, parent.rules
		if merged.Type == "" {
			merged.Type = parent.Type
		}
	}
	for k, v := range t.Colors {
		if s, ok := v.(string); ok {
			merged.colors[k] = s
		}
	}
	rules, err := tokenRules(t.TokenColors, path, root, depth)
	if err != nil {
		skipped = append(skipped, err.Error())
	}
	merged.rules = append(merged.rules, rules...)
	return merged, skipped, nil
}

// tokenRules decodes tokenColors: a list of rules, or a path to a file with them.
func tokenRules(raw json.RawMessage, path, root string, depth int) ([]tokenRule, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rules []tokenRule
	if json.Unmarshal(raw, &rules) == nil {
		return rules, nil
	}
	var file string
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, errors.New("tokenColors has an unknown format")
	}
	ref, _, err := loadTheme(filepath.Join(filepath.Dir(path), filepath.FromSlash(file)), root, depth+1)
	if err != nil {
		return nil, fmt.Errorf("syntax colors: %w", err)
	}
	return ref.rules, nil
}

// inside returns path with symlinks resolved, refusing paths that leave root
// (an include like "../../x.json", or a symlink pointing elsewhere).
func inside(root, path string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("extension folder: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is outside the extension", filepath.Base(path))
	}
	return realPath, nil
}

// colorSources lists, per Flint CSS variable, the VS Code colors to take it
// from, first match wins.
var colorSources = []struct {
	variable string
	keys     []string
}{
	{"--bg-editor", []string{"editor.background"}},
	{"--fg", []string{"editor.foreground", "foreground"}},
	{"--bg", []string{"titleBar.activeBackground", "editorGroupHeader.tabsBackground", "sideBar.background"}},
	{"--bg-sidebar", []string{"sideBar.background"}},
	{"--bg-raised", []string{"editorWidget.background", "menu.background", "dropdown.background"}},
	{"--bg-hover", []string{"list.hoverBackground", "toolbar.hoverBackground"}},
	{"--bg-active", []string{"list.activeSelectionBackground", "list.inactiveSelectionBackground"}},
	{"--border", []string{"panel.border", "sideBar.border", "editorGroup.border", "widget.border", "contrastBorder"}},
	{"--fg-muted", []string{"descriptionForeground", "tab.inactiveForeground"}},
	{"--fg-faint", []string{"editorLineNumber.foreground"}},
	{"--fg-name", []string{"sideBar.foreground"}},
	{"--accent", []string{"activityBarBadge.background", "button.background", "focusBorder"}},
	{"--accent-fg", []string{"activityBarBadge.foreground", "button.foreground"}},
	{"--danger", []string{"errorForeground", "editorError.foreground"}},
	{"--warn", []string{"editorWarning.foreground"}},
	{"--info", []string{"editorInfo.foreground"}},
	{"--ok", []string{"testing.iconPassed", "gitDecoration.addedResourceForeground"}},
	{"--selection", []string{"editor.selectionBackground"}},
	{"--selection-match", []string{"editor.selectionHighlightBackground", "editor.wordHighlightBackground"}},
	{"--search-match", []string{"editor.findMatchHighlightBackground"}},
	{"--active-line", []string{"editor.lineHighlightBackground"}},
	{"--git-added", []string{"editorGutter.addedBackground", "gitDecoration.addedResourceForeground"}},
	{"--git-modified", []string{"editorGutter.modifiedBackground", "gitDecoration.modifiedResourceForeground"}},
	{"--git-deleted", []string{"editorGutter.deletedBackground", "gitDecoration.deletedResourceForeground"}},
	{"--diff-ins-bg", []string{"diffEditor.insertedTextBackground", "diffEditor.insertedLineBackground"}},
	{"--diff-del-bg", []string{"diffEditor.removedTextBackground", "diffEditor.removedLineBackground"}},
}

// terminalSources maps VS Code's terminal colors onto the custom theme's
// terminal palette (settings.TerminalColors).
var terminalSources = []struct{ color, key string }{
	{"background", "terminal.background"},
	{"foreground", "terminal.foreground"},
	{"cursor", "terminalCursor.foreground"},
	{"cursorAccent", "terminalCursor.background"},
	{"selectionBackground", "terminal.selectionBackground"},
	{"black", "terminal.ansiBlack"},
	{"red", "terminal.ansiRed"},
	{"green", "terminal.ansiGreen"},
	{"yellow", "terminal.ansiYellow"},
	{"blue", "terminal.ansiBlue"},
	{"magenta", "terminal.ansiMagenta"},
	{"cyan", "terminal.ansiCyan"},
	{"white", "terminal.ansiWhite"},
	{"brightBlack", "terminal.ansiBrightBlack"},
	{"brightRed", "terminal.ansiBrightRed"},
	{"brightGreen", "terminal.ansiBrightGreen"},
	{"brightYellow", "terminal.ansiBrightYellow"},
	{"brightBlue", "terminal.ansiBrightBlue"},
	{"brightMagenta", "terminal.ansiBrightMagenta"},
	{"brightCyan", "terminal.ansiBrightCyan"},
	{"brightWhite", "terminal.ansiBrightWhite"},
}

// syntaxSources lists, per Flint syntax color, representative TextMate scopes
// of that kind of token, most typical first. fallback uses the text color.
var syntaxSources = []struct {
	variable string
	scopes   []string
	fallback bool
}{
	{"--syn-keyword", []string{"keyword.control", "keyword", "storage.type"}, false},
	{"--syn-string", []string{"string.quoted.double", "string"}, false},
	{"--syn-number", []string{"constant.numeric", "constant"}, false},
	{"--syn-comment", []string{"comment.line.double-slash", "comment"}, false},
	{"--syn-function", []string{"entity.name.function", "support.function"}, false},
	{"--syn-type", []string{"entity.name.type", "support.type", "storage.type"}, false},
	{"--syn-property", []string{"variable.other.property", "support.type.property-name", "variable.other.object.property"}, false},
	{"--syn-definition", []string{"variable.other.readwrite", "variable"}, true},
	{"--syn-punct", []string{"punctuation.separator", "punctuation", "keyword.operator"}, true},
}

// convertTheme maps a VS Code theme onto Flint's CSS variables. Colors that
// aren't plain hex are left out (they'd be refused on save).
func convertTheme(t vsTheme, name, base string) settings.CustomTheme {
	out := settings.CustomTheme{Name: name, Base: base, Colors: map[string]string{}}
	pick := func(keys []string) string {
		for _, k := range keys {
			if c := t.colors[k]; settings.ValidColor(c) {
				return c
			}
		}
		return ""
	}
	for _, src := range colorSources {
		if c := pick(src.keys); c != "" {
			out.Colors[src.variable] = c
		}
	}
	for _, src := range terminalSources {
		if c := pick([]string{src.key}); c != "" {
			if out.Terminal == nil {
				out.Terminal = map[string]string{}
			}
			out.Terminal[src.color] = c
		}
	}
	if accent := out.Colors["--accent"]; accent != "" {
		out.Colors["--accent-dim"] = withAlpha(accent, "47") // 28 %, as the built-in palettes
	}
	// The active line is painted over the selection: an opaque color would hide it.
	if line := out.Colors["--active-line"]; line != "" {
		out.Colors["--active-line"] = withAlpha(line, "66")
	}
	text := out.Colors["--fg"]
	if text == "" {
		text = defaultForeground(t.rules)
	}
	for _, src := range syntaxSources {
		c := ""
		for _, scope := range src.scopes {
			if c = tokenColor(t.rules, scope); c != "" {
				break
			}
		}
		if c == "" && src.fallback {
			c = text
		}
		if c != "" {
			out.Colors[src.variable] = c
		}
	}
	return out
}

// tokenColor is the foreground VS Code would give a token with this scope:
// the most specific matching selector wins, a later rule on a tie. Descendant
// selectors ("source.go keyword") need a scope stack and are ignored.
func tokenColor(rules []tokenRule, scope string) string {
	best, color := 0, ""
	for _, r := range rules {
		if !settings.ValidColor(r.Settings.Foreground) {
			continue
		}
		for _, sel := range selectors(r.Scope) {
			if strings.ContainsAny(sel, " >") || (scope != sel && !strings.HasPrefix(scope, sel+".")) {
				continue
			}
			if depth := strings.Count(sel, ".") + 1; depth >= best {
				best, color = depth, r.Settings.Foreground
			}
		}
	}
	return color
}

// selectors splits a rule's scope: a string ("a, b") or a list of strings.
func selectors(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		var one string
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		list = []string{one}
	}
	var out []string
	for _, s := range list {
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// defaultForeground is the color of a rule without a scope (old-style themes).
func defaultForeground(rules []tokenRule) string {
	for _, r := range rules {
		if len(selectors(r.Scope)) == 0 && settings.ValidColor(r.Settings.Foreground) {
			return r.Settings.Foreground
		}
	}
	return ""
}

// withAlpha gives an opaque hex color the alpha aa; translucent ones are kept.
func withAlpha(c, aa string) string {
	switch len(c) {
	case 4: // #rgb
		return "#" + strings.Repeat(c[1:2], 2) + strings.Repeat(c[2:3], 2) + strings.Repeat(c[3:4], 2) + aa
	case 7:
		return c + aa
	}
	return c
}

// readJSONC decodes a JSON-with-comments file of at most limit bytes. Symlinks
// are followed (dotfile setups link settings.json), but only to a regular file:
// opening a FIFO or a device would block or read forever.
func readJSONC(path string, limit int64, v any) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err // keeps fs.ErrNotExist for a missing file
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	f, err := os.Open(resolved)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("%s is larger than %d MB", filepath.Base(path), limit>>20)
	}
	clean, err := StripJSONC(data)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(clean, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}
