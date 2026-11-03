package extensions

import (
	"strings"
	"sync"

	"flint/internal/dap"
	"flint/internal/extensions/host"
	"flint/internal/extensions/manifest"
	"flint/internal/lsp"
	"flint/internal/testrun"
)

// languageSync keeps Flint's language servers, debuggers and test runners in
// step with the installed, enabled extensions' languages, debuggers and test
// runners (manifest contributes.languages, debuggers, testRunners).
type languageSync struct {
	mu   sync.Mutex
	list func() []host.Info
}

// sync reads the extensions again and hands their languages to the LSP
// manager, which restarts the servers of languages that changed, and their
// debuggers and test runners to the debugger and the test runs.
func (s *languageSync) sync() {
	s.mu.Lock() // the latest list wins
	defer s.mu.Unlock()
	infos := s.list()
	lsp.SetExtensionLanguages(extensionLanguages(infos))
	dap.SetExtensionDebuggers(extensionDebuggers(infos))
	testrun.SetExtensionRunners(extensionRunners(infos))
}

// active are the enabled, valid extensions, in extension id order (the first
// one wins a language or file type).
func active(infos []host.Info) []host.Info {
	var out []host.Info
	for _, in := range infos {
		if in.Enabled && in.State != host.StateInvalid {
			out = append(out, in)
		}
	}
	return out
}

// extensionLanguages are the languages of the active extensions. A server
// candidate whose program can't be resolved is left out.
func extensionLanguages(infos []host.Info) []lsp.ExtensionLanguage {
	var out []lsp.ExtensionLanguage
	for _, in := range active(infos) {
		for _, l := range in.Languages {
			out = append(out, toLSP(in, l))
		}
	}
	return out
}

// extensionDebuggers are the debuggers of the active extensions; an adapter
// candidate whose program can't be resolved is left out.
func extensionDebuggers(infos []host.Info) []dap.ExtensionDebugger {
	var out []dap.ExtensionDebugger
	for _, in := range active(infos) {
		for _, d := range in.Debuggers {
			ed := dap.ExtensionDebugger{Type: d.Type, Label: d.Label, Languages: d.Languages, Launch: d.Launch, Source: in.ID}
			if d.Adapter != nil {
				ed.Install = d.Adapter.Install
				ed.Candidates = resolveCandidates(in.Dir, d.Adapter.Candidates)
			}
			out = append(out, ed)
		}
	}
	return out
}

// extensionRunners are the test runners of the active extensions; one whose
// program can't be resolved is left out.
func extensionRunners(infos []host.Info) []testrun.ExtensionRunner {
	var out []testrun.ExtensionRunner
	for _, in := range active(infos) {
		for _, r := range in.TestRunners {
			cmd := resolveCandidates(in.Dir, [][]string{r.Command})
			if len(cmd) == 0 {
				continue
			}
			format := testrun.FormatPlain
			if r.Format == manifest.TestFormatTAP {
				format = testrun.FormatTAP
			}
			out = append(out, testrun.ExtensionRunner{
				ID: r.ID, Label: r.Label, Languages: r.Languages, Command: cmd[0], Format: format, Source: in.ID,
			})
		}
	}
	return out
}

// resolveCandidates resolves each command line's program (manifest.ServerProgram),
// leaving out those that can't be.
func resolveCandidates(dir string, candidates [][]string) [][]string {
	var out [][]string
	for _, c := range candidates {
		if len(c) == 0 {
			continue
		}
		program, err := manifest.ServerProgram(dir, c[0])
		if err != nil {
			continue
		}
		out = append(out, append([]string{program}, c[1:]...))
	}
	return out
}

func toLSP(in host.Info, l manifest.Language) lsp.ExtensionLanguage {
	el := lsp.ExtensionLanguage{
		Language: l.ID, LanguageID: l.LanguageID, Label: l.Label, Filenames: l.Filenames, Source: in.ID,
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
	el.Candidates = resolveCandidates(in.Dir, l.Server.Candidates)
	return el
}
