package runner

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/runconfig"
)

type recorder struct {
	mu     sync.Mutex
	out    bytes.Buffer
	calls  int
	exited chan Result
}

func newRecorder() *recorder { return &recorder{exited: make(chan Result, 1)} }

func (r *recorder) Output(_ string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.out.Write(data)
}

func (r *recorder) Exited(_ string, res Result) { r.exited <- res }

func (r *recorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func (r *recorder) wait(t *testing.T) Result {
	t.Helper()
	select {
	case res := <-r.exited:
		return res
	case <-time.After(10 * time.Second):
		t.Fatalf("run did not finish; output so far: %q", r.output())
		return Result{}
	}
}

func sh(line string) runconfig.Command {
	return runconfig.Command{Label: line, Program: "/bin/sh", Args: []string{"-c", line}}
}

func TestStepsInOrderAndExitCode(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	err := m.Start("a", Spec{Cwd: t.TempDir(), Env: []string{"FLINT_X=42"}, Steps: []runconfig.Command{
		sh("echo build"),
		sh("echo run $FLINT_X; echo oops >&2; exit 3"),
		sh("echo never"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	res := rec.wait(t)
	out := rec.output()
	if res.Code != 3 || res.Stopped || res.Error != "" {
		t.Errorf("result = %+v", res)
	}
	for _, want := range []string{"$ echo build", "build\r\n", "run 42\r\n", "oops\r\n", "exit code 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, "never") {
		t.Errorf("a failed step must stop the run: %q", out)
	}
	if m.Running("a") {
		t.Error("run still registered after exit")
	}
}

func TestFilterSeesLastStepLines(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	var mu sync.Mutex
	var seen []string
	err := m.Start("f", Spec{
		Steps: []runconfig.Command{sh("echo first"), sh("printf 'a\\nb\\nhidden\\ntail'")},
		Filter: func(line []byte) []byte {
			mu.Lock()
			seen = append(seen, string(line))
			mu.Unlock()
			if strings.HasPrefix(string(line), "hidden") {
				return []byte{}
			}
			return bytes.ToUpper(line)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res := rec.wait(t); res.Code != 0 {
		t.Fatalf("result %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(seen, "|") != "a\n|b\n|hidden\n|tail" {
		t.Errorf("filter saw %q", seen)
	}
	out := rec.output()
	if !strings.Contains(out, "first\r\n") || !strings.Contains(out, "A\r\nB\r\nTAIL") || strings.Contains(out, "hidden\r\n") {
		t.Errorf("output %q", out)
	}
}

func TestStopKillsProcessGroup(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	// The shell ignores SIGINT; its child sleeps: only a group SIGKILL ends both quickly.
	if err := m.Start("s", Spec{Steps: []runconfig.Command{sh("trap '' INT; sleep 30 & wait")}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("s", Spec{Steps: []runconfig.Command{sh("true")}}); err == nil {
		t.Error("duplicate id should fail")
	}
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	m.Stop("s")
	res := rec.wait(t)
	if !res.Stopped {
		t.Errorf("result %+v, want stopped", res)
	}
	if took := time.Since(start); took > interruptGrace+killGrace {
		t.Errorf("stop took %v", took)
	}
	if !strings.Contains(rec.output(), "Process stopped") {
		t.Errorf("output %q", rec.output())
	}
}

func TestPTYModeAndInput(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("p", Spec{PTY: true, Cols: 90, Rows: 20, Steps: []runconfig.Command{sh("read x; echo got-$x; stty size")}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := m.Write("p", []byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	if res := rec.wait(t); res.Code != 0 {
		t.Fatalf("result %+v", res)
	}
	if out := rec.output(); !strings.Contains(out, "got-hi") || !strings.Contains(out, "20 90") {
		t.Errorf("output %q", out)
	}
}

func TestMissingProgram(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("x", Spec{Steps: []runconfig.Command{{Label: "nope", Program: "/definitely/not/here"}}}); err != nil {
		t.Fatal(err)
	}
	res := rec.wait(t)
	if res.Error == "" || res.Code != -1 {
		t.Errorf("result %+v", res)
	}
	if err := m.Start("y", Spec{}); err == nil {
		t.Error("empty spec should fail")
	}
}

func TestToCRLF(t *testing.T) {
	if got := string(toCRLF([]byte("a\nb\r\n\n"))); got != "a\r\nb\r\n\r\n" {
		t.Errorf("toCRLF = %q", got)
	}
}
