package fuzzy

import "sync"

// Result is one quick-open answer: the top matches and the indexed file count.
type Result struct {
	Total   int     `json:"total"`
	Matches []Match `json:"matches"`
}

// Index keeps the prepared file list of the open project, so the webview never
// holds it and each keystroke only sends the query.
type Index struct {
	mu      sync.RWMutex
	root    string
	targets []Target
	wanted  string           // root of the latest Load: older listings are dropped
	loading map[string]*load // in-flight listings, one per root
}

type load struct {
	done    chan struct{}
	count   int
	err     error
	waiters int // calls sharing this listing (guarded by Index.mu)
}

// Load lists root's files with list and indexes them. Concurrent calls for the
// same root share one listing; a listing that finishes after another root was
// requested is discarded. Returns the number of files.
func (x *Index) Load(root string, list func() ([]string, error)) (int, error) {
	x.mu.Lock()
	x.wanted = root
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
	if err == nil && x.wanted == root {
		x.root, x.targets = root, targets
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
	x.root, x.targets, x.wanted = root, targets, root
	x.mu.Unlock()
}

// Search ranks the files of root; ok is false when root is not indexed.
func (x *Index) Search(root, query string, limit int) (Result, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if x.root != root {
		return Result{}, false
	}
	return Result{Total: len(x.targets), Matches: Search(query, x.targets, limit)}, true
}

func prepareAll(paths ...string) []Target {
	targets := make([]Target, len(paths))
	for i, p := range paths {
		targets[i] = Prepare(p)
	}
	return targets
}
