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

func TestIndex(t *testing.T) {
	var x Index
	if _, ok := x.Search("/p", "a", 5); ok {
		t.Fatal("unindexed root reported ok")
	}
	x.Set("/p", []string{"a.go", "b.go"})
	r, ok := x.Search("/p", "a", 5)
	if !ok || r.Total != 2 || len(r.Matches) != 1 {
		t.Fatalf("got %+v %v", r, ok)
	}
	if _, ok := x.Search("/other", "a", 5); ok {
		t.Fatal("other root reported ok")
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
	if r, ok := x.Search("/p", "", 10); !ok || r.Total != 2 {
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
	if _, err := x.Load("/new", func() ([]string, error) { return []string{"new.go"}, nil }); err != nil {
		t.Fatal(err)
	}
	close(slow)
	<-done
	if _, ok := x.Search("/new", "", 10); !ok {
		t.Fatal("slow listing of /old replaced the index of /new")
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
