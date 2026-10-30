package testrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPlanProfile(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "calc")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()

	plan, files, err := PlanProfile(Request{Lang: LangGo, Dirs: []string{pkg}, Tests: []string{"TestAdd"}}, out, true)
	if err != nil {
		t.Fatal(err)
	}
	if files.CPU != filepath.Join(out, "cpu.pprof") || files.Mem != filepath.Join(out, "mem.pprof") {
		t.Fatalf("files %+v", files)
	}
	want := []string{"test", "-json", "-cpuprofile", files.CPU, "-memprofile", files.Mem, "-o", filepath.Join(out, "test.bin"), "-run", "^TestAdd$", "./calc"}
	if got := plan.Steps[0].Args; !slices.Equal(got, want) {
		t.Fatalf("args\n got %q\nwant %q", got, want)
	}
	if !strings.HasPrefix(plan.Title, "Profile ") || plan.Format != FormatGoJSON || !strings.Contains(plan.Steps[0].Label, "-cpuprofile") {
		t.Fatalf("plan %+v", plan)
	}

	_, cpuOnly, err := PlanProfile(Request{Lang: LangGo, Dirs: []string{pkg}}, out, false)
	if err != nil || cpuOnly.Mem != "" {
		t.Fatalf("cpu only: %+v %v", cpuOnly, err)
	}
	for name, req := range map[string]Request{
		"js":           {Lang: LangJS, Dirs: []string{pkg}},
		"two packages": {Lang: LangGo, Dirs: []string{pkg, root}},
	} {
		if _, _, err := PlanProfile(req, out, false); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, _, err := PlanProfile(Request{Lang: LangGo, Dirs: []string{pkg}}, "rel", false); err == nil {
		t.Error("relative directory accepted")
	}
}

// TestPlanProfileRealRun profiles a tiny package with the real go tool: the
// profiles exist afterwards and nothing is left in the package directory.
func TestPlanProfileRealRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a test binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	root := t.TempDir()
	pkg := filepath.Join(root, "busy")
	_ = os.MkdirAll(pkg, 0o755)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/busy\n\ngo 1.21\n"), 0o644)
	_ = os.WriteFile(filepath.Join(pkg, "busy_test.go"), []byte(`package busy

import "testing"

func TestBusy(t *testing.T) {
	n := 0
	for i := 0; i < 50_000_000; i++ {
		n += i % 7
	}
	if n < 0 {
		t.Fatal(n)
	}
}
`), 0o644)
	out := t.TempDir()
	plan, files, err := PlanProfile(Request{Lang: LangGo, Dirs: []string{pkg}}, out, true)
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Steps[0]
	cmd := exec.Command(step.Program, step.Args...)
	cmd.Dir = plan.Cwd
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, msg)
	}
	for _, f := range []string{files.CPU, files.Mem} {
		if info, err := os.Stat(f); err != nil || info.Size() == 0 {
			t.Errorf("%s: %v", f, err)
		}
	}
	entries, _ := os.ReadDir(pkg)
	if len(entries) != 1 {
		t.Errorf("package directory polluted: %v", entries)
	}
}
