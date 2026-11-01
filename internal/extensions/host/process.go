package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"flint/internal/extensions/manifest"
	"flint/internal/extensions/rpc"
	"flint/internal/proctree"
	"flint/internal/shellenv"
)

// Limits on a process's output.
const (
	// maxOutputRate over rateWindow seconds stops a process that floods stdout.
	maxOutputRate = 8 << 20
	rateWindow    = 5
	// maxLogLine truncates a stderr line.
	maxLogLine = 2000
)

// proc is a running extension process and its connection.
type proc struct {
	cmd      *exec.Cmd
	conn     *rpc.Conn
	stdin    *os.File
	exited   chan struct{}
	stopping atomic.Bool
}

func (p *proc) hasExited() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

// start runs the program in its own process group with a minimal
// environment, then completes the initialize handshake.
func (e *ext) start() (*proc, error) {
	path, args, err := e.m.Program(e.dir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = e.dir
	cmd.Env = append(e.h.o.Environ(), "FLINT_EXTENSION_ID="+e.id, "FLINT_EXTENSION_DIR="+e.dir, "FLINT_VERSION="+e.h.o.App.Version())
	proctree.Isolate(cmd)
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW)
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW, outR, outW)
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	err = cmd.Start()
	closeAll(inR, outW, errW) // the child has its copies
	if err != nil {
		closeAll(inW, outR, errR)
		return nil, err
	}
	p := &proc{cmd: cmd, stdin: inW, exited: make(chan struct{})}
	p.conn = rpc.New(inW, rpc.Options{
		OnRequest: e.handle,
		OnNotify:  e.notified,
		MaxRate:   maxOutputRate, RateWindow: rateWindow,
	})
	e.log.add("flint", fmt.Sprintf("started %s (pid %d)", path, cmd.Process.Pid))
	go e.copyStderr(errR)
	go func() {
		err := p.conn.Serve(outR)
		_ = outR.Close()
		if !errors.Is(err, io.EOF) && !p.stopping.Load() {
			e.log.add("flint", "protocol error: "+err.Error())
			_ = proctree.Kill(cmd.Process)
		}
	}()
	go func() {
		werr := cmd.Wait()
		_ = proctree.Kill(cmd.Process) // what it left behind in its group
		reason := "exit status 0"
		if werr != nil {
			reason = werr.Error()
		}
		if cerr := p.conn.Err(); cerr != nil && !errors.Is(cerr, io.EOF) && !errors.Is(cerr, rpc.ErrClosed) {
			reason = cerr.Error()
		}
		p.conn.Close(fmt.Errorf("%s stopped (%s)", e.name(), reason))
		_ = inW.Close()
		close(p.exited)
		e.exited(p, reason)
	}()
	if err := e.initialize(p); err != nil {
		p.stop(e.h.o.StopTimeout)
		return nil, err
	}
	return p, nil
}

func (e *ext) initialize(p *proc) error {
	ctx, cancel := context.WithTimeout(context.Background(), e.h.o.InitTimeout)
	defer cancel()
	var res struct {
		Contract int `json:"contract"`
	}
	if err := p.conn.Call(ctx, "initialize", e.initializeParams(), &res); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("no answer to initialize within %s", e.h.o.InitTimeout)
		}
		return fmt.Errorf("initialize: %w", err)
	}
	if res.Contract != 0 && res.Contract != manifest.Contract {
		return fmt.Errorf("it speaks extension contract %d; this Flint supports %d", res.Contract, manifest.Contract)
	}
	return p.conn.Notify("initialized", struct{}{})
}

// stop asks the process to shut down, closes its input and kills its
// process group if it is still there after timeout.
func (p *proc) stop(timeout time.Duration) {
	if !p.stopping.CompareAndSwap(false, true) {
		<-p.exited
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = p.conn.Call(ctx, "shutdown", struct{}{}, nil)
	_ = p.stdin.Close()
	select {
	case <-p.exited:
		return
	case <-ctx.Done():
	}
	_ = proctree.Kill(p.cmd.Process)
	<-p.exited
}

// copyStderr keeps the process's stderr in its log, line by line, cut at
// maxLogLine.
func (e *ext) copyStderr(r *os.File) {
	defer r.Close()
	br := bufio.NewReaderSize(r, maxLogLine)
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			if errors.Is(err, bufio.ErrBufferFull) {
				e.log.add("stderr", string(line)+" …")
				for errors.Is(err, bufio.ErrBufferFull) { // drop the rest of the line
					_, err = br.ReadSlice('\n')
				}
			} else {
				e.log.add("stderr", string(line))
			}
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return
		}
	}
}

func closeAll(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// envKeep are the variables an extension inherits; everything else (tokens,
// agent keys, Flint's own variables) stays out.
var envKeep = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ", "LANG", "LC_ALL", "LC_CTYPE",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
}

// MinimalEnv is the environment extensions get: envKeep from Flint's own.
func MinimalEnv() []string {
	var out []string
	for _, k := range envKeep {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// loginEnv is MinimalEnv once the login-shell PATH is imported.
func loginEnv() []string {
	shellenv.Wait()
	return MinimalEnv()
}
