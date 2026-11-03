package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Language is a programming language an extension teaches Flint: which
// files are in it, how comments look, its language server and its syntax
// highlighting. All of it is data: the server is a program Flint's own LSP
// client runs (only in trusted folders), and the grammar is a list of
// regular expressions Flint's editor applies (CodeMirror's simple mode).
type Language struct {
	// ID names the language ("ruby"): the LSP languageId unless LanguageID
	// says otherwise, and what onLanguage activation events name.
	ID    string `json:"id"`
	Label string `json:"label"`
	// Extensions are file extensions with the dot (".rb"); Filenames are
	// exact base names ("Gemfile").
	Extensions []string `json:"extensions,omitempty"`
	Filenames  []string `json:"filenames,omitempty"`
	// LanguageID is the LSP languageId sent for these files ("" = ID).
	LanguageID   string   `json:"languageId,omitempty"`
	LineComment  string   `json:"lineComment,omitempty"`
	BlockComment []string `json:"blockComment,omitempty"`
	Server       *Server  `json:"server,omitempty"`
	Grammar      *Grammar `json:"grammar,omitempty"`
}

// Server is a language server: the first candidate found runs. A candidate's
// program is a name looked up on PATH ("ruby-lsp") or a path inside the
// extension's folder starting with "./" ("./bin/server").
type Server struct {
	Candidates [][]string `json:"candidates"`
	// Install is shown when no candidate is installed.
	Install string `json:"install,omitempty"`
}

// Grammar is a CodeMirror simple mode: states by name, "start" first; each
// rule matches a regular expression (JavaScript syntax) at the cursor.
type Grammar struct {
	States map[string][]GrammarRule `json:"states"`
}

// GrammarRule is one rule of a state.
type GrammarRule struct {
	Regex string `json:"regex"`
	// Token is one of GrammarTokens ("" = no style).
	Token string `json:"token,omitempty"`
	// Next switches to a state; Push enters one; Pop leaves it.
	Next string `json:"next,omitempty"`
	Push string `json:"push,omitempty"`
	Pop  bool   `json:"pop,omitempty"`
	// Sol: the rule matches only at the start of a line.
	Sol    bool `json:"sol,omitempty"`
	Indent bool `json:"indent,omitempty"`
	Dedent bool `json:"dedent,omitempty"`
}

// GrammarTokens are the token names a grammar may use; the editor styles
// them with the theme's syntax colors.
var GrammarTokens = []string{
	"keyword", "atom", "number", "def", "variable", "variable-2", "variable-3", "type", "builtin",
	"string", "string-2", "comment", "meta", "operator", "punctuation", "bracket", "tag",
	"attribute", "property", "qualifier", "header", "link", "error", "invalidchar",
}

// Limits of the languages of one extension.
const (
	maxLanguages     = 20
	maxFilePatterns  = 50
	maxCandidates    = 8
	maxCandidateArgs = 16
	maxGrammarStates = 30
	maxGrammarRules  = 400
	maxRegexLen      = 500
)

var (
	languagePattern  = regexp.MustCompile(`^[a-z][a-z0-9+#_-]{0,39}$`)
	extPattern       = regexp.MustCompile(`^\.[A-Za-z0-9_+-]{1,20}$`)
	filenamePattern  = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,80}$`)
	statePattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,39}$`)
	programPattern   = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,80}$`)
	nestedQuantifier = regexp.MustCompile(`\([^()]*[+*}][^()]*\)[+*{]`)
)

func (m *Manifest) checkLanguages() error {
	if err := checkCount("languages", len(m.Contributes.Languages), maxLanguages); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, l := range m.Contributes.Languages {
		if !languagePattern.MatchString(l.ID) {
			return fmt.Errorf("language id %q: use lower-case letters, digits, +, #, _ or -, at most 40", l.ID)
		}
		if seen[l.ID] {
			return fmt.Errorf("language %q is listed twice", l.ID)
		}
		seen[l.ID] = true
		if err := l.check(); err != nil {
			return fmt.Errorf("language %s: %w", l.ID, err)
		}
	}
	return nil
}

func (l Language) check() error {
	if err := checkText("label", l.Label, 40, true); err != nil {
		return err
	}
	if len(l.Extensions)+len(l.Filenames) == 0 {
		return fmt.Errorf("no extensions or filenames")
	}
	if len(l.Extensions)+len(l.Filenames) > maxFilePatterns {
		return fmt.Errorf("at most %d extensions and filenames", maxFilePatterns)
	}
	for _, e := range l.Extensions {
		if !extPattern.MatchString(e) {
			return fmt.Errorf("extension %q is not like \".rb\"", e)
		}
	}
	for _, f := range l.Filenames {
		if !filenamePattern.MatchString(f) || f == "." || f == ".." {
			return fmt.Errorf("filename %q is not a plain file name", f)
		}
	}
	if l.LanguageID != "" && !languagePattern.MatchString(l.LanguageID) {
		return fmt.Errorf("languageId %q: use lower-case letters, digits, +, #, _ or -", l.LanguageID)
	}
	if err := checkText("lineComment", l.LineComment, 10, false); err != nil {
		return err
	}
	if len(l.BlockComment) != 0 && (len(l.BlockComment) != 2 || l.BlockComment[0] == "" || l.BlockComment[1] == "") {
		return fmt.Errorf(`blockComment is ["open", "close"]`)
	}
	for _, c := range l.BlockComment {
		if err := checkText("blockComment", c, 10, true); err != nil {
			return err
		}
	}
	if l.Server != nil {
		if err := l.Server.check(); err != nil {
			return fmt.Errorf("server: %w", err)
		}
	}
	if l.Grammar != nil {
		if err := l.Grammar.check(); err != nil {
			return fmt.Errorf("grammar: %w", err)
		}
	}
	return nil
}

func (s Server) check() error { return s.checkAs("server") }

// checkAs checks the candidates of a server or a debug adapter (kind).
func (s Server) checkAs(kind string) error {
	if len(s.Candidates) == 0 {
		return fmt.Errorf("no candidates")
	}
	if len(s.Candidates) > maxCandidates {
		return fmt.Errorf("at most %d candidates", maxCandidates)
	}
	for _, c := range s.Candidates {
		if len(c) == 0 {
			return fmt.Errorf("empty candidate")
		}
		if err := checkProgram(kind, c[0]); err != nil {
			return err
		}
		if len(c)-1 > maxCandidateArgs {
			return fmt.Errorf("at most %d arguments", maxCandidateArgs)
		}
		if err := checkArgs(c[1:]); err != nil {
			return err
		}
	}
	return checkText("install", s.Install, 200, false)
}

// checkProgram: a name on PATH, or "./" and a path inside the folder; kind
// names it in errors ("server", "adapter", "test runner").
func checkProgram(kind, p string) error {
	if rel, ok := strings.CutPrefix(p, "./"); ok {
		if err := checkCommandPath(rel); err != nil {
			return fmt.Errorf("%s program %q: %w", kind, p, err)
		}
		return nil
	}
	if !programPattern.MatchString(p) || p == "." || p == ".." {
		return fmt.Errorf(`%s program %q is neither a program name nor a "./" path inside the extension`, kind, p)
	}
	return nil
}

func (g Grammar) check() error {
	if len(g.States["start"]) == 0 {
		return fmt.Errorf(`no "start" state`)
	}
	if len(g.States) > maxGrammarStates {
		return fmt.Errorf("at most %d states", maxGrammarStates)
	}
	rules := 0
	for name, list := range g.States {
		if !statePattern.MatchString(name) {
			return fmt.Errorf("state name %q: letters, digits and _", name)
		}
		rules += len(list)
		for i, r := range list {
			if err := g.checkRule(r); err != nil {
				return fmt.Errorf("%s[%d]: %w", name, i, err)
			}
		}
	}
	if rules > maxGrammarRules {
		return fmt.Errorf("at most %d rules", maxGrammarRules)
	}
	return nil
}

func (g Grammar) checkRule(r GrammarRule) error {
	switch {
	case r.Regex == "":
		return fmt.Errorf("regex is missing")
	case len(r.Regex) > maxRegexLen:
		return fmt.Errorf("regex longer than %d characters", maxRegexLen)
	case nestedQuantifier.MatchString(r.Regex):
		// (a+)+ and the like backtrack exponentially and would freeze the editor.
		return fmt.Errorf("regex %q repeats a repeated group", truncate(r.Regex, 40))
	case r.Token != "" && !slices.Contains(GrammarTokens, r.Token):
		return fmt.Errorf("token %q is not one of %s", r.Token, strings.Join(GrammarTokens, ", "))
	}
	for _, s := range []string{r.Next, r.Push} {
		if s != "" && g.States[s] == nil {
			return fmt.Errorf("state %q doesn't exist", s)
		}
	}
	if r.Next != "" && r.Push != "" {
		return fmt.Errorf("next and push both set")
	}
	return checkText("regex", r.Regex, maxRegexLen, true)
}

// ServerProgram resolves a candidate's program (a language server's, a debug
// adapter's or a test runner's) for an extension in dir: a "./" path becomes
// the absolute path of an executable file inside dir (symbolic links
// included); a name is returned as it is, for a PATH lookup.
func ServerProgram(dir, program string) (string, error) {
	rel, ok := strings.CutPrefix(program, "./")
	if !ok {
		return program, nil
	}
	if err := checkCommandPath(rel); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	p, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if !within(root, p) {
		return "", fmt.Errorf("server program %q leads outside the extension's folder", program)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("server program %q is not an executable file", program)
	}
	return p, nil
}
