package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"flint/internal/agenthost"
	"flint/internal/settings"
)

// The Check of a machine runs "<command> --version" over ssh with the same
// options as a connection (BatchMode: no prompt, so a missing key fails
// instead of waiting). It is bounded in time and in output.
const (
	// CheckTimeout bounds the whole check: ssh's own connect timeout is 10 s.
	CheckTimeout = 20 * time.Second
	// MaxCheckOutput bounds what is read from the remote's standard output;
	// the version line is under 200 bytes, anything bigger is not a Flint.
	MaxCheckOutput = 4 << 10
)

// CheckStatus is the verdict of a Check.
type CheckStatus string

const (
	// CheckOK: reachable, and the remote Flint speaks this Flint's host protocol.
	CheckOK CheckStatus = "ok"
	// CheckIncompatible: a Flint answered, with another host protocol (or in
	// a form this Flint does not know).
	CheckIncompatible CheckStatus = "incompatible"
	// CheckNotFound: ssh worked but the command is not on the machine.
	CheckNotFound CheckStatus = "notFound"
	// CheckError: ssh failed (key refused, host unknown, timeout, …); Message
	// holds ssh's redacted error.
	CheckError CheckStatus = "error"
)

// CheckResult is what the Settings page shows for a machine.
type CheckResult struct {
	Status CheckStatus `json:"status"`
	// Version and Protocol are the remote Flint's, when it answered.
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
	// Message explains the status in a sentence; for CheckError it is ssh's
	// own error, redacted and bounded.
	Message string `json:"message"`
}

// Usable reports whether terminals can be opened on the machine.
func (r CheckResult) Usable() bool { return r.Status == CheckOK }

// NeedsInstall reports whether the install instructions apply: Flint is
// missing or speaks another protocol.
func (r CheckResult) NeedsInstall() bool {
	return r.Status == CheckNotFound || r.Status == CheckIncompatible
}

// runner runs an ssh argument vector, writes its standard output to stdout
// (which may fail to stop it) and its standard error to stderr, and returns
// ssh's exit status (-1 when it did not exit by itself).
type runner func(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, error)

// Check asks machine m for its Flint version. It never returns an error: a
// failure is a CheckError result. m is validated again by CheckArgv.
func Check(ctx context.Context, m settings.Machine) CheckResult {
	return check(ctx, m, runSSH)
}

func check(ctx context.Context, m settings.Machine, run runner) CheckResult {
	argv, err := CheckArgv(m)
	if err != nil {
		return CheckResult{Status: CheckError, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	out := &limitedBuffer{limit: MaxCheckOutput, onOverflow: cancel}
	errTail := &tail{}
	code, err := run(ctx, argv, out, errTail)
	return interpret(code, err, out, errTail.String(), ctx.Err())
}

// interpret turns what the command did into a verdict.
func interpret(code int, runErr error, out *limitedBuffer, stderr string, ctxErr error) CheckResult {
	switch {
	case out.overflowed():
		return CheckResult{Status: CheckError, Message: fmt.Sprintf("the machine answered with more than %d bytes: that is not Flint", MaxCheckOutput)}
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return CheckResult{Status: CheckError, Message: fmt.Sprintf("no answer within %v%s", CheckTimeout, withStderr(stderr))}
	case ctxErr != nil:
		return CheckResult{Status: CheckError, Message: "the check was cancelled"}
	case runErr != nil:
		return CheckResult{Status: CheckError, Message: strings.TrimSpace(runErr.Error() + withStderr(stderr))}
	case code == 127 || code == 126 || (code != 0 && code != 255 && notFoundText(stderr)):
		return CheckResult{Status: CheckNotFound, Message: "Flint was not found on the machine (is the command right, and on the PATH of a non-interactive shell?)"}
	case code != 0:
		msg := fmt.Sprintf("ssh exited with status %d", code)
		if stderr != "" {
			msg += ": " + stderr
		}
		return CheckResult{Status: CheckError, Message: msg}
	}
	line := strings.TrimSpace(out.String())
	version, protocol, ok := agenthost.ParseVersionLine(line)
	switch {
	case !ok:
		return CheckResult{Status: CheckIncompatible, Message: "the command did not answer like Flint's host (an older Flint, or another program): install the headless host"}
	case protocol != agenthost.Protocol:
		return CheckResult{Status: CheckIncompatible, Version: version, Protocol: protocol,
			Message: fmt.Sprintf("Flint %s on the machine speaks host protocol %d; this Flint speaks %d", version, protocol, agenthost.Protocol)}
	}
	return CheckResult{Status: CheckOK, Version: version, Protocol: protocol, Message: fmt.Sprintf("Flint %s is ready (host protocol %d)", version, protocol)}
}

func withStderr(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// notFoundText recognises a shell's "command not found" for the shells that
// report it with another status.
func notFoundText(stderr string) bool {
	l := strings.ToLower(stderr)
	return strings.Contains(l, "command not found") || strings.Contains(l, "no such file or directory")
}

// limitedBuffer keeps at most limit bytes and calls onOverflow, once, at the
// first byte beyond them (which stops the command).
type limitedBuffer struct {
	limit      int
	onOverflow func()

	mu   sync.Mutex
	buf  bytes.Buffer
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.buf.Write(p[:max(room, 0)])
		if !b.over {
			b.over = true
			if b.onOverflow != nil {
				b.onOverflow()
			}
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) overflowed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.over
}

// runSSH runs the real ssh found on PATH, directly (no shell), in a process
// group that is killed when ctx ends.
func runSSH(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, error) {
	cmd, err := newCommand(argv)
	if err != nil {
		return -1, err
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("start ssh: %w", err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killGroup(cmd)
		case <-done:
		}
	}()
	err = cmd.Wait()
	close(done)
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee):
		return ee.ExitCode(), nil
	}
	return -1, err
}
