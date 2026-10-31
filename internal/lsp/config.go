package lsp

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// ServerConfig is the user's setup of one language's server (settings.json
// "languageServers"). The zero value runs the first installed default server.
type ServerConfig struct {
	// Disabled keeps the language's server off.
	Disabled bool
	// Command is the server to run: one of the language's known servers by
	// name, an absolute path to one, or another server. Empty picks the first
	// installed known server.
	Command string
	// Args are added after the server's own arguments (--stdio, …).
	Args []string
}

// LanguageInfo describes a language Flint runs a server for (for Settings).
type LanguageInfo struct {
	Language string `json:"language"`
	// Servers are the known servers, in the order they are tried.
	Servers []string `json:"servers"`
	// NeedsTrust: the servers run the project's code, so they start only in
	// trusted folders.
	NeedsTrust bool   `json:"needsTrust"`
	Install    string `json:"install"`
}

// Limits for ServerConfig.Args.
const (
	MaxArgs   = 32
	MaxArgLen = 512
)

// ErrDisabled: the user turned the language's server off in Settings.
var ErrDisabled = errors.New("language server disabled in Settings")

// customServerReason explains Restricted Mode for a server Flint doesn't know.
const customServerReason = "a language server set in Settings may run the project's code"

// Languages lists the languages Flint has servers for.
func Languages() []LanguageInfo {
	out := make([]LanguageInfo, 0, len(specs))
	for _, s := range specs {
		names := make([]string, 0, len(s.Candidates))
		for _, c := range s.Candidates {
			names = append(names, c[0])
		}
		_, gated := needsTrust[s.Language]
		out = append(out, LanguageInfo{Language: s.Language, Servers: names, NeedsTrust: gated, Install: s.Install})
	}
	return out
}

// KnownLanguage reports whether Flint has a server spec for language.
func KnownLanguage(language string) bool {
	_, ok := specFor(language)
	return ok
}

// CheckArgs validates extra server arguments: a bounded list of single-line
// values. typescript-language-server's --tsserver-path is refused: it would
// let the server load another TypeScript than its own (see trust.go).
func CheckArgs(args []string) error {
	if len(args) > MaxArgs {
		return fmt.Errorf("too many arguments (max %d)", MaxArgs)
	}
	for _, a := range args {
		switch {
		case a == "":
			return errors.New("empty argument")
		case len(a) > MaxArgLen:
			return fmt.Errorf("argument longer than %d characters", MaxArgLen)
		case strings.ContainsFunc(a, unicode.IsControl):
			return fmt.Errorf("argument %q contains a newline or control character", a)
		case strings.HasPrefix(a, "--tsserver"):
			return fmt.Errorf("%s is not allowed: the TypeScript server always uses its own TypeScript", a)
		}
	}
	return nil
}

// command resolves the server to run for cfg: the configured command (with
// a known server's own arguments when it names one) or the first installed
// known server. custom reports a server Flint doesn't know, which is held
// back in Restricted Mode like the servers in needsTrust.
func (s serverSpec) command(cfg ServerConfig, lookPath func(string) (string, error)) (argv []string, custom, ok bool) {
	if cfg.Command == "" {
		argv, ok = s.resolveCommand(lookPath)
		return argv, false, ok
	}
	// A relative path would be resolved in the project folder (the server's
	// working directory) and run the project's file.
	if strings.ContainsRune(cfg.Command, '/') && !filepath.IsAbs(cfg.Command) {
		return nil, true, false
	}
	path, err := lookPath(cfg.Command)
	if err != nil {
		return nil, false, false
	}
	name := filepath.Base(cfg.Command)
	i := slices.IndexFunc(s.Candidates, func(c []string) bool { return c[0] == name })
	if i < 0 {
		return []string{path}, true, true
	}
	return append([]string{path}, s.Candidates[i][1:]...), false, true
}
