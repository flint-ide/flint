package remote

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/settings"
)

// fakeRun is a runner that writes fixed output and returns a status.
func fakeRun(stdout, stderr string, code int, err error) runner {
	return func(_ context.Context, _ []string, out, errw io.Writer) (int, error) {
		_, _ = io.WriteString(out, stdout)
		_, _ = io.WriteString(errw, stderr)
		return code, err
	}
}

func TestCheckVerdicts(t *testing.T) {
	good := agenthost.VersionLine("v1.2.3") + "\n"
	tests := []struct {
		name   string
		run    runner
		want   CheckStatus
		inText string // part of Message
	}{
		{"reachable and compatible", fakeRun(good, "", 0, nil), CheckOK, "v1.2.3"},
		{"other protocol", fakeRun("flint v9 host-protocol 99\n", "", 0, nil), CheckIncompatible, "protocol 99"},
		{"not a flint", fakeRun("GNU bash 5\n", "", 0, nil), CheckIncompatible, "did not answer like Flint"},
		{"empty answer", fakeRun("", "", 0, nil), CheckIncompatible, ""},
		{"shell: command not found", fakeRun("", "sh: 1: flint: not found", 127, nil), CheckNotFound, "not found"},
		{"bash: command not found", fakeRun("", "bash: flint: command not found", 1, nil), CheckNotFound, ""},
		{"permission denied", fakeRun("", "me@box: Permission denied (publickey).", 255, nil), CheckError, "Permission denied (publickey)"},
		{"unknown host", fakeRun("", "ssh: Could not resolve hostname nope", 255, nil), CheckError, "Could not resolve"},
		{"ssh missing", fakeRun("", "", -1, ErrNoSSH), CheckError, "ssh was not found"},
		{"other status", fakeRun("", "boom", 3, nil), CheckError, "status 3"},
		{"ssh error that mentions a file", fakeRun("", "Warning: No such file or directory", 255, nil), CheckError, "No such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := check(context.Background(), testMachine(), tt.run)
			if r.Status != tt.want {
				t.Fatalf("status = %q (%s), want %q", r.Status, r.Message, tt.want)
			}
			if !strings.Contains(r.Message, tt.inText) {
				t.Errorf("message %q lacks %q", r.Message, tt.inText)
			}
		})
	}
}

func TestCheckResultFields(t *testing.T) {
	r := check(context.Background(), testMachine(), fakeRun(agenthost.VersionLine("v1.2.3")+"\n", "", 0, nil))
	if !r.Usable() || r.NeedsInstall() || r.Version != "v1.2.3" || r.Protocol != agenthost.Protocol {
		t.Errorf("%+v", r)
	}
	r = check(context.Background(), testMachine(), fakeRun("", "", 127, nil))
	if r.Usable() || !r.NeedsInstall() {
		t.Errorf("%+v", r)
	}
	r = check(context.Background(), testMachine(), fakeRun("", "x", 255, nil))
	if r.Usable() || r.NeedsInstall() {
		t.Errorf("%+v", r)
	}
}

func TestCheckRedactsSSHError(t *testing.T) {
	secret := "token=ghp_0123456789abcdefghijABCDEFGHIJ0123 AKIAABCDEFGHIJKLMNOP"
	r := check(context.Background(), testMachine(), fakeRun("", secret+"\nPermission denied (publickey).", 255, nil))
	if strings.Contains(r.Message, "ghp_0123") || strings.Contains(r.Message, "AKIAABCDEFGHIJKLMNOP") {
		t.Errorf("secret leaked: %q", r.Message)
	}
	if !strings.Contains(r.Message, "Permission denied") {
		t.Errorf("message %q", r.Message)
	}
}

func TestCheckStopsAnOversizedAnswer(t *testing.T) {
	stopped := make(chan struct{})
	run := func(ctx context.Context, _ []string, out, _ io.Writer) (int, error) {
		_, _ = out.Write([]byte(strings.Repeat("x", MaxCheckOutput+1)))
		select {
		case <-ctx.Done(): // the overflow must cancel the command
			close(stopped)
		case <-time.After(5 * time.Second):
		}
		return -1, nil
	}
	r := check(context.Background(), testMachine(), run)
	if r.Status != CheckError || !strings.Contains(r.Message, "more than") {
		t.Fatalf("%+v", r)
	}
	select {
	case <-stopped:
	default:
		t.Error("the command was not stopped")
	}
}

func TestCheckKeepsOnlyTheCapOfOutput(t *testing.T) {
	b := &limitedBuffer{limit: 8}
	_, _ = b.Write([]byte("0123456789abcdef"))
	if b.String() != "01234567" || !b.overflowed() {
		t.Errorf("kept %q over=%v", b.String(), b.overflowed())
	}
}

func TestCheckTimesOut(t *testing.T) {
	run := func(ctx context.Context, _ []string, _, _ io.Writer) (int, error) {
		<-ctx.Done()
		return -1, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r := check(ctx, testMachine(), run)
	if r.Status != CheckError || !strings.Contains(r.Message, "no answer") {
		t.Fatalf("%+v", r)
	}
}

func TestCheckRefusesAnInvalidMachine(t *testing.T) {
	called := false
	run := func(context.Context, []string, io.Writer, io.Writer) (int, error) { called = true; return 0, nil }
	m := testMachine()
	m.Target = "-oProxyCommand=evil"
	r := check(context.Background(), m, run)
	if r.Status != CheckError || called {
		t.Fatalf("ran an unchecked machine: %+v called=%v", r, called)
	}
}

func TestCheckRunsTheCheckArgv(t *testing.T) {
	var got []string
	run := func(_ context.Context, argv []string, _, _ io.Writer) (int, error) { got = argv; return 127, nil }
	check(context.Background(), testMachine(), run)
	want, _ := CheckArgv(testMachine())
	if !slices.Equal(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
}

// The whole path through the real runner with a fake ssh on PATH.
func TestCheckThroughTheFakeSSH(t *testing.T) {
	script := func(body string) {
		t.Setenv("PATH", fakeBin(t, "#!/bin/sh\n"+body+"\n")+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	m := settings.Machine{ID: "box", Target: "me@box.example", Port: 2222, Command: "flint"}

	script(`echo "` + agenthost.VersionLine("v0.9.0") + `"`)
	if r := Check(context.Background(), m); r.Status != CheckOK || r.Version != "v0.9.0" {
		t.Errorf("ok: %+v", r)
	}
	script(`echo "sh: 1: flint: not found" >&2; exit 127`)
	if r := Check(context.Background(), m); r.Status != CheckNotFound {
		t.Errorf("not found: %+v", r)
	}
	script(`echo "Permission denied (publickey)." >&2; exit 255`)
	if r := Check(context.Background(), m); r.Status != CheckError || !strings.Contains(r.Message, "Permission denied") {
		t.Errorf("denied: %+v", r)
	}
	// An endless answer is cut off and the process group ended.
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	script(`echo $$ > ` + pidfile + `; yes xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`)
	if r := Check(context.Background(), m); r.Status != CheckError || !strings.Contains(r.Message, "more than") {
		t.Errorf("oversized: %+v", r)
	}
	data, _ := os.ReadFile(pidfile)
	pid := 0
	for _, c := range strings.TrimSpace(string(data)) {
		pid = pid*10 + int(c-'0')
	}
	deadline := time.Now().Add(5 * time.Second)
	for pid > 0 && !gone(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pid > 0 && !gone(pid) {
		t.Errorf("the fake ssh %d is still running", pid)
	}
}
