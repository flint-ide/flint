package fuzzy

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var files = prepareAll(
	"internal/db/store.go",
	"internal/db/store_test.go",
	"frontend/src/store/editor.ts",
	"frontend/src/components/editor/FileView.tsx",
	"README.md",
	"api_workspace.go",
	"frontend/src/components/sidebar/SearchPanel.tsx",
)

func first(t *testing.T, query string) string {
	t.Helper()
	m := Search(query, files, 5)
	if len(m) == 0 {
		t.Fatalf("no match for %q", query)
	}
	return m[0].Path
}

func TestMatchesSubsequencesOnly(t *testing.T) {
	if len(Search("stgo", prepareAll("internal/db/store.go"), 5)) != 1 {
		t.Fatal("stgo should match")
	}
	if len(Search("xyz", prepareAll("internal/db/store.go"), 5)) != 0 {
		t.Fatal("xyz should not match")
	}
}

func TestRanking(t *testing.T) {
	tests := []struct{ query, want string }{
		{"fileview", "frontend/src/components/editor/FileView.tsx"},
		{"store.go", "internal/db/store.go"}, // shorter exact name above its test sibling
		{"SP", "frontend/src/components/sidebar/SearchPanel.tsx"},
	}
	for _, tt := range tests {
		if got := first(t, tt.query); got != tt.want {
			t.Errorf("Search(%q)[0] = %s, want %s", tt.query, got, tt.want)
		}
	}
}

// Jumping to a later word boundary must not strand the rest of the query.
func TestBoundaryJumpKeepsMatchesReachable(t *testing.T) {
	for _, tt := range []struct{ query, path string }{
		{"ab", "src/xab_a.go"},
		{"xab_a", "src/xab_a.go"},
	} {
		m := Search(tt.query, prepareAll(tt.path), 1)
		if len(m) != 1 {
			t.Errorf("Search(%q) found no match in %s", tt.query, tt.path)
		}
	}
}

func TestEmptyQueryHonoursLimit(t *testing.T) {
	m := Search("", files, 3)
	if len(m) != 3 || m[0].Path != "internal/db/store.go" {
		t.Fatalf("got %+v", m)
	}
}

func TestIndicesSpellQuery(t *testing.T) {
	m := Search("rdm", prepareAll("README.md"), 1)
	runes := []rune("README.md")
	var sb strings.Builder
	for _, i := range m[0].Indices {
		sb.WriteString(strings.ToLower(string(runes[i])))
	}
	if sb.String() != "rdm" {
		t.Fatalf("indices %v spell %q", m[0].Indices, sb.String())
	}
}

func TestNonASCIIIndicesAreCodePoints(t *testing.T) {
	m := Search("RÓ", prepareAll("docs/źródło.md"), 1)
	if len(m) != 1 || fmt.Sprint(m[0].Indices) != "[6 7]" {
		t.Fatalf("got %+v", m)
	}
}

func TestTopNKeepsBest(t *testing.T) {
	var paths []string
	for i := range 1000 {
		paths = append(paths, fmt.Sprintf("pkg/dir%d/file%d.go", i, i))
	}
	paths = append(paths, "main.go")
	m := Search("main", prepareAll(paths...), 3)
	if len(m) != 1 || m[0].Path != "main.go" {
		t.Fatalf("got %+v", m)
	}
	m = Search("file", prepareAll(paths...), 10)
	if len(m) != 10 {
		t.Fatalf("got %d results", len(m))
	}
	// Equal scores fall back to shorter path, then listing order.
	if m[0].Path != "pkg/dir0/file0.go" {
		t.Fatalf("first = %s", m[0].Path)
	}
}

func one(root string) []Root { return []Root{{Path: root, Label: root}} }

func TestIndex(t *testing.T) {
	var x Index
	if _, ok := x.Search(one("/p"), "a", 5); ok {
		t.Fatal("unindexed root reported ok")
	}
	x.Set("/p", []string{"a.go", "b.go"})
	r, ok := x.Search(one("/p"), "a", 5)
	if !ok || r.Total != 2 || len(r.Matches) != 1 {
		t.Fatalf("got %+v %v", r, ok)
	}
	if m := r.Matches[0]; m.Path != "a.go" || m.Root != "/p" || m.Offset != 0 {
		t.Fatalf("single-root match %+v", m)
	}
	if _, ok := x.Search(one("/other"), "a", 5); ok {
		t.Fatal("other root reported ok")
	}
}

func TestIndexSearchesSeveralRoots(t *testing.T) {
	var x Index
	x.Set("/w/api", []string{"main.go", "handler.go"})
	x.Set("/w/web", []string{"src/main.ts"})
	roots := []Root{{Path: "/w/api", Label: "api"}, {Path: "/w/web", Label: "web"}}
	if _, ok := x.Search(append(roots, Root{Path: "/w/x", Label: "x"}), "", 5); ok {
		t.Fatal("search with an unindexed root reported ok")
	}

	r, ok := x.Search(roots, "main", 10)
	if !ok || r.Total != 3 || len(r.Matches) != 2 {
		t.Fatalf("got %+v %v", r, ok)
	}
	byRoot := map[string]Match{}
	for _, m := range r.Matches {
		byRoot[m.Root] = m
	}
	api, web := byRoot["/w/api"], byRoot["/w/web"]
	if api.Path != "api/main.go" || api.Offset != 4 || web.Path != "web/src/main.ts" || web.Offset != 4 {
		t.Fatalf("matches %+v", r.Matches)
	}

	// The project name can be typed to narrow the list.
	r, _ = x.Search(roots, "web/main", 10)
	if len(r.Matches) != 1 || r.Matches[0].Root != "/w/web" {
		t.Fatalf("web/main = %+v", r.Matches)
	}

	// A changed listing is picked up by the next multi-root search.
	x.Set("/w/web", []string{"src/main.ts", "src/app.ts"})
	if r, _ = x.Search(roots, "", 10); r.Total != 4 {
		t.Fatalf("after Set total = %d", r.Total)
	}

	x.Retain([]string{"/w/api"})
	if _, ok := x.Search(roots, "", 10); ok {
		t.Fatal("removed root still indexed")
	}
}

func TestLoadSharesConcurrentListings(t *testing.T) {
	var x Index
	var calls atomic.Int32
	release := make(chan struct{})
	list := func() ([]string, error) {
		calls.Add(1)
		<-release
		return []string{"a.go", "b.go"}, nil
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if n, err := x.Load("/p", list); err != nil || n != 2 {
				t.Errorf("Load = %d, %v", n, err)
			}
		})
	}
	// Release the listing only once the other four calls joined it.
	for {
		x.mu.RLock()
		l := x.loading["/p"]
		joined := l != nil && l.waiters == 4
		x.mu.RUnlock()
		if joined {
			break
		}
		runtime.Gosched()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("listed %d times, want 1", calls.Load())
	}
	if r, ok := x.Search(one("/p"), "", 10); !ok || r.Total != 2 {
		t.Fatalf("Search = %+v, %v", r, ok)
	}
}

func TestLoadDropsListingOfAbandonedRoot(t *testing.T) {
	var x Index
	slow := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = x.Load("/old", func() ([]string, error) {
			<-slow
			return []string{"old.go"}, nil
		})
	}()
	for {
		x.mu.RLock()
		started := x.loading["/old"] != nil
		x.mu.RUnlock()
		if started {
			break
		}
		runtime.Gosched()
	}
	// /old leaves the workspace while it is still being listed.
	x.Retain([]string{"/new"})
	if _, err := x.Load("/new", func() ([]string, error) { return []string{"new.go"}, nil }); err != nil {
		t.Fatal(err)
	}
	close(slow)
	<-done
	if _, ok := x.Search(one("/new"), "", 10); !ok {
		t.Fatal("/new is not indexed")
	}
	if _, ok := x.Search(one("/old"), "", 10); ok {
		t.Fatal("listing of a removed root was kept")
	}
}

func BenchmarkSearch200k(b *testing.B) {
	paths := make([]string, 200_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("src/module%d/sub%d/component_%d_view.tsx", i%300, i%17, i)
	}
	targets := prepareAll(paths...)
	for b.Loop() {
		Search("cmpview", targets, 60)
	}
}
