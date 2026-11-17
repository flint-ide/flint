package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	"flint/internal/terminal"
)

// The platforms of the release archives (flint-host-<os>-<arch>.tar.gz).
var targets = []struct{ goos, goarch string }{
	{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"},
}

func goTool(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool")
	}
	return p
}

func goEnv(goos, goarch string) []string {
	return append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
}

// The host runs on machines without a GUI and must be a static, cgo-free
// binary: it compiles for every release platform with CGO_ENABLED=0, and its
// import graph never reaches Wails (or the C pseudo-package).
func TestCompilesWithoutCgoForEveryReleasePlatform(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles four targets")
	}
	gobin := goTool(t)
	for _, tg := range targets {
		t.Run(tg.goos+"-"+tg.goarch, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "flint")
			build := exec.Command(gobin, "build", "-o", out, ".")
			build.Env = goEnv(tg.goos, tg.goarch)
			if msg, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, msg)
			}
			list := exec.Command(gobin, "list", "-deps", "-f", "{{.ImportPath}}", ".")
			list.Env = goEnv(tg.goos, tg.goarch)
			msg, err := list.CombinedOutput()
			if err != nil {
				t.Fatalf("go list: %v\n%s", err, msg)
			}
			for _, pkg := range strings.Fields(string(msg)) {
				if strings.Contains(pkg, "github.com/wailsapp") || pkg == "C" {
					t.Errorf("flint-host depends on %s", pkg)
				}
			}
		})
	}
}

// build compiles the host for this platform with a known version.
func build(t *testing.T) string {
	t.Helper()
	gobin := goTool(t)
	out := filepath.Join(t.TempDir(), "flint")
	cmd := exec.Command(gobin, "build", "-ldflags", "-X main.version=v9.9.9", "-o", out, ".")
	cmd.Env = goEnv(runtime.GOOS, runtime.GOARCH)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}
	return out
}

// --version prints the line the machine Check parses: the version and the
// host protocol version of this Flint.
func TestVersionIsWhatTheCheckParses(t *testing.T) {
	bin := build(t)
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	version, protocol, ok := agenthost.ParseVersionLine(strings.TrimSpace(string(out)))
	if !ok || version != "v9.9.9" || protocol != agenthost.Protocol {
		t.Fatalf("--version printed %q (parsed %q %d %v)", out, version, protocol, ok)
	}
}

func TestWithoutFlagsItExplainsAndFails(t *testing.T) {
	bin := build(t)
	err := exec.Command(bin).Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Fatalf("no flags: %v, want exit status 2", err)
	}
}

type pipe struct {
	io.Reader
	io.WriteCloser
}

// The built binary is a working remote Flint: "--agent-host --stdio" starts
// its host, a terminal runs in it, and agent calls say agents are not
// available.
func TestRelayServesTerminalsAndRefusesAgents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix only")
	}
	bin := build(t)
	cfg, err := os.MkdirTemp("/tmp", "fhost")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	cmd := exec.Command(bin, "--agent-host", "--stdio")
	cmd.Env = append(os.Environ(), "FLINT_CONFIG_DIR="+cfg, "XDG_RUNTIME_DIR="+cfg)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	events := &sink{}
	cl, err := client.New(pipe{out, in}, client.Config{Version: "test", Handlers: client.Handlers{Terminal: events}})
	if err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal(err)
	}
	// Whatever happens, the host this started must not outlive the test.
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer scancel()
		_ = cl.Shutdown(sctx, true)
		_ = cl.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		stopLeftoverHost(cfg)
	})

	if _, err := cl.Agents().Start(ctx, "claude", cfg); err == nil || !strings.Contains(err.Error(), "agents are not available on this host") {
		t.Errorf("agent start = %v", err)
	}
	if err := cl.Terminals().Start("t1", terminal.Options{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	if err := cl.Terminals().Write("t1", []byte("echo HEADLESS$((40+2))\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(events.text(), "HEADLESS42") {
		if time.Now().After(deadline) {
			t.Fatalf("no output from the remote shell: %q", events.text())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
