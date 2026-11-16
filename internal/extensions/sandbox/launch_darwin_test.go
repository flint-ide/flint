package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The macOS sandbox, for real: sandbox-exec with the generated profile.
// These tests skip where sandbox-exec is missing or refuses to work (inside
// another sandbox).

func needSandboxExec(t *testing.T) Support {
	t.Helper()
	s := Probe()
	if !s.Available {
		t.Skipf("no sandbox on this system: %s", s.Reason)
	}
	return s
}

// denied is how the macOS sandbox refuses.
const deniedDarwin = "operation not permitted"

func TestStrictProfileOnDarwin(t *testing.T) {
	s := needSandboxExec(t)
	pl := newPlace(t)
	addr := listener(t)
	ops := []op{
		{"read", filepath.Join(pl.ext, "own.txt")}, // its own folder
		{"list", pl.ext},                                //
		{"read", "/etc/hosts"},                          // the system
		{"read", pl.secret},                             // someone else's file
		{"list", pl.outside},                            //
		{"write", filepath.Join(pl.data, "state.json")}, // its data folder
		{"read", filepath.Join(pl.data, "state.json")},  //
		{"mkdir", filepath.Join(pl.tmp, "work")},        // its temporary folder
		{"write", filepath.Join(pl.ext, "new.txt")},     // not its own folder
		{"remove", filepath.Join(pl.ext, "own.txt")},    //
		{"write", filepath.Join(pl.outside, "new")},     // nor anywhere else
		{"write", pl.secret},                            //
		{"write", "/dev/null"},                          // the sink
		{"tcp", addr},                                   // no network
		{"udp", addr},                                   //
		{"run", "/usr/bin/true"},                        // another program
		{"signal", fmt.Sprint(os.Getpid())},             // a process outside the sandbox
		{"env", launchEnv},                              // the launcher's variable is gone
		{"limits", ""},                                  //
	}
	results := tryAll(t, pl, Build(pl.request(nil)), s, ops...)
	d := deniedDarwin
	expect(t, ops, results, "ok", "ok", "ok", d, d, "ok", "ok", "ok", d, d, d, d, "ok", d, d, d, d, "ok", "ok")
	if got := results[17].Out; got != " false" {
		t.Errorf("%s reaches the program: %q", launchEnv, got)
	}
	if !strings.HasSuffix(results[18].Out, "core 0/0") {
		t.Errorf("limits = %q", results[18].Out)
	}
	if data, _ := os.ReadFile(pl.secret); string(data) != "secret" {
		t.Errorf("the secret changed: %q", data)
	}
}

func TestDeclaredAccessOnDarwin(t *testing.T) {
	s := needSandboxExec(t)
	pl := newPlace(t)
	addr := listener(t)
	shared := filepath.Join(pl.outside, "shared")
	out := filepath.Join(pl.outside, "out")
	for _, dir := range []string{shared, out} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shared, "doc"), []byte("doc"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Build(pl.request(func(r *Request) {
		r.Read = []string{shared}
		r.Write = []string{out}
		r.Network = true
		r.Exec = true
	}))
	if len(p.Notes) != 0 {
		t.Fatalf("notes: %v", p.Notes)
	}
	ops := []op{
		{"read", filepath.Join(shared, "doc")},
		{"write", filepath.Join(shared, "doc")},
		{"write", filepath.Join(out, "result")},
		{"read", pl.secret},
		{"tcp", addr},
		{"sh", "echo started"},
		{"sh", "cat " + pl.secret}, // what it starts is confined like itself
	}
	results := tryAll(t, pl, p, s, ops...)
	expect(t, ops, results, "ok", deniedDarwin, "ok", deniedDarwin, "ok", "ok", "exit status")
	if strings.Contains(results[6].Out, "secret\n") {
		t.Errorf("a child read the secret: %q", results[6].Out)
	}
}
