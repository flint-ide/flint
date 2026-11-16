package sandbox

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// The tests start this test binary twice over: as the sandbox launcher
// (Main, as Flint's own main does) and, confined by it, as a program that
// tries what helperOps lists and prints what happened.

const helperEnv = "FLINT_SANDBOX_TEST_OPS"

func TestMain(m *testing.M) {
	Main()
	if raw, ok := os.LookupEnv(helperEnv); ok {
		helper(raw)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// op is something the confined helper tries: Kind on Arg.
type op struct {
	Kind string `json:"kind"`
	Arg  string `json:"arg"`
}

// result is what came of it: Err is "" when it worked, Out what it produced.
type result struct {
	Err string `json:"err"`
	Out string `json:"out"`
}

func helper(raw string) {
	var ops []op
	if err := json.Unmarshal([]byte(raw), &ops); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	out := make([]result, len(ops))
	for i, o := range ops {
		text, err := try(o)
		out[i].Out = text
		if err != nil {
			out[i].Err = err.Error()
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func try(o op) (string, error) {
	switch o.Kind {
	case "read":
		data, err := os.ReadFile(o.Arg)
		return string(data), err
	case "list":
		entries, err := os.ReadDir(o.Arg)
		return fmt.Sprint(len(entries)), err
	case "write":
		return "", os.WriteFile(o.Arg, []byte("written"), 0o600)
	case "truncate":
		return "", os.Truncate(o.Arg, 0)
	case "remove":
		return "", os.Remove(o.Arg)
	case "mkdir":
		return "", os.Mkdir(o.Arg, 0o700)
	case "symlink":
		return "", os.Symlink("/etc/passwd", o.Arg)
	case "tcp":
		c, err := net.DialTimeout("tcp", o.Arg, 2*time.Second)
		if err == nil {
			c.Close()
		}
		return "", err
	case "udp":
		c, err := net.Dial("udp", o.Arg)
		if err == nil {
			_, err = c.Write([]byte("x"))
			c.Close()
		}
		return "", err
	case "unix":
		c, err := net.DialTimeout("unix", o.Arg, 2*time.Second)
		if err == nil {
			c.Close()
		}
		return "", err
	case "listen":
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			l.Close()
		}
		return "", err
	case "run":
		data, err := exec.Command(o.Arg).CombinedOutput()
		return string(data), err
	case "sh":
		data, err := exec.Command("/bin/sh", "-c", o.Arg).CombinedOutput()
		return string(data), err
	case "signal":
		var pid int
		fmt.Sscan(o.Arg, &pid)
		return "", syscall.Kill(pid, 0)
	case "env":
		v, ok := os.LookupEnv(o.Arg)
		return fmt.Sprintf("%s %v", v, ok), nil
	case "limits":
		var files, core syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &files); err != nil {
			return "", err
		}
		err := syscall.Getrlimit(syscall.RLIMIT_CORE, &core)
		return fmt.Sprintf("files %d/%d core %d/%d", files.Cur, files.Max, core.Cur, core.Max), err
	}
	return "", fmt.Errorf("unknown op %q", o.Kind)
}
