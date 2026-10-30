package fuzzy

import (
	"strconv"
	"strings"
	"sync"
)

// Result is one quick-open answer: the top matches and the indexed file count.
type Result struct {
	Total   int     `json:"total"`
	Matches []Match `json:"matches"`
}

// Root is one indexed project folder and the name its files are shown under when
// several folders are searched together.
type Root struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// Index keeps the prepared file lists of the open project folders, so the webview
// never holds them and each keystroke only sends the query.
type Index struct {
	mu      sync.RWMutex
	sets    map[string][]Target // relative paths per indexed root
	wanted  map[string]bool     // roots still in the workspace; other listings are dropped
	loading map[string]*load    // in-flight listings, one per root
	gen     int                 // bumped whenever a set changes
	merged  *merged             // prefixed targets of the last multi-root search
}

type load struct {
	done    chan struct{}
	count   int
	err     error
	waiters int // calls sharing this listing (guarded by Index.mu)
}

// merged is the combined list "label/relative path" of several roots.
type merged struct {
	key     string
	targets []Target
	owner   []int // index into roots per target
	offset  []int // length of the "label/" prefix per root
}

// Load lists root's files with list and indexes them. Concurrent calls for the
// same root share one listing; a listing that finishes after its root left the
// workspace (Retain) is discarded. Returns the number of files.
func (x *Index) Load(root string, list func() ([]string, error)) (int, error) {
	x.mu.Lock()
	x.want(root)
	if l, ok := x.loading[root]; ok {
		l.waiters++
		x.mu.Unlock()
		<-l.done
		return l.count, l.err
	}
	l := &load{done: make(chan struct{})}
	if x.loading == nil {
		x.loading = make(map[string]*load)
	}
	x.loading[root] = l
	x.mu.Unlock()

	paths, err := list()
	var targets []Target
	if err == nil {
		targets = prepareAll(paths...)
	}

	x.mu.Lock()
	delete(x.loading, root)
	if err == nil && x.wanted[root] {
		x.store(root, targets)
	}
	x.mu.Unlock()
	l.count, l.err = len(paths), err
	close(l.done)
	return l.count, l.err
}

// Set replaces the indexed files of root.
func (x *Index) Set(root string, paths []string) {
	targets := prepareAll(paths...)
	x.mu.Lock()
	x.want(root)
	x.store(root, targets)
	x.mu.Unlock()
}

// Retain keeps only the given roots (the folders open in the workspace).
func (x *Index) Retain(roots []string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	keep := make(map[string]bool, len(roots))
	for _, r := range roots {
		keep[r] = true
	}
	x.wanted = keep
	for r := range x.sets {
		if !keep[r] {
			delete(x.sets, r)
			x.gen++
		}
	}
}

// Search ranks the files of roots. With several roots, paths are shown as
// "label/relative" (so the project name can be typed) and each match says which
// root it belongs to and where the relative path starts. ok is false when one of
// the roots is not indexed yet.
func (x *Index) Search(roots []Root, query string, limit int) (Result, bool) {
	if len(roots) == 0 {
		return Result{Matches: []Match{}}, true
	}
	x.mu.RLock()
	for _, r := range roots {
		if _, ok := x.sets[r.Path]; !ok {
			x.mu.RUnlock()
			return Result{}, false
		}
	}
	if len(roots) == 1 {
		defer x.mu.RUnlock()
		targets := x.sets[roots[0].Path]
		matches, _ := search(query, targets, limit)
		for i := range matches {
			matches[i].Root = roots[0].Path
		}
		return Result{Total: len(targets), Matches: matches}, true
	}
	m := x.merged
	key := mergeKey(roots, x.gen)
	if m == nil || m.key != key {
		m = x.merge(roots, key)
	}
	x.mu.RUnlock()

	x.mu.Lock()
	x.merged = m
	x.mu.Unlock()

	matches, idx := search(query, m.targets, limit)
	for i, ti := range idx {
		owner := m.owner[ti]
		matches[i].Root = roots[owner].Path
		matches[i].Offset = m.offset[owner]
	}
	return Result{Total: len(m.targets), Matches: matches}, true
}

// merge builds the combined target list; the caller holds at least a read lock.
func (x *Index) merge(roots []Root, key string) *merged {
	total := 0
	for _, r := range roots {
		total += len(x.sets[r.Path])
	}
	m := &merged{key: key, targets: make([]Target, 0, total), owner: make([]int, 0, total), offset: make([]int, len(roots))}
	for i, r := range roots {
		prefix := r.Label + "/"
		m.offset[i] = len([]rune(prefix))
		for _, t := range x.sets[r.Path] {
			m.targets = append(m.targets, Prepare(prefix+t.Path))
			m.owner = append(m.owner, i)
		}
	}
	return m
}

func mergeKey(roots []Root, gen int) string {
	var b strings.Builder
	for _, r := range roots {
		b.WriteString(r.Path)
		b.WriteByte(0)
		b.WriteString(r.Label)
		b.WriteByte(0)
	}
	b.WriteString(strconv.Itoa(gen))
	return b.String()
}

// want marks root as part of the workspace; the caller holds the write lock.
func (x *Index) want(root string) {
	if x.wanted == nil {
		x.wanted = make(map[string]bool)
	}
	x.wanted[root] = true
}

// store replaces root's targets; the caller holds the write lock.
func (x *Index) store(root string, targets []Target) {
	if x.sets == nil {
		x.sets = make(map[string][]Target)
	}
	x.sets[root] = targets
	x.gen++
}

func prepareAll(paths ...string) []Target {
	targets := make([]Target, len(paths))
	for i, p := range paths {
		targets[i] = Prepare(p)
	}
	return targets
}
