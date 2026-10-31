package todo

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// dependencyTree is a workspace with a note in the project and in every kind of
// dependency folder the scan must leave out.
func dependencyTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":                        "// TODO: kept\n",
		"java/target/Gen.java":           "// TODO: target without a manifest is source\n",
		"node_modules/dep/index.js":      "// TODO: dep\n",
		"web/node_modules/dep/index.js":  "// TODO: nested dep\n",
		"a/b/c/d/e/node_modules/x.js":    "// TODO: below the detection depth\n",
		".env/pyvenv.cfg":                "home = /usr/bin\n",
		".env/lib/site.py":               "# TODO: virtualenv named .env\n",
		"svc/api/py311/pyvenv.cfg":       "home = /usr/bin\n",
		"svc/api/py311/lib/x.py":         "# TODO: virtualenv with any name\n",
		"svc/api/handler.py":             "# TODO: kept too\n",
		"rust/Cargo.toml":                "[package]\n",
		"rust/target/debug/build/out.rs": "// TODO: cargo output\n",
		"cache/CACHEDIR.TAG":             "Signature: 8a477f597d28d172789f06886806bc55\n",
		"cache/blob.txt":                 "TODO: tagged cache\n",
		"pkg/mod.egg-info/PKG-INFO":      "TODO: egg-info\n",
		"docs/legacy/old.md":             "TODO: excluded by the user\n",
		"docs/new.md":                    "TODO: kept as well\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDetectDependencyDirs(t *testing.T) {
	root := dependencyTree(t)
	got := detectDependencyDirs(root)
	slices.Sort(got)
	want := []string{".env", "cache", "node_modules", "pkg/mod.egg-info", "rust/target", "svc/api/py311", "web/node_modules"}
	if !slices.Equal(got, want) {
		t.Fatalf("detected %q, want %q", got, want)
	}
}

func TestScanSkipsDependenciesAndUserExcludes(t *testing.T) {
	root := dependencyTree(t)
	res, err := Scan(t.Context(), []string{root}, []string{"docs/legacy"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, it.Path)
	}
	slices.Sort(got)
	want := []string{"docs/new.md", "java/target/Gen.java", "main.go", "svc/api/handler.py"}
	if !slices.Equal(got, want) {
		t.Fatalf("found %q, want %q", got, want)
	}
	if !slices.Contains(res.Skipped, filepath.Join(root, ".env")) {
		t.Fatalf("skipped %q", res.Skipped)
	}
}

func TestExcludeGlobsAnchorOnlyDetectedFolders(t *testing.T) {
	globs := excludeGlobs([]string{"web/node_modules", "svc/py311"}, []string{"*.min.js"})
	if !slices.Contains(globs, "node_modules/") || !slices.Contains(globs, "/svc/py311/") || globs[len(globs)-1] != "*.min.js" {
		t.Fatalf("globs %q", globs)
	}
	if slices.Contains(globs, "/web/node_modules/") {
		t.Fatalf("named folder anchored again: %q", globs)
	}
}
