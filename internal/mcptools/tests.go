package mcptools

import (
	"path/filepath"
	"slices"
	"sync"
	"time"

	"flint/internal/testrun"
)

// Bounds of the kept test runs.
const (
	maxRuns        = 16
	maxNodesPerRun = 20000
)

// Tests keeps the results of the latest test runs. It is safe for
// concurrent use.
type Tests struct {
	mu   sync.Mutex
	runs []*testRunRec // oldest first
}

type testRunRec struct {
	id, title, dir string
	started        time.Time
	nodes          map[string]testrun.Node
	order          []string
	done           bool
	code           int
	stopped        bool
	elapsedMs      int64
	err            string
}

// TestRun is a copy of one run's state.
type TestRun struct {
	Title     string
	Dir       string
	Started   time.Time
	Done      bool
	ExitCode  int
	Stopped   bool
	ElapsedMs int64
	Error     string
	Nodes     []testrun.Node // in the order they were first reported
}

// NewTests returns an empty store.
func NewTests() *Tests { return &Tests{} }

// Begin records a new run id in dir.
func (t *Tests) Begin(id, title, dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs = slices.DeleteFunc(t.runs, func(r *testRunRec) bool { return r.id == id })
	if len(t.runs) >= maxRuns {
		t.runs = slices.Delete(t.runs, 0, len(t.runs)-maxRuns+1)
	}
	t.runs = append(t.runs, &testRunRec{id: id, title: title, dir: filepath.Clean(dir), started: time.Now(), nodes: map[string]testrun.Node{}})
}

func (t *Tests) runLocked(id string) *testRunRec {
	for _, r := range t.runs {
		if r.id == id {
			return r
		}
	}
	return nil
}

// Update applies result updates of run id (later states replace earlier ones).
func (t *Tests) Update(id string, nodes []testrun.Node) {
	if len(nodes) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.runLocked(id)
	if r == nil {
		return
	}
	for _, n := range nodes {
		if _, known := r.nodes[n.ID]; !known {
			if len(r.order) >= maxNodesPerRun {
				continue
			}
			r.order = append(r.order, n.ID)
		}
		r.nodes[n.ID] = n
	}
}

// Finish records how run id ended.
func (t *Tests) Finish(id string, code int, stopped bool, elapsedMs int64, errText string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r := t.runLocked(id); r != nil {
		r.done, r.code, r.stopped, r.elapsedMs, r.err = true, code, stopped, elapsedMs, errText
	}
}

// Last returns the latest run whose directory is in root.
func (t *Tests) Last(root string) (TestRun, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range slices.Backward(t.runs) {
		if !within(r.dir, root) {
			continue
		}
		out := TestRun{
			Title: r.title, Dir: r.dir, Started: r.started, Done: r.done, ExitCode: r.code,
			Stopped: r.stopped, ElapsedMs: r.elapsedMs, Error: r.err, Nodes: make([]testrun.Node, 0, len(r.order)),
		}
		for _, id := range r.order {
			out.Nodes = append(out.Nodes, r.nodes[id])
		}
		return out, true
	}
	return TestRun{}, false
}
