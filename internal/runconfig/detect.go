package runconfig

import (
	"bufio"
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DetectedPrefix starts the id of every detected (unsaved) configuration.
const DetectedPrefix = "detected:"

// Limits that keep detection cheap on large trees.
const (
	maxDetected = 100
	maxGoDirs   = 3000
	maxGoDepth  = 6
	maxTargets  = 40
)

// Detect lists runnable targets of the project that need no configuration:
// Go main packages, Makefile targets and package.json scripts (in the root and
// its direct subdirectories). Nothing is saved until the user edits one.
func Detect(root string) []Config {
	var out []Config
	out = append(out, detectGo(root)...)
	out = append(out, detectMake(root)...)
	out = append(out, detectNpm(root)...)
	if len(out) > maxDetected {
		out = out[:maxDetected]
	}
	return out
}

// IsDetected reports whether id belongs to a detected configuration.
func IsDetected(id string) bool { return strings.HasPrefix(id, DetectedPrefix) }

// skipDir: directories the go tool ignores, plus heavy ones that never hold Go code.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "testdata" || name == "vendor" || name == "node_modules"
}

func detectGo(root string) []Config {
	module := goModule(filepath.Join(root, "go.mod"))
	if module == "" {
		return nil
	}
	var out []Config
	visited := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root {
			if skipDir(d.Name()) || depth(root, path) > maxGoDepth {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir // a nested module is a project of its own
			}
		}
		if visited++; visited > maxGoDirs {
			return filepath.SkipAll
		}
		if isMainPackage(path) {
			rel, _ := filepath.Rel(root, path)
			pkg, name := "./"+filepath.ToSlash(rel), filepath.Base(path)
			if rel == "." {
				pkg, name = ".", filepath.Base(module)
			}
			out = append(out, Config{ID: DetectedPrefix + "go:" + pkg, Name: name, Type: TypeGo, Package: pkg})
		}
		return nil
	})
	return out
}

func depth(root, path string) int {
	rel, _ := filepath.Rel(root, path)
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

var moduleLine = regexp.MustCompile(`(?m)^module\s+"?([^\s"]+)"?`)

func goModule(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := moduleLine.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// isMainPackage parses only the package clause of the first non-test Go file.
func isMainPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		return f.Name.Name == "main"
	}
	return false
}

// makeRule matches "target [target...]:" but not variable assignments (":=", "::=").
var makeRule = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.\-/]*(?:[ \t]+[A-Za-z0-9_][A-Za-z0-9_.\-/]*)*)[ \t]*:(?:[^=:]|$)`)

func detectMake(root string) []Config {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		if targets := MakeTargets(filepath.Join(root, name)); targets != nil {
			out := make([]Config, 0, len(targets))
			for _, t := range targets {
				out = append(out, Config{ID: DetectedPrefix + "make:" + t, Name: "make " + t, Type: TypeMake, Target: t})
			}
			return out
		}
	}
	return nil
}

// MakeTargets lists the explicit targets of a Makefile in file order (nil when
// there is no such file). Pattern rules and special targets (.PHONY) are skipped.
func MakeTargets(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	targets := []string{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(targets) < maxTargets {
		m := makeRule.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		for _, t := range strings.Fields(m[1]) {
			if !seen[t] && !strings.ContainsAny(t, "%$") {
				seen[t] = true
				targets = append(targets, t)
			}
		}
	}
	return targets
}

func detectNpm(root string) []Config {
	dirs := []string{"."}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() && !skipDir(e.Name()) {
				dirs = append(dirs, e.Name())
			}
		}
	}
	var out []Config
	for _, dir := range dirs {
		scripts := Scripts(filepath.Join(root, dir))
		if len(scripts) == 0 {
			continue
		}
		pm := PackageManager(filepath.Join(root, dir))
		for _, s := range scripts {
			name := pm + " run " + s
			cwd := ""
			if dir != "." {
				name += " (" + dir + ")"
				cwd = dir
			}
			out = append(out, Config{
				ID: DetectedPrefix + "npm:" + filepath.ToSlash(filepath.Join(dir, s)), Name: name, Type: TypeNpm, Cwd: cwd, Script: s,
			})
		}
	}
	return out
}

// Scripts returns the script names of dir/package.json, sorted.
func Scripts(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
