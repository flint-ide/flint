package profiling

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/google/pprof/profile"
)

// builder assembles a profile programmatically: one location per function.
type builder struct {
	p     *profile.Profile
	funcs map[string]*profile.Location
}

func newBuilder(types ...string) *builder {
	p := &profile.Profile{}
	for _, t := range types {
		p.SampleType = append(p.SampleType, &profile.ValueType{Type: t, Unit: "count"})
	}
	return &builder{p: p, funcs: map[string]*profile.Location{}}
}

func (b *builder) loc(name string) *profile.Location {
	if l := b.funcs[name]; l != nil {
		return l
	}
	id := uint64(len(b.funcs) + 1)
	fn := &profile.Function{ID: id, Name: name, Filename: "/src/" + name + ".go", StartLine: int64(10 * id)}
	l := &profile.Location{ID: id, Line: []profile.Line{{Function: fn, Line: int64(10*id + 2)}}}
	b.p.Function = append(b.p.Function, fn)
	b.p.Location = append(b.p.Location, l)
	b.funcs[name] = l
	return l
}

// sample adds a stack given root first ("main", "a", "b"); pprof stores it leaf first.
func (b *builder) sample(stack string, values ...int64) {
	names := strings.Split(stack, ";")
	locs := make([]*profile.Location, len(names))
	for i, n := range names {
		locs[len(names)-1-i] = b.loc(n)
	}
	b.p.Sample = append(b.p.Sample, &profile.Sample{Location: locs, Value: values})
}

func child(t *testing.T, f Frame, name string) Frame {
	t.Helper()
	for _, c := range f.Children {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("%s has no child %s (children: %v)", f.Name, name, names(f.Children))
	return Frame{}
}

func names(frames []Frame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = f.Name
	}
	return out
}

func topEntry(t *testing.T, r Report, name string) TopEntry {
	t.Helper()
	for _, e := range r.Top {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no %s in top", name)
	return TopEntry{}
}

func TestBuildCallTree(t *testing.T) {
	b := newBuilder("samples", "cpu")
	b.sample("main;a;b", 6, 600)
	b.sample("main;a", 2, 200)
	b.sample("main;c", 1, 150)
	b.sample("main;a;b", 1, 50) // merges into the same frames

	r, err := Build(b.p, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.SampleType != "cpu" || r.Unit != "count" || r.Total != 1000 {
		t.Fatalf("type %q unit %q total %d", r.SampleType, r.Unit, r.Total)
	}
	if got := strings.Join(r.SampleTypes, ","); got != "samples,cpu" {
		t.Fatalf("sample types %s", got)
	}
	if r.Root.Name != RootName || r.Root.Total != 1000 || r.Root.Self != 0 {
		t.Fatalf("root %+v", r.Root)
	}
	main := child(t, r.Root, "main")
	a := child(t, main, "a")
	bb := child(t, a, "b")
	c := child(t, main, "c")
	if main.Total != 1000 || a.Total != 850 || a.Self != 200 || bb.Total != 650 || bb.Self != 650 || c.Total != 150 {
		t.Fatalf("main %d, a %d/%d, b %d/%d, c %d", main.Total, a.Total, a.Self, bb.Total, bb.Self, c.Total)
	}
	if got := names(main.Children); strings.Join(got, ",") != "a,c" {
		t.Fatalf("children not heaviest first: %v", got)
	}
	if bb.File != "/src/b.go" || bb.Line != 30 {
		t.Fatalf("b location %s:%d", bb.File, bb.Line)
	}
	if r.Frames != 5 || r.Trimmed != 0 {
		t.Fatalf("frames %d trimmed %d", r.Frames, r.Trimmed)
	}

	if e := topEntry(t, r, "a"); e.Flat != 200 || e.Cum != 850 {
		t.Fatalf("a: %+v", e)
	}
	if e := topEntry(t, r, "main"); e.Flat != 0 || e.Cum != 1000 {
		t.Fatalf("main: %+v", e)
	}
	if r.Top[0].Name != "b" {
		t.Fatalf("top not sorted by flat: %v", r.Top)
	}

	samples, err := Build(b.p, "samples")
	if err != nil || samples.Total != 10 {
		t.Fatalf("samples: total %d, %v", samples.Total, err)
	}
	if _, err := Build(b.p, "alloc_space"); err == nil {
		t.Fatal("unknown sample type accepted")
	}
}

func TestBuildTrimsSmallFrames(t *testing.T) {
	b := newBuilder("cpu")
	b.sample("main;hot;leaf", 996)
	b.sample("main;tiny;deeper", 4) // 0.4% < 0.5%

	r, err := Build(b.p, "")
	if err != nil {
		t.Fatal(err)
	}
	main := child(t, r.Root, "main")
	if len(main.Children) != 1 || main.Children[0].Name != "hot" {
		t.Fatalf("tiny frame kept: %v", names(main.Children))
	}
	if main.Total != 1000 {
		t.Fatalf("trimmed value must stay in the parent's total, got %d", main.Total)
	}
	if r.Trimmed != 2 || r.Frames != 4 {
		t.Fatalf("trimmed %d frames %d", r.Trimmed, r.Frames)
	}
	// The top table still knows the trimmed function.
	if e := topEntry(t, r, "deeper"); e.Flat != 4 {
		t.Fatalf("deeper: %+v", e)
	}
}

func TestBuildRecursionAndInlining(t *testing.T) {
	b := newBuilder("cpu")
	b.sample("main;rec;rec;rec", 10)
	// One location with an inlined call: Line holds the innermost function first.
	outer := &profile.Function{ID: 90, Name: "outer", Filename: "/src/o.go", StartLine: 5}
	inner := &profile.Function{ID: 91, Name: "inner", Filename: "/src/o.go", StartLine: 20}
	loc := &profile.Location{ID: 90, Line: []profile.Line{{Function: inner, Line: 21}, {Function: outer, Line: 7}}}
	b.p.Function = append(b.p.Function, outer, inner)
	b.p.Location = append(b.p.Location, loc)
	b.p.Sample = append(b.p.Sample, &profile.Sample{Location: []*profile.Location{loc, b.loc("main")}, Value: []int64{30}})

	r, err := Build(b.p, "")
	if err != nil {
		t.Fatal(err)
	}
	if e := topEntry(t, r, "rec"); e.Cum != 10 || e.Flat != 10 {
		t.Fatalf("recursion counted more than once: %+v", e)
	}
	in := child(t, child(t, child(t, r.Root, "main"), "outer"), "inner")
	if in.Self != 30 || in.Line != 20 {
		t.Fatalf("inlined frame %+v", in)
	}
}

func TestBuildCapsDepth(t *testing.T) {
	b := newBuilder("cpu")
	stack := make([]string, MaxDepth+20)
	for i := range stack {
		stack[i] = "f" + strings.Repeat("x", i%7) + string(rune('a'+i%26))
	}
	b.sample(strings.Join(stack, ";"), 5)
	r, err := Build(b.p, "")
	if err != nil {
		t.Fatal(err)
	}
	depth, f := 0, r.Root
	for len(f.Children) > 0 {
		f = f.Children[0]
		depth++
	}
	if depth != MaxDepth || f.Self != 5 {
		t.Fatalf("depth %d, deepest self %d", depth, f.Self)
	}
}

//go:noinline
func spin(until time.Time) int {
	n := 0
	for time.Now().Before(until) {
		for i := range 10000 {
			n += i % 7
		}
	}
	return n
}

// TestLoadRealCPUProfile records a real profile of a busy loop.
func TestLoadRealCPUProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu.pprof")
	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		t.Skipf("CPU profiling unavailable (already profiling?): %v", err)
	}
	spin(time.Now().Add(400 * time.Millisecond))
	pprof.StopCPUProfile()
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.SampleType != "cpu" || r.Unit != "nanoseconds" || r.Path != path {
		t.Fatalf("type %q unit %q path %q", r.SampleType, r.Unit, r.Path)
	}
	if r.Total == 0 {
		t.Skip("no CPU samples recorded (overloaded machine)")
	}
	found := false
	for _, e := range r.Top {
		if strings.HasSuffix(e.Name, "profiling.spin") {
			found = true
			if e.File == "" || !strings.HasSuffix(e.File, "report_test.go") || e.Line == 0 {
				t.Fatalf("spin location %s:%d", e.File, e.Line)
			}
		}
	}
	if !found {
		t.Fatalf("spin not in top: %v", r.Top)
	}

	// Loaded again from the cache: same result.
	again, err := Load(path, "samples")
	if err != nil || again.SampleType != "samples" {
		t.Fatalf("reload: %v %q", err, again.SampleType)
	}
}

func TestLoadRejectsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pprof")
	junk := filepath.Join(dir, "junk.pprof")
	_ = os.WriteFile(empty, nil, 0o600)
	_ = os.WriteFile(junk, []byte("not a profile"), 0o600)
	for _, p := range []string{empty, junk, dir, filepath.Join(dir, "missing")} {
		if _, err := Load(p, ""); err == nil {
			t.Errorf("%s loaded", p)
		}
	}
}
