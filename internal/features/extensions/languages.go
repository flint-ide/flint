package extensions

import (
	"strings"
	"sync"

	"flint/internal/extensions/host"
	"flint/internal/extensions/manifest"
	"flint/internal/lsp"
)

// languageSync keeps Flint's language servers in step with the installed,
// enabled extensions' languages (manifest contributes.languages).
type languageSync struct {
	mu   sync.Mutex
	list func() []host.Info
}

// sync reads the extensions again and hands their languages to the LSP
// manager, which restarts the servers of languages that changed.
func (s *languageSync) sync() {
	s.mu.Lock() // the latest list wins
	defer s.mu.Unlock()
	lsp.SetExtensionLanguages(extensionLanguages(s.list()))
}

// extensionLanguages are the languages of the enabled, valid extensions, in
// extension id order (the first one wins a language or file type). A server
// candidate whose program can't be resolved is left out.
func extensionLanguages(infos []host.Info) []lsp.ExtensionLanguage {
	var out []lsp.ExtensionLanguage
	for _, in := range infos {
		if !in.Enabled || in.State == host.StateInvalid {
			continue
		}
		for _, l := range in.Languages {
			out = append(out, toLSP(in, l))
		}
	}
	return out
}

func toLSP(in host.Info, l manifest.Language) lsp.ExtensionLanguage {
	el := lsp.ExtensionLanguage{
		Language: l.ID, LanguageID: l.LanguageID, Filenames: l.Filenames, Source: in.ID,
	}
	if el.LanguageID == "" {
		el.LanguageID = l.ID
	}
	for _, e := range l.Extensions {
		el.Extensions = append(el.Extensions, strings.ToLower(e))
	}
	if l.Server == nil {
		return el
	}
	el.Install = l.Server.Install
	for _, c := range l.Server.Candidates {
		program, err := manifest.ServerProgram(in.Dir, c[0])
		if err != nil {
			continue
		}
		el.Candidates = append(el.Candidates, append([]string{program}, c[1:]...))
	}
	return el
}
