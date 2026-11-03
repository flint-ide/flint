package testrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"flint/internal/runconfig"
)

// Request selects tests to run.
type Request struct {
	Lang string `json:"lang"` // LangGo | LangJS | LangExt
	// Dirs: Go package directories; JS: a directory inside the package (the
	// nearest package.json is used).
	Dirs []string `json:"dirs"`
	// File limits the run to the tests of one file.
	File string `json:"file,omitempty"`
	// Tests limits the run to these tests (Node.Test / Mark.Name).
	Tests []string `json:"tests,omitempty"`
}

// Output formats a plan's stdout is parsed as.
const (
	FormatGoJSON = "go-json"
	FormatTAP    = "tap"
	// FormatPlain: an unknown test runner; only the exit code is known.
	FormatPlain = "plain"
)

// Plan is how to run a Request.
type Plan struct {
	// Title names the run in the UI.
	Title  string              `json:"title"`
	Steps  []runconfig.Command `json:"steps"`
	Cwd    string              `json:"cwd"`
	Env    []string            `json:"env"`
	Format string              `json:"format"`
	// Root is the directory module/package-relative paths resolve against.
	Root string `json:"root"`
	// module is the Go module path (FormatGoJSON).
	module string
}

// NewParser returns the output parser of a plan.
func (p Plan) NewParser() Parser {
	switch p.Format {
	case FormatGoJSON:
		root, module := p.Root, p.module
		return NewGoParser(p.Cwd, func(importPath string) string {
			if importPath == module {
				return root
			}
			if rel, ok := strings.CutPrefix(importPath, module+"/"); ok {
				return filepath.Join(root, filepath.FromSlash(rel))
			}
			return ""
		})
	case FormatTAP:
		return NewTAPParser(p.Root)
	}
	return newPlainParser(p.Title, p.Root)
}

// PlanRun resolves a request into the command to run.
func PlanRun(req Request) (Plan, error) {
	if len(req.Dirs) == 0 {
		return Plan{}, errors.New("no test directory given")
	}
	for _, d := range append(slices.Clone(req.Dirs), req.File) {
		if d != "" && !filepath.IsAbs(d) {
			return Plan{}, fmt.Errorf("path %q is not absolute", d)
		}
	}
	switch req.Lang {
	case LangGo:
		return planGo(req)
	case LangJS:
		return planJS(req)
	}
	return Plan{}, fmt.Errorf("unknown test language %q", req.Lang)
}

func planGo(req Request) (Plan, error) {
	modRoot := findUp(req.Dirs[0], "go.mod")
	if modRoot == "" {
		return Plan{}, fmt.Errorf("no go.mod above %s", req.Dirs[0])
	}
	args := []string{"test", "-json"}
	tests := req.Tests
	if len(tests) == 0 && req.File != "" {
		data, err := os.ReadFile(req.File)
		if err != nil {
			return Plan{}, err
		}
		for _, m := range goMarks(string(data)) {
			if m.Kind == MarkTest {
				tests = append(tests, m.Name)
			}
		}
		if len(tests) == 0 {
			return Plan{}, fmt.Errorf("no tests in %s", filepath.Base(req.File))
		}
	}
	if len(tests) > 0 {
		args = append(args, "-run", GoRunPattern(tests))
	}
	var pkgs []string
	for _, d := range req.Dirs {
		if !within(d, modRoot) {
			return Plan{}, fmt.Errorf("%s is outside the module at %s", d, modRoot)
		}
		rel, _ := filepath.Rel(modRoot, d)
		pkgs = append(pkgs, "./"+filepath.ToSlash(rel))
	}
	args = append(args, pkgs...)
	return Plan{
		Title:  goTitle(req, tests, pkgs),
		Steps:  []runconfig.Command{runconfig.NewCommand("go", args...)},
		Cwd:    modRoot,
		Format: FormatGoJSON,
		Root:   modRoot,
		module: goModulePath(filepath.Join(modRoot, "go.mod")),
	}, nil
}

func goTitle(req Request, tests, pkgs []string) string {
	switch {
	case len(req.Tests) == 1:
		return req.Tests[0]
	case len(req.Tests) > 1:
		return fmt.Sprintf("%d tests", len(req.Tests))
	case req.File != "":
		return filepath.Base(req.File)
	case len(pkgs) == 1:
		return "go test " + pkgs[0]
	}
	return fmt.Sprintf("go test (%d packages)", len(pkgs))
}

// GoRunPattern builds a -run pattern. One test is matched exactly, level by
// level ("^TestA$/^sub$"); several select their top-level tests, as -run can't
// express arbitrary sets of subtests.
func GoRunPattern(tests []string) string {
	if len(tests) == 1 {
		levels := strings.Split(tests[0], "/")
		for i, l := range levels {
			levels[i] = "^" + regexp.QuoteMeta(l) + "$"
		}
		return strings.Join(levels, "/")
	}
	var tops []string
	for _, t := range tests {
		top := regexp.QuoteMeta(strings.SplitN(t, "/", 2)[0])
		if !slices.Contains(tops, top) {
			tops = append(tops, top)
		}
	}
	return "^(" + strings.Join(tops, "|") + ")$"
}

var goModuleLine = regexp.MustCompile(`(?m)^module\s+"?([^\s"]+)"?`)

func goModulePath(gomod string) string {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return ""
	}
	if m := goModuleLine.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

func planJS(req Request) (Plan, error) {
	pkgDir := findUp(req.Dirs[0], "package.json")
	if pkgDir == "" {
		return Plan{}, fmt.Errorf("no package.json above %s", req.Dirs[0])
	}
	script, err := testScript(pkgDir)
	if err != nil {
		return Plan{}, err
	}
	var flags []string
	if len(req.Tests) > 0 {
		flags = append(flags, "--test-name-pattern="+JSNamePattern(req.Tests))
	}
	title := filepath.Base(pkgDir)
	switch {
	case len(req.Tests) == 1:
		title = req.Tests[0]
	case len(req.Tests) > 1:
		title = fmt.Sprintf("%d tests", len(req.Tests))
	case req.File != "":
		title = filepath.Base(req.File)
	}
	plan := Plan{
		Title:  title,
		Cwd:    pkgDir,
		Root:   pkgDir,
		Format: FormatTAP,
		// Local binaries first, as npm run does.
		Env: []string{"PATH=" + filepath.Join(pkgDir, "node_modules", ".bin") + string(os.PathListSeparator) + os.Getenv("PATH")},
	}
	files := []string{}
	if req.File != "" {
		files = append(files, req.File)
	}
	tap := append([]string{"--test-reporter=tap"}, flags...)
	switch jsRunner(script) {
	case "webbuild":
		// Flint's own bundler: arguments after -- starting with "-" go to node,
		// the rest filter test files.
		plan.Steps = []runconfig.Command{runconfig.Shell(script + " -- " + quoteAll(append(tap, files...)))}
	case "node":
		if script == "" || req.File != "" {
			args := append(append(append(nodeFlags(script), "--test"), tap...), files...)
			plan.Steps = []runconfig.Command{runconfig.NewCommand("node", args...)}
		} else {
			plan.Steps = []runconfig.Command{runconfig.Shell(injectAfterTest(script, quoteAll(tap)))}
		}
	default:
		// jest, vitest, …: run the project's command; results come from the exit code.
		plan.Format = FormatPlain
		plan.Steps = []runconfig.Command{runconfig.Shell(strings.TrimSpace(script + " " + quoteAll(files)))}
	}
	return plan, nil
}

func testScript(pkgDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return "", err
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", fmt.Errorf("%s/package.json: %w", pkgDir, err)
	}
	script := strings.TrimSpace(pkg.Scripts["test"])
	if strings.Contains(script, "no test specified") {
		return "", nil // npm init's placeholder
	}
	return script, nil
}

var (
	testFlag    = regexp.MustCompile(`(^|\s)--test(\s|$)`)
	nodeCommand = regexp.MustCompile(`(^|[\s/])node\s`)
)

// jsRunner classifies a package.json test script.
func jsRunner(script string) string {
	switch {
	case strings.Contains(script, "webbuild") && testFlag.MatchString(script):
		return "webbuild"
	case script == "" || (nodeCommand.MatchString(script) && testFlag.MatchString(script)):
		return "node"
	}
	return "other"
}

// nodeFlags keeps the node options of a `node --test …` script (loaders,
// --experimental-*), dropping its file arguments.
func nodeFlags(script string) []string {
	fields := strings.Fields(script)
	i := slices.Index(fields, "node")
	if i < 0 {
		return nil
	}
	var flags []string
	withValue := map[string]bool{"--import": true, "--require": true, "-r": true, "--loader": true, "--experimental-loader": true}
	for j := i + 1; j < len(fields); j++ {
		f := fields[j]
		if !strings.HasPrefix(f, "-") || f == "&&" {
			break
		}
		if f == "--test" || strings.HasPrefix(f, "--test-reporter") || strings.HasPrefix(f, "--test-name-pattern") {
			continue
		}
		flags = append(flags, f)
		if withValue[f] && j+1 < len(fields) {
			j++
			flags = append(flags, fields[j])
		}
	}
	return flags
}

func injectAfterTest(script, extra string) string {
	loc := testFlag.FindStringIndex(script)
	end := loc[1]
	if strings.HasSuffix(script[loc[0]:loc[1]], " ") {
		end--
	}
	return script[:end] + " " + extra + script[end:]
}

// JSNamePattern matches tests by full name (suites and test joined by spaces),
// which node:test compares --test-name-pattern against.
func JSNamePattern(tests []string) string {
	quoted := make([]string, len(tests))
	for i, t := range tests {
		quoted[i] = regexp.QuoteMeta(t)
	}
	return "^(?:" + strings.Join(quoted, "|") + ")$"
}

func quoteAll(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = runconfig.Quote(a)
	}
	return strings.Join(out, " ")
}

// plainParser reports one node whose result is the exit code.
type plainParser struct {
	node Node
	sent bool
}

func newPlainParser(title, dir string) *plainParser {
	return &plainParser{node: Node{ID: title, Name: title, Kind: KindSuite, Status: Running, Dir: dir}}
}

func (p *plainParser) Line([]byte) ([]byte, []Node) {
	if p.sent {
		return nil, nil
	}
	p.sent = true
	return nil, []Node{p.node}
}

func (p *plainParser) Finish(exitCode int, stopped bool) []Node {
	switch {
	case stopped:
		p.node.Status = Cancelled
	case exitCode == 0:
		p.node.Status = Passed
	default:
		p.node.Status = Failed
	}
	return []Node{p.node}
}
