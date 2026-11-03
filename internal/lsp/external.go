package lsp

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ExtensionLanguage is a language an installed, enabled extension adds
// (manifest contributes.languages). Flint's own languages win over an
// extension's for the same file type or name.
type ExtensionLanguage struct {
	// Language names it ("ruby"); LanguageID is the LSP languageId.
	Language   string
	LanguageID string
	// Label is the language's name for people ("Ruby").
	Label string
	// Extensions are lower-case, with the dot; Filenames are exact names.
	Extensions []string
	Filenames  []string
	// Candidates are the server command lines, programs already resolved
	// (a name for PATH, or an absolute path inside the extension). None: the
	// language has no server.
	Candidates [][]string
	Install    string
	// Source names the extension, for messages.
	Source string
}

// extensionTrustReason: an extension's server is a program Flint doesn't
// know, so it starts only in trusted folders.
const extensionTrustReason = "a language server from an extension may run the project's code"

// registry holds the extensions' languages; reads are frequent (every file
// opened), writes happen when the installed extensions change.
var registry struct {
	sync.RWMutex
	langs      map[string]ExtensionLanguage
	byExt      map[string]string
	byFilename map[string]string
	// onChange are the managers to restart servers of changed languages.
	onChange []func(languages []string)
}

// SetExtensionLanguages replaces the extensions' languages and restarts the
// servers of languages that changed or went away. A language Flint knows
// itself, or a file type it already serves, is skipped; between extensions
// the first one listed wins.
func SetExtensionLanguages(list []ExtensionLanguage) {
	langs := map[string]ExtensionLanguage{}
	byExt := map[string]string{}
	byFilename := map[string]string{}
	for _, l := range list {
		if _, builtin := builtinSpec(l.Language); builtin || langs[l.Language].Language != "" {
			continue
		}
		langs[l.Language] = l
		for _, e := range l.Extensions {
			e = strings.ToLower(e)
			if _, builtin := languageIDs[e]; !builtin && byExt[e] == "" {
				byExt[e] = l.Language
			}
		}
		for _, f := range l.Filenames {
			if byFilename[f] == "" {
				byFilename[f] = l.Language
			}
		}
	}
	registry.Lock()
	changed := changedLanguages(registry.langs, langs)
	registry.langs, registry.byExt, registry.byFilename = langs, byExt, byFilename
	hooks := slices.Clone(registry.onChange)
	registry.Unlock()
	if len(changed) > 0 {
		for _, h := range hooks {
			h(changed)
		}
	}
}

func changedLanguages(prev, next map[string]ExtensionLanguage) []string {
	var out []string
	for name, p := range prev {
		if n, ok := next[name]; !ok || !sameLanguage(p, n) {
			out = append(out, name)
		}
	}
	for name := range next {
		if _, ok := prev[name]; !ok {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func sameLanguage(a, b ExtensionLanguage) bool {
	return a.LanguageID == b.LanguageID && a.Install == b.Install && a.Source == b.Source &&
		slices.Equal(a.Extensions, b.Extensions) && slices.Equal(a.Filenames, b.Filenames) &&
		slices.EqualFunc(a.Candidates, b.Candidates, slices.Equal[[]string])
}

// onExtensionLanguagesChanged registers a manager's restart hook.
func onExtensionLanguagesChanged(h func(languages []string)) {
	registry.Lock()
	defer registry.Unlock()
	registry.onChange = append(registry.onChange, h)
}

// extensionLanguageFor finds an extension's language for path.
func extensionLanguageFor(path string) (ExtensionLanguage, bool) {
	registry.RLock()
	defer registry.RUnlock()
	name := registry.byFilename[filepath.Base(path)]
	if name == "" {
		name = registry.byExt[strings.ToLower(filepath.Ext(path))]
	}
	l, ok := registry.langs[name]
	return l, ok && name != ""
}

func extensionLanguage(name string) (ExtensionLanguage, bool) {
	registry.RLock()
	defer registry.RUnlock()
	l, ok := registry.langs[name]
	return l, ok
}

// builtinLabels name Flint's own languages in messages.
var builtinLabels = map[string]string{"go": "Go", "typescript": "TypeScript/JavaScript", "python": "Python", "rust": "Rust"}

// LanguageOf names the language of path: Flint's own ("go", "typescript",
// "python", "rust") first, else an extension's, with or without a server.
// label is its name for messages ("Ruby").
func LanguageOf(path string) (language, label string, ok bool) {
	if ids, ok := languageIDs[strings.ToLower(filepath.Ext(path))]; ok {
		return ids[0], builtinLabels[ids[0]], true
	}
	if l, ok := extensionLanguageFor(path); ok {
		if l.Label == "" {
			l.Label = l.Language
		}
		return l.Language, l.Label, true
	}
	return "", "", false
}

// ExtensionLanguages lists the extensions' languages in effect, for Settings
// and the status bar.
func ExtensionLanguages() []ExtensionLanguage {
	registry.RLock()
	defer registry.RUnlock()
	out := make([]ExtensionLanguage, 0, len(registry.langs))
	for _, l := range registry.langs {
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b ExtensionLanguage) int { return strings.Compare(a.Language, b.Language) })
	return out
}
