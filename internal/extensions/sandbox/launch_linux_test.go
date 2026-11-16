package sandbox

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests apply the real thing on this machine's kernel. They skip, with
// the reason, where Landlock isn't available.

func needLandlock(t *testing.T) Support {
	t.Helper()
	s := Probe()
	if !s.Available {
		t.Skipf("no sandbox on this system: %s", s.Reason)
	}
	return s
}

func TestStrictProfileOnLinux(t *testing.T) {
	s := needLandlock(t)
	pl := newPlace(t)
	addr := listener(t)
	sock := filepath.Join(pl.outside, "sock")
	ul, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ul.Close()
	ops := []op{
		{"read", filepath.Join(pl.ext, "own.txt")}, // its own folder
		{"list", pl.ext},                                //
		{"read", "/etc/passwd"},                         // the system
		{"read", pl.secret},                             // someone else's file
		{"list", pl.outside},                            //
		{"list", filepath.Dir(pl.ext)},                  // the other extensions
		{"write", filepath.Join(pl.data, "state.json")}, // its data folder
		{"read", filepath.Join(pl.data, "state.json")},  //
		{"mkdir", filepath.Join(pl.tmp, "work")},        // its temporary folder
		{"write", filepath.Join(pl.ext, "new.txt")},     // not its own folder
		{"write", filepath.Join(pl.ext, "own.txt")},     //
		{"remove", filepath.Join(pl.ext, "own.txt")},    //
		{"write", filepath.Join(pl.outside, "new")},     // nor anywhere else
		{"write", pl.secret},                            //
		{"remove", pl.secret},                           //
		{"write", "/dev/null"},                          // the sink
		{"symlink", filepath.Join(pl.data, "link")},     // a link out of its data folder…
		{"read", filepath.Join(pl.data, "link")},        // …reads only what it could read anyway
		{"run", "/bin/true"},                            // another program
		{"env", launchEnv},                              // the launcher's variable is gone
		{"limits", ""},                                  //
	}
	results := tryAll(t, pl, Build(pl.request(nil)), s, ops...)
	denied := "permission denied"
	expect(t, ops, results, "ok", "ok", "ok", denied, denied, denied, "ok", "ok", "ok", denied, denied, denied, denied, denied, denied, "ok", "ok", "ok", denied, "ok", "ok")
	if got := results[19].Out; got != " false" {
		t.Errorf("%s reaches the program: %q", launchEnv, got)
	}
	var soft, hard, coreSoft, coreHard uint64
	if _, err := fmt.Sscanf(results[20].Out, "files %d/%d core %d/%d", &soft, &hard, &coreSoft, &coreHard); err != nil {
		t.Fatalf("limits = %q: %v", results[20].Out, err)
	}
	if soft > MaxOpenFiles || hard > MaxOpenFiles || coreSoft != 0 || coreHard != 0 {
		t.Errorf("limits = %q, want at most %d open files and no core dumps", results[20].Out, MaxOpenFiles)
	}
	if data, _ := os.ReadFile(pl.secret); string(data) != "secret" {
		t.Errorf("the secret changed: %q", data)
	}

	// Sockets and signals, as far as this kernel goes.
	ops = []op{{"tcp", addr}, {"udp", addr}, {"unix", sock}, {"listen", ""}, {"signal", fmt.Sprint(os.Getpid())}}
	results = tryAll(t, pl, Build(pl.request(nil)), s, ops...)
	if s.seccomp {
		expect(t, ops[:4], results[:4], denied, denied, denied, denied)
		// The Internet without local sockets, and the other way around.
		results = tryAll(t, pl, Build(pl.request(func(r *Request) { r.Network = true })), s, ops[:4]...)
		expect(t, ops[:4], results, "ok", "ok", denied, "ok")
		results = tryAll(t, pl, Build(pl.request(func(r *Request) { r.LocalSockets = true })), s, ops[:4]...)
		expect(t, ops[:4], results, denied, denied, "ok", denied)
	} else if s.landlockABI >= 4 {
		t.Logf("no seccomp here: only TCP is blocked (%v)", s.Gaps)
		expect(t, ops[:1], results[:1], denied)
		expect(t, ops[3:4], results[3:4], denied)
	} else {
		t.Logf("this kernel can't block the network: %v", s.Gaps)
		expect(t, ops[:1], results[:1], "ok")
		if !strings.Contains(strings.Join(s.Gaps, " "), "are not blocked") {
			t.Errorf("the gap isn't reported: %v", s.Gaps)
		}
	}
	if s.landlockABI >= 6 {
		expect(t, ops[4:], results[4:], "operation not permitted")
	} else {
		expect(t, ops[4:], results[4:], "ok")
		if !strings.Contains(strings.Join(s.Gaps, " "), "signals") {
			t.Errorf("the gap isn't reported: %v", s.Gaps)
		}
	}
}

func TestDeclaredAccessOnLinux(t *testing.T) {
	s := needLandlock(t)
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
		r.Network, r.LocalSockets = true, true
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
		{"listen", ""},
		{"sh", "echo started"},
		// What it starts is confined like itself.
		{"sh", "cat " + pl.secret},
		{"sh", "echo x > " + filepath.Join(pl.outside, "new")},
	}
	results := tryAll(t, pl, p, s, ops...)
	expect(t, ops, results, "ok", "permission denied", "ok", "permission denied", "ok", "ok", "ok", "exit status", "exit status")
	if !strings.Contains(results[6].Out, "started") {
		t.Errorf("sh printed %q", results[6].Out)
	}
	if !strings.Contains(results[7].Out, "ermission denied") || strings.Contains(results[7].Out, "secret\n") {
		t.Errorf("a child read the secret: %q", results[7].Out)
	}
}

// A script runs through its interpreter without the extension being allowed
// to start other programs.
func TestScriptProgramOnLinux(t *testing.T) {
	s := needLandlock(t)
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	pl := newPlace(t)
	script := filepath.Join(pl.ext, "run.sh")
	text := "#!/bin/sh\necho from the script\nread line < " + filepath.Join(pl.ext, "own.txt") + " && echo read $line\n/bin/cat /etc/hostname 2>&1 || echo cat refused\n"
	if err := os.WriteFile(script, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Build(pl.request(func(r *Request) { r.Interpreters = Interpreters(script, os.Getenv("PATH")) }))
	if len(p.ExecFiles) == 0 {
		t.Fatal("no interpreter found for a #!/bin/sh script")
	}
	out, err := start(t, p, s, []string{script})
	if err != nil {
		t.Fatalf("the script did not run: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "from the script") || !strings.Contains(got, "cat refused") {
		t.Errorf("output:\n%s", got)
	}
	// Without its interpreter the script can't start at all.
	p.ExecFiles = nil
	if out, err := start(t, p, s, []string{script}); err == nil {
		t.Errorf("the script ran without its interpreter being allowed: %s", out)
	} else if !strings.Contains(err.Error(), "sandbox launcher") {
		t.Errorf("err = %v", err)
	}
}

// An older kernel's rules, applied here: what Support reports as a gap is
// really open, and the rest still holds.
func TestDegradedKernelOnLinux(t *testing.T) {
	needLandlock(t)
	pl := newPlace(t)
	addr := listener(t)
	old := describeLinux(Support{Available: true, landlockABI: 1})
	gaps := strings.Join(old.Gaps, "\n")
	for _, want := range []string{"Network access and local sockets are not blocked", "emptied", "signals"} {
		if !strings.Contains(gaps, want) {
			t.Errorf("gaps lack %q:\n%s", want, gaps)
		}
	}
	if old.Mechanism != "Landlock (ABI 1)" {
		t.Errorf("mechanism = %q", old.Mechanism)
	}
	ops := []op{{"read", pl.secret}, {"write", filepath.Join(pl.outside, "new")}, {"write", filepath.Join(pl.data, "x")}, {"tcp", addr}, {"signal", fmt.Sprint(os.Getpid())}}
	results := tryAll(t, pl, Build(pl.request(nil)), old, ops...)
	expect(t, ops, results, "permission denied", "permission denied", "ok", "ok", "ok")

	// Landlock 4 without seccomp: TCP is blocked, UDP is not.
	mid := describeLinux(Support{Available: true, landlockABI: 4})
	if !strings.Contains(strings.Join(mid.Gaps, "\n"), "Only TCP is blocked") {
		t.Errorf("gaps: %v", mid.Gaps)
	}
	ops = []op{{"tcp", addr}, {"udp", addr}}
	results = tryAll(t, pl, Build(pl.request(nil)), mid, ops...)
	expect(t, ops, results, "permission denied", "ok")
}

func TestLauncherRefusesWhatItCantRun(t *testing.T) {
	s := needLandlock(t)
	pl := newPlace(t)
	_, err := start(t, Build(pl.request(nil)), s, []string{filepath.Join(pl.ext, "nothing")})
	if err == nil || !strings.Contains(err.Error(), "sandbox launcher") || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("err = %v", err)
	}
	// Not started as the launcher: Main returns and the program runs normally.
	cmd := exec.Command(pl.program)
	cmd.Env = []string{helperEnv + `=[{"kind":"read","arg":"` + pl.secret + `"}]`}
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), `"out":"secret"`) {
		t.Errorf("unconfined run: %s, %v", out, err)
	}
}
