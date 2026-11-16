package manifest

import (
	"fmt"
	"strings"
	"testing"
)

// setsManifest is a manifest whose contributes.decorationSets is sets (JSON).
func setsManifest(sets string, more ...string) []byte {
	perms, activation := `["workspace.read"]`, `[]`
	if len(more) > 0 {
		perms = more[0]
	}
	if len(more) > 1 {
		activation = more[1]
	}
	return fmt.Appendf(nil, `{"contract":1,"id":"acme.lint","name":"Lint","version":"1.0.0","run":{"command":"lint"},
	  "permissions":%s,"activation":%s,"contributes":{"decorationSets":%s}}`, perms, activation, sets)
}

func TestDecorationSets(t *testing.T) {
	m, err := Parse(setsManifest(`[
	  {"id":"acme.lint.issues","title":"Lint issues","languages":["go"],"extensions":[".MD"],"globs":["Dockerfile*","*.test.ts"],"unsavedText":true},
	  {"id":"acme.lint.todo","title":"TODO","globs":["*"],"default":false}]`, `["workspace.read"]`, `["onLanguage:go"]`))
	if err != nil {
		t.Fatal(err)
	}
	if !m.NeedsProgram() {
		t.Error("decoration sets are served by a program")
	}
	issues, ok := m.DecorationSet("acme.lint.issues")
	if !ok || !issues.On() || !issues.UnsavedText {
		t.Fatalf("issues = %+v, %v", issues, ok)
	}
	if todo, _ := m.DecorationSet("acme.lint.todo"); todo.On() {
		t.Error(`"default": false leaves a set off`)
	}
	if _, ok := m.DecorationSet("acme.lint.nope"); ok {
		t.Error("an unknown set was found")
	}
	for _, tc := range []struct {
		file, language string
		want           bool
	}{
		{"/p/main.go", "go", true},
		{"/p/main.go", "", false}, // the language is the caller's, not guessed from the name
		{"/p/README.md", "", true},
		{"/p/README.MD", "", true},
		{"/p/Dockerfile.dev", "", true},
		{"/p/a.test.ts", "typescript", true},
		{"/p/a.ts", "typescript", false},
		{"/p/Dockerfile*/x.txt", "", false}, // globs see the file's name only
		{"/p/md", "", false},
	} {
		if got := issues.Matches(tc.file, tc.language); got != tc.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", tc.file, tc.language, got, tc.want)
		}
	}
}

func TestDecorationSetsRefused(t *testing.T) {
	many := make([]string, maxDecorationSets+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"id":"acme.lint.s%d","title":"S","globs":["*"]}`, i)
	}
	for name, tc := range map[string]struct {
		manifest []byte
		want     string
	}{
		"foreign id":        {setsManifest(`[{"id":"other.ext.issues","title":"T","globs":["*"]}]`), `must start with "acme.lint."`},
		"duplicate id":      {setsManifest(`[{"id":"acme.lint.a","title":"T","globs":["*"]},{"id":"acme.lint.a","title":"T","globs":["*"]}]`), "used twice"},
		"no title":          {setsManifest(`[{"id":"acme.lint.a","globs":["*"]}]`), "title is missing"},
		"long title":        {setsManifest(`[{"id":"acme.lint.a","title":"` + strings.Repeat("x", 61) + `","globs":["*"]}]`), "longer than 60"},
		"no selector":       {setsManifest(`[{"id":"acme.lint.a","title":"T"}]`), "which files"},
		"bad language":      {setsManifest(`[{"id":"acme.lint.a","title":"T","languages":["Go Lang"]}]`), "language"},
		"bad extension":     {setsManifest(`[{"id":"acme.lint.a","title":"T","extensions":["go"]}]`), `not like ".rb"`},
		"glob with folder":  {setsManifest(`[{"id":"acme.lint.a","title":"T","globs":["src/*.go"]}]`), "without folders"},
		"malformed glob":    {setsManifest(`[{"id":"acme.lint.a","title":"T","globs":["[a"]}]`), "not a pattern"},
		"empty glob":        {setsManifest(`[{"id":"acme.lint.a","title":"T","globs":[""]}]`), "glob is missing"},
		"too many":          {setsManifest("[" + strings.Join(many, ",") + "]"), "at most 20"},
		"too many patterns": {setsManifest(`[{"id":"acme.lint.a","title":"T","globs":[` + strings.TrimSuffix(strings.Repeat(`"*",`, maxSetSelectors+1), ",") + `]}]`), "at most 50"},
		"no permission":     {setsManifest(`[{"id":"acme.lint.a","title":"T","globs":["*"]}]`, `[]`), `needs the "workspace.read" permission`},
		"unlisted language": {setsManifest(`[{"id":"acme.lint.a","title":"T","languages":["go"]}]`, `["workspace.read"]`, `["onLanguage:python"]`), "activation event"},
		"wrong type":        {setsManifest(`{"id":"acme.lint.a"}`), "contributes.decorationSets"},
		"no program": {[]byte(`{"contract":1,"id":"acme.lint","name":"Lint","version":"1.0.0","permissions":["workspace.read"],
		  "contributes":{"decorationSets":[{"id":"acme.lint.a","title":"T","globs":["*"]}]}}`), "run.command is missing"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(tc.manifest)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want one with %q", err, tc.want)
			}
		})
	}
}
