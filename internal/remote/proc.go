package remote

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	// closeGrace is how long a closing connection lets ssh end by itself
	// (its stdin is closed, which ends the remote relay and then ssh) before
	// the process group is killed.
	closeGrace = 500 * time.Millisecond
	// waitDelay bounds the wait for ssh's stderr to drain once it exited, in
	// case something it started (a ProxyCommand) still holds the pipe.
	waitDelay = time.Second
)

// sshProc is a running ssh and the byte stream over its standard input and
// output. It is an io.ReadWriteCloser, which is what client.New wants.
// ssh runs in a process group of its own so that Close ends it together
// with anything it started (a ProxyCommand), and never leaves one behind.
type sshProc struct {
	cmd *exec.Cmd
	in  *os.File // write end: ssh's stdin
	out *os.File // read end: ssh's stdout

	done    chan struct{} // closed when ssh was waited for
	waitErr error         // valid after done

	closeOnce sync.Once
}

// startSSH starts ssh with the argument vector argv (from HostArgv or
// CheckArgv) and sends its standard error to stderr. ssh is resolved on PATH
// now and run directly: no shell.
func startSSH(argv []string, stderr io.Writer) (*sshProc, error) {
	cmd, err := newCommand(argv)
	if err != nil {
		return nil, err
	}
	// Pipes made here (not cmd.StdinPipe/StdoutPipe) so Wait never closes
	// the read end under a reader that has not finished.
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, stderr
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)
	err = cmd.Start()
	_ = inR.Close() // the child's copies are the only ones that matter now
	_ = outW.Close()
	if err != nil {
		_ = inW.Close()
		_ = outR.Close()
		return nil, fmt.Errorf("start ssh: %w", err)
	}
	p := &sshProc{cmd: cmd, in: inW, out: outR, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *sshProc) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *sshProc) Write(b []byte) (int, error) { return p.in.Write(b) }

// Close ends the connection: stdin is closed, ssh gets closeGrace to exit,
// then its process group is killed. It returns when ssh has been waited for.
func (p *sshProc) Close() error {
	p.closeOnce.Do(func() {
		_ = p.in.Close()
		select {
		case <-p.done:
		case <-time.After(closeGrace):
		}
		p.kill()
		<-p.done
		_ = p.out.Close()
	})
	return nil
}

// kill ends ssh and its process group at once. It returns without waiting.
func (p *sshProc) kill() {
	select {
	case <-p.done: // already reaped: its pid may be someone else's now
		return
	default:
	}
	killGroup(p.cmd)
}

// exitCode is ssh's exit status after done; -1 when it was killed by a
// signal or did not run.
func (p *sshProc) exitCode() int {
	if p.waitErr == nil {
		return 0
	}
	if ee, ok := p.waitErr.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
