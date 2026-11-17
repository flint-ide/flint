package remote

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/redact"
)

// ConnectError is a failed attempt. ssh exiting before it spoke any protocol
// (a refused key, an unknown host key, a name that does not resolve, no Flint
// on the remote) is the usual case: ExitCode is then ssh's status and Stderr
// its explanation.
type ConnectError struct {
	// ExitCode is ssh's exit status; -1 when ssh did not exit by itself.
	ExitCode int
	// Stderr is what ssh printed, redacted.
	Stderr string
	// Cause is the protocol-level error.
	Cause error
}

func (e *ConnectError) Error() string {
	var cause string
	if e.Cause != nil {
		cause = redact.Secrets(e.Cause.Error())
	}
	switch {
	case e.ExitCode > 0 && e.Stderr != "":
		return fmt.Sprintf("ssh exited with status %d: %s", e.ExitCode, e.Stderr)
	case e.ExitCode > 0:
		return fmt.Sprintf("ssh exited with status %d", e.ExitCode)
	case e.Stderr != "":
		return fmt.Sprintf("%s: %s", cause, e.Stderr)
	}
	return cause
}

func (e *ConnectError) Unwrap() error { return e.Cause }

// connectOnce runs one attempt: ssh, hello, the re-attach of what is in use,
// and then installs the link. show is the state to show while it runs; fromSupervisor
// says the supervisor is the caller, whose supervision ends with a success.
// A failure sets the state to failed and returns a *ConnectError (or
// ErrClosed when the Conn was closed meanwhile).
func (c *Conn) connectOnce(show State, fromSupervisor bool) error {
	t := &tail{}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.tail, c.state, c.lastErr = t, show, ""
	c.mu.Unlock()
	c.emit()

	// The argument vector is built again, and validated again, every time.
	argv, err := HostArgv(c.cfg.Machine)
	if err != nil {
		return c.fail(&ConnectError{ExitCode: -1, Cause: err})
	}
	p, err := startSSH(argv, t)
	if err != nil {
		return c.fail(&ConnectError{ExitCode: -1, Cause: err})
	}
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() { // Close during the attempt ends ssh at once
		select {
		case <-c.ctx.Done():
			p.kill()
		case <-stopWatch:
		}
	}()

	cl, err := client.New(p, client.Config{Version: c.cfg.Version, HelloTimeout: c.cfg.HelloTimeout, Handlers: c.handlers()})
	if err == nil {
		err = c.prepare(cl)
	}
	if err != nil {
		return c.fail(c.abandon(p, cl, err))
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = cl.Close()
		_ = p.Close()
		return ErrClosed
	}
	l := &link{cl: cl, proc: p, since: c.cfg.Clock.Now()}
	c.link, c.state, c.lastErr, c.retryIn = l, StateConnected, "", 0
	if fromSupervisor {
		c.supervising = false
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go c.watch(l)
	c.emit()
	slog.Info("remote connected", "machine", c.cfg.Machine.ID)
	return nil
}

// abandon tears down a failed attempt and explains it. ssh gets a moment to
// finish by itself first, so its exit status and last words are not lost to
// our kill.
func (c *Conn) abandon(p *sshProc, cl *client.Client, cause error) *ConnectError {
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
	code := -1
	select {
	case <-p.done:
		code = p.exitCode()
	default:
	}
	if cl != nil {
		_ = cl.Close()
		// Output the new client delivered before it broke counts as seen.
		pos := cl.Positions()
		c.mu.Lock()
		for id, seq := range pos {
			c.termPos[id] = seq
		}
		c.mu.Unlock()
	}
	p.kill()
	_ = p.Close()
	<-p.done
	var pe *client.ProtocolError
	if errors.As(cause, &pe) {
		cause = fmt.Errorf("%w: update Flint on the remote machine", cause)
	}
	return &ConnectError{ExitCode: code, Stderr: c.currentTail(), Cause: cause}
}

func (c *Conn) currentTail() string {
	c.mu.Lock()
	t := c.tail
	c.mu.Unlock()
	return t.String()
}

// fail records a failed attempt in the state and returns the error.
func (c *Conn) fail(ce *ConnectError) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.state, c.lastErr = StateFailed, ce.Error()
	c.mu.Unlock()
	c.emit()
	slog.Warn("remote connection failed", "machine", c.cfg.Machine.ID, "ssh exit", ce.ExitCode)
	return ce
}

// prepare brings a new connection up to date before it is installed: sessions
// closed while the connection was down are closed on the remote, every
// session still in use is attached from the last seq seen, and the policy
// set in the meantime is applied. Sessions the host no longer holds are
// dropped and reported to OnLost.
func (c *Conn) prepare(cl *client.Client) error {
	hello := cl.Hello()
	holds := func(ids []string, id string) bool {
		for _, h := range ids {
			if h == id {
				return true
			}
		}
		return false
	}

	c.mu.Lock()
	closeT, stopC := keys(c.pendingClose), keys(c.pendingStop)
	terms, chats := keys(c.terms), keys(c.chats)
	termPos := make(map[string]uint64, len(terms))
	for _, id := range terms {
		termPos[id] = c.termPos[id]
	}
	chatSeq := make(map[string]uint64, len(chats))
	for _, id := range chats {
		chatSeq[id] = c.chatSeq[id]
	}
	policy, policySet := c.policy, c.policySet
	c.mu.Unlock()

	for _, id := range closeT {
		if holds(hello.Terminals, id) {
			cl.Terminals().Close(id)
		}
	}
	for _, id := range stopC {
		if holds(hello.Chats, id) {
			_ = cl.Agents().Stop(id)
		}
	}
	var lostT, lostC []string
	for _, id := range terms {
		if !holds(hello.Terminals, id) {
			lostT = append(lostT, id)
			continue
		}
		if err := cl.AttachTerminal(id, termPos[id]); err != nil {
			return fmt.Errorf("attach terminal %s: %w", id, err)
		}
	}
	for _, id := range chats {
		if !holds(hello.Chats, id) {
			lostC = append(lostC, id)
			continue
		}
		if err := cl.AttachChat(id, chatSeq[id]); err != nil {
			return fmt.Errorf("attach chat %s: %w", id, err)
		}
	}
	if policySet {
		cl.Agents().SetPolicy(policy)
	}

	c.mu.Lock()
	for _, id := range closeT {
		delete(c.pendingClose, id)
	}
	for _, id := range stopC {
		delete(c.pendingStop, id)
	}
	for _, id := range lostT {
		delete(c.terms, id)
		delete(c.termPos, id)
	}
	for _, id := range lostC {
		delete(c.chats, id)
		delete(c.chatSeq, id)
	}
	c.mu.Unlock()
	if c.cfg.OnLost != nil {
		for _, id := range lostT {
			c.cfg.OnLost("terminal", id)
		}
		for _, id := range lostC {
			c.cfg.OnLost("chat", id)
		}
	}
	return nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
