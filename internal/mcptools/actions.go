package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"flint/internal/httpfile"
	"flint/internal/mcpserver"
	"flint/internal/runconfig"
	"flint/internal/testrun"
)

// Tools that act: they do what the user does in Flint's UI (the gutter ▶, the
// Run toolbar, an .http file's ▶), so the result also lands in Flint's own
// views. They are off by default, and the server asks the user before every
// call (mcpserver.Tool.Acts). Their arguments only name things that exist in
// the project (a file, a test, a run configuration, a request): no command
// line is ever built from them.
const (
	ToolRunTests  = "run_tests"
	ToolRunConfig = "run_configuration"
	ToolRunOutput = "get_run_output" // read-only; offered with run_configuration
	ToolHTTP      = "send_http_request"
	// ToolSQL is offered by the Database feature (internal/features/database).
	ToolSQL = "run_sql_query"
)

// ActionNames lists the tools Flint offers on top of Names when the user
// switched their group on.
var ActionNames = []string{ToolRunTests, ToolRunConfig, ToolRunOutput, ToolHTTP, ToolSQL}

// Bounds of the acting tools' arguments and answers.
const (
	maxTestNames   = 20
	maxNameLen     = 200
	maxTestFiles   = 500              // _test.go files read to check test names
	maxSourceFile  = 2 << 20          // bytes of a test or .http file
	maxListed      = 40               // names listed in an error or an answer
	maxHTTPBody    = 16 << 10         // bytes of a response body in an answer
	maxHTTPHeaders = 40               // response headers in an answer
	maxHeaderValue = 300              // bytes of one header value
	startSlack     = 30 * time.Second // on top of a tool's own wait
	httpTimeout    = 5 * time.Minute  // a request's "# @timeout" may ask for more than the default
)

// RunCols and RunRows size the console of a run an agent started (the run
// tab resizes it once it is shown).
const (
	RunCols = 120
	RunRows = 30
)

// Actor does what the acting tools ask, the way Flint's UI does it (package
// main implements it; tests use a fake).
type Actor interface {
	// PlanTests resolves a request into the command to run.
	PlanTests(root string, req testrun.Request) (testrun.Plan, error)
	// StartTests starts a planned run in the Tests tab and returns its id;
	// done is closed once the run ended.
	StartTests(root string, req testrun.Request, plan testrun.Plan) (id string, done <-chan struct{}, err error)
	// RunConfigs lists root's saved and detected run configurations.
	RunConfigs(root string) ([]runconfig.Config, error)
	// StartConfig starts (or restarts) a configuration in its run tab and
	// returns the run's id.
	StartConfig(root string, cfg runconfig.Config) (id string, err error)
	// SendHTTP sends a prepared request of file and keeps the response in the
	// file's history.
	SendHTTP(ctx context.Context, root, file string, p httpfile.Prepared) (*httpfile.Response, error)
}

func (s *Sources) actionTools(cfg Config) []mcpserver.Tool {
	if s.Actor == nil {
		return nil
	}
	var out []mcpserver.Tool
	if cfg.RunTests {
		wait := cfg.TestWait
		if wait <= 0 {
			wait = 2 * time.Minute
		}
		out = append(out, mcpserver.Tool{
			Name:  ToolRunTests,
			Title: "Run tests",
			Description: "Runs tests in Flint's test runner, like the ▶ in the editor gutter, and waits for the results: totals, and each failed test with its location and output. " +
				"The run also shows in Flint's Tests tab. Go: pass a package folder or a _test.go file, optionally with test names (\"TestA\", \"TestA/sub\"). " +
				"JavaScript/TypeScript: pass a *.test.* or *.spec.* file, optionally with test names. The user is asked before each run.",
			InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
				`"path":{"type":"string","description":"A Go package folder, a _test.go file or a JS/TS test file, relative to the project or absolute inside it."},`+
				`"tests":{"type":"array","items":{"type":"string"},"maxItems":%d,"description":"Names of tests in that file or package. Default: all of them."}`+
				`},"required":["path"],"additionalProperties":false}`, maxTestNames)),
			Acts:    true,
			Timeout: wait + startSlack,
			Plan: func(_ context.Context, root string, args json.RawMessage) (mcpserver.Planned, error) {
				return s.planRunTests(root, args, wait)
			},
		})
	}
	if cfg.RunConfigs {
		lines := max(1, cfg.TerminalLines)
		out = append(out, mcpserver.Tool{
			Name:  ToolRunConfig,
			Title: "Run a configuration",
			Description: "Starts (or restarts) one of the project's run configurations (.flint/run.json and the detected ones: Go main packages, package.json scripts, make targets) " +
				"in Flint's run tab, by its name. It returns once the process started, with the run's id; read its output and state with " + ToolRunOutput + ". The user is asked before each start.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"name":{"type":"string","description":"Name (or id) of the run configuration, as Flint's Run toolbar shows it."}` +
				`},"required":["name"],"additionalProperties":false}`),
			Acts: true,
			Plan: s.planRunConfig,
		}, mcpserver.Tool{
			Name:  ToolRunOutput,
			Title: "Run output",
			Description: "Recent output and state (running, or the exit code) of the runs in Flint's run tabs for this project: run configurations and test runs. " +
				"Without a run id it lists them with their last lines; pass an id for a longer tail of one.",
			InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
				`"run":{"type":"string","description":"Id of one run, as %s returned it or a call without it lists."},`+
				`"lines":{"type":"integer","minimum":1,"maximum":%d,"description":"Lines per run (default %d for one run, %d for the list)."}`+
				`},"additionalProperties":false}`, ToolRunConfig, lines, lines, min(lines, listLines))),
			Call: func(_ context.Context, root string, args json.RawMessage) (string, error) {
				return s.runOutput(root, args, lines)
			},
		})
	}
	if cfg.HTTP {
		out = append(out, mcpserver.Tool{
			Name:  ToolHTTP,
			Title: "Send an HTTP request",
			Description: "Sends one named request of a .http or .rest file of the project with Flint's HTTP client (the file as saved on disk) and returns the status, " +
				"the response headers and the start of the body. The response is also kept in the file's response history in Flint. " +
				"Requests are named with \"### name\" or \"# @name name\". The user is asked before each request.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"file":{"type":"string","description":"The .http or .rest file, relative to the project or absolute inside it."},` +
				`"request":{"type":"string","description":"Name of the request in that file."},` +
				`"environment":{"type":"string","description":"Environment from http-client.env.json next to the file. Default: none."}` +
				`},"required":["file","request"],"additionalProperties":false}`),
			Acts:    true,
			Timeout: httpTimeout,
			Plan: func(_ context.Context, root string, args json.RawMessage) (mcpserver.Planned, error) {
				return s.planHTTP(root, args)
			},
		})
	}
	return out
}

// decodeStrict reads an acting tool's arguments; unknown fields are refused,
// so a call never does less than what the user was shown.
func decodeStrict(args json.RawMessage, v any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

// checkName bounds a name from a tool's arguments and refuses control
// characters (it is shown to the user).
func checkName(what, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%s is required", what)
	case len(name) > maxNameLen:
		return fmt.Errorf("%s is longer than %d bytes", what, maxNameLen)
	case strings.ContainsFunc(name, unicode.IsControl):
		return fmt.Errorf("%s contains a control character", what)
	}
	return nil
}

// existingInside resolves a required path argument to an existing file or
// folder of the project. Symlinks are resolved for the check, so a link
// pointing out of the project is refused.
func existingInside(root, p string) (string, os.FileInfo, error) {
	if p == "" {
		return "", nil, errors.New("path is required")
	}
	path, err := scopePath(root, p)
	if err != nil {
		return "", nil, err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, fmt.Errorf("%s does not exist in the project", rel(path, root))
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, fmt.Errorf("project folder: %v", err)
	}
	if !within(real, realRoot) {
		return "", nil, fmt.Errorf("%s leads outside the project", rel(path, root))
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", nil, fmt.Errorf("%s does not exist in the project", rel(path, root))
	}
	return path, info, nil
}

// readSource reads a test or request file of bounded size.
func readSource(path string, info os.FileInfo) (string, error) {
	if info.IsDir() || info.Size() > maxSourceFile {
		return "", fmt.Errorf("%s is a folder or larger than %d MB", filepath.Base(path), maxSourceFile>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %v", filepath.Base(path), unwrap(err))
	}
	return string(data), nil
}

func unwrap(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// ---------- run_tests ----------

// testRequest turns run_tests' arguments into the request the gutter ▶
// would send: every path and test name must exist in the project.
func testRequest(root string, args json.RawMessage) (testrun.Request, error) {
	var in struct {
		Path  string   `json:"path"`
		Tests []string `json:"tests"`
	}
	if err := decodeStrict(args, &in); err != nil {
		return testrun.Request{}, err
	}
	path, info, err := existingInside(root, in.Path)
	if err != nil {
		return testrun.Request{}, err
	}
	if len(in.Tests) > maxTestNames {
		return testrun.Request{}, fmt.Errorf("at most %d test names", maxTestNames)
	}
	for _, name := range in.Tests {
		if err := checkName("a test name", name); err != nil {
			return testrun.Request{}, err
		}
	}
	tests := slices.Compact(slices.Sorted(slices.Values(in.Tests)))
	if info.IsDir() {
		known, err := goPackageTests(path)
		if err != nil {
			return testrun.Request{}, err
		}
		if len(known) == 0 {
			return testrun.Request{}, fmt.Errorf("%s has no Go tests (*_test.go); for JavaScript or TypeScript pass a test file", rel(path, root))
		}
		if err := knownGoTests(tests, known, rel(path, root)); err != nil {
			return testrun.Request{}, err
		}
		return testrun.Request{Lang: testrun.LangGo, Dirs: []string{path}, Tests: tests}, nil
	}
	dir := filepath.Dir(path)
	switch testrun.Lang(path) {
	case testrun.LangGo:
		if len(tests) == 0 {
			return testrun.Request{Lang: testrun.LangGo, Dirs: []string{dir}, File: path}, nil
		}
		known, err := goPackageTests(dir)
		if err != nil {
			return testrun.Request{}, err
		}
		if err := knownGoTests(tests, known, rel(dir, root)); err != nil {
			return testrun.Request{}, err
		}
		return testrun.Request{Lang: testrun.LangGo, Dirs: []string{dir}, Tests: tests}, nil
	case testrun.LangJS:
		content, err := readSource(path, info)
		if err != nil {
			return testrun.Request{}, err
		}
		var known []string
		for _, m := range testrun.Marks(path, content) {
			if m.Name != "" {
				known = append(known, m.Name)
			}
		}
		for _, name := range tests {
			if !slices.Contains(known, name) {
				return testrun.Request{}, fmt.Errorf("no test %q in %s (tests: %s)", name, rel(path, root), listNames(known))
			}
		}
		return testrun.Request{Lang: testrun.LangJS, Dirs: []string{dir}, File: path, Tests: tests}, nil
	}
	return testrun.Request{}, fmt.Errorf("%s is not a test file Flint runs itself: pass a Go package folder, a _test.go file or a *.test.* / *.spec.* JavaScript or TypeScript file", rel(path, root))
}

// goPackageTests lists the tests (and the subtests written out in the
// source) of the _test.go files in dir.
func goPackageTests(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %v", filepath.Base(dir), unwrap(err))
	}
	var names []string
	files := 0
	for _, e := range entries {
		if e.IsDir() || testrun.Lang(e.Name()) != testrun.LangGo {
			continue
		}
		if files++; files > maxTestFiles {
			break
		}
		info, err := e.Info()
		if err != nil || info.Size() > maxSourceFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range testrun.Marks(e.Name(), string(data)) {
			if m.Name != "" && !slices.Contains(names, m.Name) {
				names = append(names, m.Name)
			}
		}
	}
	return names, nil
}

// knownGoTests requires every name to be a test of the package: a test
// function, or a subtest of one (subtests made at run time aren't in the
// source, so only their top-level test is checked).
func knownGoTests(tests, known []string, where string) error {
	for _, name := range tests {
		top, _, _ := strings.Cut(name, "/")
		if !slices.Contains(known, name) && !slices.Contains(known, top) {
			return fmt.Errorf("no test %q in %s (tests: %s)", name, where, listNames(topLevel(known)))
		}
	}
	return nil
}

func topLevel(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return strings.Contains(n, "/") })
}

// listNames joins names for a message, bounded.
func listNames(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	shown := names[:min(len(names), maxListed)]
	out := strings.Join(shown, ", ")
	if len(names) > len(shown) {
		out += fmt.Sprintf(", … and %d more", len(names)-len(shown))
	}
	return out
}

// commandLines shows a launch's steps, one per line.
func commandLines(steps []runconfig.Command) string {
	lines := make([]string, 0, len(steps))
	for _, st := range steps {
		label := st.Label
		if label == "" {
			label = strings.Join(append([]string{st.Program}, st.Args...), " ")
		}
		lines = append(lines, "  "+label)
	}
	return strings.Join(lines, "\n")
}

func (s *Sources) planRunTests(root string, args json.RawMessage, wait time.Duration) (mcpserver.Planned, error) {
	req, err := testRequest(root, args)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	plan, err := s.Actor.PlanTests(root, req)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	summary := fmt.Sprintf("Run tests: %s\nIn %s:\n%s", plan.Title, relDir(plan.Cwd, root), commandLines(plan.Steps))
	return mcpserver.Planned{Summary: summary, Run: func(ctx context.Context) (string, error) {
		id, done, err := s.Actor.StartTests(root, req, plan)
		if err != nil {
			return "", err
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		finished := true
		select {
		case <-done:
		case <-timer.C:
			finished = false
		case <-ctx.Done():
			finished = false
		}
		run, ok := s.Tests.Get(id)
		if !ok {
			return "", fmt.Errorf("the test run %s started, but Flint keeps no results of it; see the Tests tab", plan.Title)
		}
		text := s.describeRun("Test run", run, root)
		if !finished && !run.Done {
			text += fmt.Sprintf("\n\nThe run is still going after %s and continues in Flint's Tests tab (run id %s). Ask again later for its results.", wait.Round(time.Second), id)
		}
		return text, nil
	}}, nil
}

// relDir names a folder of the project for the user ("the project folder"
// for the root itself).
func relDir(dir, root string) string {
	if r := rel(filepath.Clean(dir), root); r != "." {
		return r
	}
	return "the project folder"
}

// ---------- run_configuration / get_run_output ----------

// pickConfig finds the configuration called name (its name, else its id).
func pickConfig(configs []runconfig.Config, name string) (runconfig.Config, error) {
	var byName []runconfig.Config
	for _, c := range configs {
		if c.Name == name {
			byName = append(byName, c)
		}
	}
	if len(byName) == 0 {
		if i := slices.IndexFunc(configs, func(c runconfig.Config) bool { return c.ID == name }); i >= 0 {
			return configs[i], nil
		}
		names := make([]string, 0, len(configs))
		for _, c := range configs {
			names = append(names, c.Name)
		}
		return runconfig.Config{}, fmt.Errorf("no run configuration named %q in this project (configurations: %s)", name, listNames(names))
	}
	if len(byName) > 1 {
		ids := make([]string, 0, len(byName))
		for _, c := range byName {
			ids = append(ids, c.ID)
		}
		return runconfig.Config{}, fmt.Errorf("%d run configurations are named %q; pass the id of one (%s)", len(byName), name, listNames(ids))
	}
	return byName[0], nil
}

func (s *Sources) planRunConfig(_ context.Context, root string, args json.RawMessage) (mcpserver.Planned, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeStrict(args, &in); err != nil {
		return mcpserver.Planned{}, err
	}
	if err := checkName("name", in.Name); err != nil {
		return mcpserver.Planned{}, err
	}
	configs, err := s.Actor.RunConfigs(root)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	cfg, err := pickConfig(configs, in.Name)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	launch, err := runconfig.Resolve(root, cfg)
	if err != nil {
		return mcpserver.Planned{}, fmt.Errorf("run configuration %q: %v", cfg.Name, err)
	}
	summary := fmt.Sprintf("Run configuration: %s\nIn %s:\n%s", cfg.Name, relDir(launch.Cwd, root), commandLines(launch.Steps))
	return mcpserver.Planned{Summary: summary, Run: func(context.Context) (string, error) {
		id, err := s.Actor.StartConfig(root, cfg)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Started %q in Flint's run tab (run id %s). It keeps running; read its output and whether it exited with %s {\"run\": %q}.",
			cfg.Name, id, ToolRunOutput, id), nil
	}}, nil
}

func (s *Sources) runOutput(root string, args json.RawMessage, maxLines int) (string, error) {
	var in struct {
		Run   string `json:"run"`
		Lines int    `json:"lines"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	if in.Lines < 0 {
		return "", fmt.Errorf("lines must be positive")
	}
	list := slices.DeleteFunc(s.Terminals.List(root), func(t TerminalInfo) bool { return t.Kind != KindRun })
	if len(list) == 0 {
		return "No runs of this project in Flint's run tabs.", nil
	}
	return s.consoleTails(root, list, "run", in.Run, in.Lines, maxLines)
}

// ---------- send_http_request ----------

func isHTTPFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".http" || ext == ".rest"
}

func (s *Sources) planHTTP(root string, args json.RawMessage) (mcpserver.Planned, error) {
	var in struct {
		File        string `json:"file"`
		Request     string `json:"request"`
		Environment string `json:"environment"`
	}
	if err := decodeStrict(args, &in); err != nil {
		return mcpserver.Planned{}, err
	}
	if in.File == "" {
		return mcpserver.Planned{}, errors.New("file is required")
	}
	if err := checkName("request", in.Request); err != nil {
		return mcpserver.Planned{}, err
	}
	path, info, err := existingInside(root, in.File)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	if !isHTTPFile(path) {
		return mcpserver.Planned{}, fmt.Errorf("%s is not a .http or .rest file", rel(path, root))
	}
	text, err := readSource(path, info)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	var names []string
	line, found := 0, 0
	for _, r := range httpfile.Parse(text).Requests {
		if r.Name == "" {
			continue
		}
		names = append(names, r.Name)
		if r.Name == in.Request {
			line, found = r.Line, found+1
		}
	}
	switch {
	case found == 0:
		return mcpserver.Planned{}, fmt.Errorf("no request named %q in %s (named requests: %s); name one with \"### name\"", in.Request, rel(path, root), listNames(names))
	case found > 1:
		return mcpserver.Planned{}, fmt.Errorf("%d requests are named %q in %s; give them different names", found, in.Request, rel(path, root))
	}
	if in.Environment != "" {
		if err := checkName("environment", in.Environment); err != nil {
			return mcpserver.Planned{}, err
		}
		envs, err := httpfile.Environments(path)
		if err != nil {
			return mcpserver.Planned{}, err
		}
		if !slices.Contains(envs, in.Environment) {
			return mcpserver.Planned{}, fmt.Errorf("no environment %q next to %s (environments: %s)", in.Environment, rel(path, root), listNames(envs))
		}
	}
	// Body files only from inside this project, whatever else is open.
	prepared, err := httpfile.Prepare(httpfile.Target{Path: path, Text: text, Line: line, Env: in.Environment, Roots: []string{root}}, s.now)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Send %s %s\nRequest %q of %s", prepared.Method, prepared.URL, in.Request, rel(path, root))
	if in.Environment != "" {
		fmt.Fprintf(&b, ", environment %s", in.Environment)
	}
	if len(prepared.Headers) > 0 {
		headers := make([]string, 0, len(prepared.Headers))
		for _, h := range prepared.Headers {
			headers = append(headers, h.Name)
		}
		fmt.Fprintf(&b, "\nHeaders: %s", listNames(headers))
	}
	if len(prepared.Body) > 0 {
		from := ""
		if prepared.BodyPath != "" {
			from = " from " + rel(prepared.BodyPath, root)
		}
		fmt.Fprintf(&b, "\nBody: %d bytes%s", len(prepared.Body), from)
	}
	return mcpserver.Planned{Summary: b.String(), Run: func(ctx context.Context) (string, error) {
		resp, err := s.Actor.SendHTTP(ctx, root, path, prepared)
		if err != nil {
			return "", err
		}
		return describeResponse(resp), nil
	}}, nil
}

// hiddenHeaders carry session secrets; their values stay out of the answer.
var hiddenHeaders = []string{"set-cookie", "cookie", "authorization", "proxy-authorization"}

// describeResponse renders a response for the agent: the status line, the
// headers and the start of the body.
func describeResponse(r *httpfile.Response) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s (%s %s, %.0f ms, %d bytes", r.Proto, r.StatusText, r.Method, r.URL, r.TimeMs, r.Size)
	if r.ContentType != "" {
		fmt.Fprintf(&b, ", %s", r.ContentType)
	}
	b.WriteString(")\n")
	if r.FinalURL != "" && r.FinalURL != r.URL {
		fmt.Fprintf(&b, "Final URL after redirects: %s\n", r.FinalURL)
	}
	if len(r.Headers) > 0 {
		b.WriteString("\nHeaders:\n")
		for _, h := range r.Headers[:min(len(r.Headers), maxHTTPHeaders)] {
			value := h.Value
			if slices.Contains(hiddenHeaders, strings.ToLower(h.Name)) {
				value = "(hidden)"
			} else if len(value) > maxHeaderValue {
				value = cut(value, maxHeaderValue) + "…"
			}
			fmt.Fprintf(&b, "%s: %s\n", h.Name, value)
		}
		if n := len(r.Headers) - maxHTTPHeaders; n > 0 {
			fmt.Fprintf(&b, "… and %d more\n", n)
		}
	}
	switch {
	case r.Kind == httpfile.KindEmpty || r.Size == 0:
		b.WriteString("\nNo body.")
	case r.Body == "":
		fmt.Fprintf(&b, "\nBody: %s data, not shown; see the response tab in Flint.", r.Kind)
	default:
		body, note := r.Body, ""
		if len(body) > maxHTTPBody {
			body, note = cut(body, maxHTTPBody), fmt.Sprintf(" (the first %d KiB; the whole response is in Flint's response tab)", maxHTTPBody>>10)
		} else if r.Truncated || r.Incomplete {
			note = " (cut; the whole response is in Flint's response tab)"
		}
		fmt.Fprintf(&b, "\nBody%s:\n````\n%s\n````", note, body)
	}
	return strings.TrimRight(b.String(), "\n")
}
