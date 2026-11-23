// Package clisock is the command line's transport: the socket a running
// Flint listens on in its run directory, and the client that the flint
// binary becomes when its first argument is a command (Main). The commands
// themselves are package cli's.
//
// The socket keeps to the background host's rules (hostmode): a 0700 run
// directory of ours, a 0600 socket bound under a tight umask, a peer of
// another uid closed before a byte is read, a token in a 0600 file the
// client must present, one Flint per configuration directory (a lock), and
// bounded connections and messages. On top of that, every command that acts
// asks Classify whether the process that connected runs for an agent.
package clisock

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"flint/internal/agenthost/hostmode"
	"flint/internal/cli"
	"flint/internal/jsonrpc"
)

// Names of the command line's files in the run directory.
const (
	SockName  = "cli.sock"
	TokenName = "cli.token"
	LockName  = "cli.lock"
)

const (
	// maxConns bounds the connections served at once.
	maxConns = 16
	// requestWait is how long a connection has to send its request.
	requestWait = 10 * time.Second
)

// Paths are the command line's files.
type Paths struct {
	Dir, Sock, Token, Lock string
}

// PathsIn are the files in the run directory dir.
func PathsIn(dir string) Paths {
	return Paths{Dir: dir, Sock: filepath.Join(dir, SockName), Token: filepath.Join(dir, TokenName), Lock: filepath.Join(dir, LockName)}
}

// PathsFor are the files of the Flint that uses configDir: its run directory
// is the background host's (hostmode.RunDir), so FLINT_CONFIG_DIR instances
// stay apart.
func PathsFor(configDir string) Paths { return PathsIn(hostmode.RunDir(configDir)) }

// Options configure a Server.
type Options struct {
	Paths Paths
	// Registry are the commands.
	Registry *cli.Registry
	// Classify decides who the process pid is; asked only before a command
	// that acts.
	Classify func(pid int) cli.Caller
	// Confirm asks the user in Flint's native dialog (a human decision,
	// cli.Command.Human); nil answers no.
	Confirm func(ctx context.Context, q cli.Question) bool
}

// Server serves the command line until Close.
type Server struct {
	o      Options
	ln     *net.UnixListener
	token  string
	unlock func()
	ctx    context.Context
	cancel context.CancelFunc

	open  atomic.Int32
	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

// Start prepares the run directory, takes the lock, writes a fresh token and
// listens. It refuses a system that cannot tell a socket's peer.
func Start(o Options) (*Server, error) {
	if !hostmode.PeerCredSupported {
		return nil, errors.New("this system cannot identify a socket's peer: no command line")
	}
	if o.Registry == nil || o.Classify == nil {
		return nil, errors.New("clisock: Registry and Classify are required")
	}
	p := o.Paths
	if err := hostmode.PrepareRunDir(p.Dir); err != nil {
		return nil, err
	}
	unlock, err := hostmode.AcquireLock(p.Lock)
	if err != nil {
		return nil, err
	}
	token, err := hostmode.WriteToken(p.Token)
	if err != nil {
		unlock()
		return nil, err
	}
	ln, err := hostmode.Listen(p.Sock)
	if err != nil {
		_ = os.Remove(p.Token)
		unlock()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{o: o, ln: ln, token: token, unlock: unlock, ctx: ctx, cancel: cancel, conns: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Close stops listening, ends the open connections (a waiting command's
// context ends) and removes the socket and the token before releasing the
// lock. Commands still running finish on their own; their answers are lost.
func (s *Server) Close() {
	s.cancel()
	_ = s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	_ = os.Remove(s.o.Paths.Sock)
	_ = os.Remove(s.o.Paths.Token)
	s.unlock()
}

func (s *Server) accept() {
	defer s.wg.Done()
	backoff := 5 * time.Millisecond
	for {
		c, err := s.ln.AcceptUnix()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || s.ctx.Err() != nil {
				return
			}
			slog.Warn("cli: accept", "error", err)
			select {
			case <-time.After(backoff):
			case <-s.ctx.Done():
				return
			}
			backoff = min(backoff*2, time.Second)
			continue
		}
		backoff = 5 * time.Millisecond
		if uid, err := hostmode.PeerUID(c); err != nil || uid != uint32(os.Getuid()) {
			slog.Warn("cli: refused a connection", "peerUid", uid, "error", err)
			_ = c.Close()
			continue
		}
		if int(s.open.Add(1)) > maxConns {
			s.open.Add(-1)
			_ = c.Close()
			continue
		}
		if !s.track(c) {
			s.open.Add(-1)
			_ = c.Close()
			return
		}
		go func() {
			defer s.open.Add(-1)
			defer s.untrack(c)
			s.serve(c)
		}()
	}
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	_ = c.Close()
}

// serve answers one request on c, then closes it. The connection stays
// read while the command runs, so a client that goes away (Ctrl-C) ends
// the command's context: a wait stops.
func (s *Server) serve(c *net.UnixConn) {
	pid, perr := peerPID(c)
	_ = c.SetReadDeadline(time.Now().Add(requestWait))
	var served atomic.Bool
	var rpc *jsonrpc.Conn
	rpc = jsonrpc.New(c, jsonrpc.Options{
		MaxMessage:    cli.MaxMessage,
		MaxConcurrent: 1,
		OnRequest: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			if method != cli.Method || served.Swap(true) {
				return nil, jsonrpc.Errorf(jsonrpc.CodeMethodNotFound, "one %s per connection", cli.Method)
			}
			_ = c.SetReadDeadline(time.Time{})
			req, err := s.decode(params)
			if err != nil {
				return nil, err
			}
			ctx, stop := context.WithCancel(ctx)
			defer stop()
			defer context.AfterFunc(s.ctx, stop)()
			who := func() cli.Caller {
				if perr != nil {
					return cli.Caller{Unknown: true, What: perr.Error()}
				}
				return s.o.Classify(pid)
			}
			return cli.DispatchWith(ctx, s.o.Registry, req, cli.Env{Who: who, Confirm: s.o.Confirm}), nil
		},
		AfterReply: func(string) {
			rpc.Close(nil)
			_ = c.Close()
		},
	})
	_ = rpc.Serve(c)
}

// decode checks a request: the token first, then the bounds.
func (s *Server) decode(params json.RawMessage) (cli.Request, error) {
	var req cli.Request
	dec := json.NewDecoder(strings.NewReader(string(params)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return cli.Request{}, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "invalid request: %v", err)
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) != 1 {
		return cli.Request{}, jsonrpc.Errorf(jsonrpc.CodePermission, "the token does not match: is this Flint's own flint command?")
	}
	switch {
	case len(req.Args) > cli.MaxArgs:
		return cli.Request{}, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "more than %d words", cli.MaxArgs)
	case len(req.Files) > cli.MaxFiles:
		return cli.Request{}, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "more than %d files", cli.MaxFiles)
	case req.Cwd != "" && (len(req.Cwd) > cli.MaxPath || !filepath.IsAbs(req.Cwd)):
		return cli.Request{}, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "the working directory must be an absolute path")
	}
	for _, a := range req.Args {
		if len(a) > cli.MaxText {
			return cli.Request{}, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "a word is longer than %d bytes", cli.MaxText)
		}
	}
	return req, nil
}

// String names the server for logs.
func (s *Server) String() string { return fmt.Sprintf("cli socket %s", s.o.Paths.Sock) }
