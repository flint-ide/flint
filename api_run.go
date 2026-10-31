package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"flint/internal/runconfig"
	"flint/internal/runner"
	"flint/internal/testrun"
)

// eventRun streams a run: console output, test results and finally the exit,
// all in one sequence so the UI can restore their order.
const eventRun = "run:event"

// RunEvent is one step of a run's stream.
type RunEvent struct {
	ID  string `json:"id"`
	Seq uint64 `json:"seq"` // per run, from 1
	// Data is console output (base64 in JSON, like terminal output).
	Data  []byte         `json:"data,omitempty"`
	Tests []testrun.Node `json:"tests,omitempty"`
	Exit  *runner.Result `json:"exit,omitempty"`
}

// testSink collects parsed results between console events.
type testSink struct {
	parser testrun.Parser

	mu      sync.Mutex
	pending []testrun.Node
}

func (s *testSink) add(nodes []testrun.Node) {
	if len(nodes) == 0 {
		return
	}
	s.mu.Lock()
	s.pending = append(s.pending, nodes...)
	s.mu.Unlock()
}

func (s *testSink) take() []testrun.Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.pending
	s.pending = nil
	return out
}

// runEvents forwards runner output to the frontend.
type runEvents struct {
	app *App

	mu    sync.Mutex
	seq   map[string]uint64
	tests map[string]*testSink
}

func newRunEvents(app *App) *runEvents {
	return &runEvents{app: app, seq: map[string]uint64{}, tests: map[string]*testSink{}}
}

func (e *runEvents) sink(id string) *testSink {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tests[id]
}

func (e *runEvents) Output(id string, data []byte) {
	var tests []testrun.Node
	if s := e.sink(id); s != nil {
		tests = s.take()
	}
	if len(data) == 0 && len(tests) == 0 {
		return
	}
	e.emit(RunEvent{ID: id, Data: data, Tests: tests})
}

func (e *runEvents) Exited(id string, res runner.Result) {
	var tests []testrun.Node
	if s := e.sink(id); s != nil {
		// The process is gone, so the parser is no longer used by the runner.
		tests = append(s.take(), s.parser.Finish(res.Code, res.Stopped)...)
	}
	e.emit(RunEvent{ID: id, Tests: tests, Exit: &res})
	e.mu.Lock()
	delete(e.seq, id)
	delete(e.tests, id)
	e.mu.Unlock()
}

func (e *runEvents) emit(ev RunEvent) {
	e.mu.Lock()
	defer e.mu.Unlock() // stamp and emit together so sequence order equals send order
	e.seq[ev.ID]++
	ev.Seq = e.seq[ev.ID]
	e.app.emit(eventRun, ev)
}

// RunConfigs lists a project's run configurations.
type RunConfigs struct {
	// Saved come from .flint/run.json.
	Saved []runconfig.Config `json:"saved"`
	// Detected need no setup (Go main packages, make targets, package.json scripts).
	Detected     []runconfig.Config                `json:"detected"`
	Capabilities map[string]runconfig.Capabilities `json:"capabilities"`
	// Error explains why .flint/run.json could not be used ("" when fine).
	Error string `json:"error"`
	// Path of the configuration file.
	Path string `json:"path"`
}

// RunAPI runs configurations and tests (IntelliJ-style Run / Debug toolbar).
type RunAPI struct {
	app *App

	// Detection walks the project; the selector reloads configurations every
	// time it opens, so recent results are reused.
	detectMu sync.Mutex
	detected map[string]detection
}

type detection struct {
	at      time.Time
	configs []runconfig.Config
}

// detectTTL is how long detected configurations are reused for a root.
const detectTTL = 30 * time.Second

func (r *RunAPI) detect(root string) []runconfig.Config {
	r.detectMu.Lock()
	defer r.detectMu.Unlock()
	if d, ok := r.detected[root]; ok && time.Since(d.at) < detectTTL {
		return d.configs
	}
	configs := runconfig.Detect(root)
	if r.detected == nil {
		r.detected = map[string]detection{}
	}
	for k, d := range r.detected {
		if time.Since(d.at) >= detectTTL {
			delete(r.detected, k) // projects not looked at lately
		}
	}
	r.detected[root] = detection{at: time.Now(), configs: configs}
	return configs
}

// Configs loads the saved and detected configurations of root.
func (r *RunAPI) Configs(root string) (RunConfigs, error) {
	if err := checkRoot(root); err != nil {
		return RunConfigs{}, err
	}
	out := RunConfigs{Saved: []runconfig.Config{}, Detected: r.detect(root), Path: runconfig.Path(root)}
	saved, err := runconfig.Load(root)
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Saved = saved
	}
	if out.Detected == nil {
		out.Detected = []runconfig.Config{}
	}
	out.Capabilities = map[string]runconfig.Capabilities{}
	for _, c := range append(append([]runconfig.Config{}, out.Saved...), out.Detected...) {
		out.Capabilities[c.ID] = runconfig.CapabilitiesOf(c)
	}
	return out, nil
}

// SaveConfigs replaces root's .flint/run.json and returns what was saved (ids filled in).
func (r *RunAPI) SaveConfigs(root string, configs []runconfig.Config) ([]runconfig.Config, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}
	if err := runconfig.Save(root, configs); err != nil {
		return nil, err
	}
	return runconfig.Load(root)
}

// Start runs a configuration (saved or detected) under id, chosen by the
// frontend. cols/rows size the PTY.
func (r *RunAPI) Start(id, root string, cfg runconfig.Config, cols, rows int) error {
	if err := checkRoot(root); err != nil {
		return err
	}
	launch, err := runconfig.Resolve(root, cfg)
	if err != nil {
		return err
	}
	return r.app.runs.Start(id, runner.Spec{
		Steps: launch.Steps, Cwd: launch.Cwd, Env: launch.Env, PTY: true, Cols: cols, Rows: rows,
	})
}

// StartTests runs tests under id and returns the run's title. Results stream
// as RunEvent.Tests.
func (r *RunAPI) StartTests(id string, req testrun.Request) (string, error) {
	plan, err := testrun.PlanRun(req)
	if err != nil {
		return "", err
	}
	if err := r.app.startTestPlan(id, plan); err != nil {
		return "", err
	}
	return plan.Title, nil
}

// startTestPlan runs a planned test run under id, parsing its output into
// RunEvent.Tests (also used by profiled test runs, see ProfileAPI).
func (a *App) startTestPlan(id string, plan testrun.Plan) error {
	sink := &testSink{parser: plan.NewParser()}
	a.runEvents.mu.Lock()
	a.runEvents.tests[id] = sink
	a.runEvents.mu.Unlock()
	err := a.runs.Start(id, runner.Spec{
		Steps: plan.Steps, Cwd: plan.Cwd, Env: plan.Env,
		Filter: func(line []byte) []byte {
			shown, nodes := sink.parser.Line(line)
			sink.add(nodes)
			if shown == nil {
				return line
			}
			return shown
		},
	})
	if err != nil {
		a.runEvents.mu.Lock()
		delete(a.runEvents.tests, id)
		a.runEvents.mu.Unlock()
	}
	return err
}

// Stop interrupts a run and returns once it ended.
func (r *RunAPI) Stop(id string) { r.app.runs.Stop(id) }

// Write sends input to a running configuration's PTY.
func (r *RunAPI) Write(id, data string) error { return r.app.runs.Write(id, []byte(data)) }

// Resize updates a run's PTY size.
func (r *RunAPI) Resize(id string, cols, rows int) error { return r.app.runs.Resize(id, cols, rows) }

// TestMarks finds runnable tests in a file's (unsaved) content for the gutter.
func (r *RunAPI) TestMarks(path, content string) []testrun.Mark {
	marks := testrun.Marks(path, content)
	if marks == nil {
		return []testrun.Mark{}
	}
	return marks
}

func checkRoot(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("project root %q is not absolute", root)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return fmt.Errorf("project root %s is not a directory", root)
	}
	return nil
}
