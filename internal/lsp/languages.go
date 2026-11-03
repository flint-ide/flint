package lsp

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// serverSpec describes how to run a language server.
type serverSpec struct {
	// Language groups file types served by one server process.
	Language string
	// Candidates are tried in order; the first one on PATH wins.
	Candidates [][]string
	// Install is a hint shown when no candidate is installed.
	Install string
}

var specs = []serverSpec{
	{Language: "go", Candidates: [][]string{{"gopls"}}, Install: "go install golang.org/x/tools/gopls@latest"},
	{
		Language:   "typescript",
		Candidates: [][]string{{"vtsls", "--stdio"}, {"typescript-language-server", "--stdio"}},
		Install:    "npm i -g @vtsls/language-server   (or typescript-language-server typescript)",
	},
	{
		Language:   "python",
		Candidates: [][]string{{"basedpyright-langserver", "--stdio"}, {"pyright-langserver", "--stdio"}, {"pylsp"}},
		Install:    "pipx install basedpyright   (or npm i -g pyright)",
	},
	{Language: "rust", Candidates: [][]string{{"rust-analyzer"}}, Install: "rustup component add rust-analyzer"},
}

// languageIDs maps file extensions to (server language, LSP languageId).
var languageIDs = map[string][2]string{
	".go":  {"go", "go"},
	".ts":  {"typescript", "typescript"},
	".tsx": {"typescript", "typescriptreact"},
	".mts": {"typescript", "typescript"},
	".cts": {"typescript", "typescript"},
	".js":  {"typescript", "javascript"},
	".jsx": {"typescript", "javascriptreact"},
	".mjs": {"typescript", "javascript"},
	".cjs": {"typescript", "javascript"},
	".py":  {"python", "python"},
	".pyi": {"python", "python"},
	".rs":  {"rust", "rust"},
}

// languageFor returns the server language and LSP languageId for a path:
// Flint's own languages first, then an extension's that has a server.
func languageFor(path string) (language, languageID string, ok bool) {
	if ids, ok := languageIDs[strings.ToLower(filepath.Ext(path))]; ok {
		return ids[0], ids[1], true
	}
	if l, ok := extensionLanguageFor(path); ok && len(l.Candidates) > 0 {
		return l.Language, l.LanguageID, true
	}
	return "", "", false
}

// specFor is a language's server: Flint's own, else an extension's.
func specFor(language string) (serverSpec, bool) {
	if s, ok := builtinSpec(language); ok {
		return s, true
	}
	if l, ok := extensionLanguage(language); ok && len(l.Candidates) > 0 {
		return serverSpec{Language: l.Language, Candidates: l.Candidates, Install: l.Install}, true
	}
	return serverSpec{}, false
}

func builtinSpec(language string) (serverSpec, bool) {
	for _, s := range specs {
		if s.Language == language {
			return s, true
		}
	}
	return serverSpec{}, false
}

// ServerBinaries lists the binaries tried for a language's server, in the
// order they are tried, so the tool check reports the one that would start.
func ServerBinaries(language string) []string {
	s, _ := specFor(language)
	out := make([]string, 0, len(s.Candidates))
	for _, c := range s.Candidates {
		out = append(out, c[0])
	}
	return out
}

// resolveCommand picks the first installed candidate; lookPath is injectable for tests.
func (s serverSpec) resolveCommand(lookPath func(string) (string, error)) ([]string, bool) {
	for _, c := range s.Candidates {
		if path, err := lookPath(c[0]); err == nil {
			return append([]string{path}, c[1:]...), true
		}
	}
	return nil, false
}

var defaultLookPath = exec.LookPath
