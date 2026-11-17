// Package hostmode is the background session host as a process: the same
// binary as Flint, started with --agent-host, with no window and no Wails. It
// owns the terminals and agent chats (package agenthost serves them) and
// outlives the windows that started them
// (docs/design/agent-runtime.md, "Phase 3").
//
// The host takes a lock so there is one per configuration directory, listens
// on a Unix socket that only the same user may connect to (file mode, peer uid
// and a token in a 0600 file), and exits by itself when it has nothing left to
// serve. Dial and Spawn are what a window uses to find or start it.
//
// The package must not import Wails: a test checks the import graph.
package hostmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"flint/internal/agenthost"
	as "flint/internal/agentsession"
	"flint/internal/config"
	"flint/internal/sessions"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// ExitAlreadyRunning is the exit status of a host that lost the lock: another
// host serves this configuration. It is EX_TEMPFAIL of sysexits.h, and no
// other way of ending uses it.
const ExitAlreadyRunning = 75

const (
	// agentHostFlag selects host mode; it must be the first argument.
	agentHostFlag = "--agent-host"
	// readyFDFlag names the descriptor on which to say "ready" (a pipe the
	// starter passed in ExtraFiles).
	readyFDFlag = "--ready-fd="
	// DefaultIdle is how long the host stays with no sessions and no clients.
	DefaultIdle = 60 * time.Second
	// maxConns bounds simultaneous connections (windows are a handful).
	maxConns = 64
	// closeGrace bounds how long exit waits for sessions to stop.
	closeGrace = 10 * time.Second
)

// Options configure the host process.
type Options struct {
	// Version is the build version reported in hello.
	Version string
	// Drivers registers the agent drivers on the host's agent manager. It
	// is a parameter because the drivers are chosen in package main, which
	// this package must not import. Nil registers none.
	Drivers func(m *as.Manager)
	// AllowShell lets clients choose the shell over the wire. Only tests
	// set it; the Flint binary does not, so its host offers no more than
	// the web view's terminal calls do.
	AllowShell bool
}

// Main runs the host when the process was started with --agent-host as its
// first argument and then never returns: it exits with status 0, with
// ExitAlreadyRunning when another host holds the lock, or with 1. In any
// other process it returns immediately and startup goes on. Call it right
// after sandbox.Main, before anything that touches Wails.
func Main(o Options) {
	isHost, readyFD, err := parseArgs(os.Args)
	if !isHost {
		return
	}
	var ready io.WriteCloser
	if err == nil && readyFD > 0 {
		ready = os.NewFile(uintptr(readyFD), "ready")
	}
	if err == nil {
		err = runProcess(o, ready)
	}
	switch {
	case err == nil:
		os.Exit(0)
	case errors.Is(err, ErrAlreadyRunning):
		fmt.Fprintln(os.Stderr, "flint host:", err)
		os.Exit(ExitAlreadyRunning)
	default:
		fmt.Fprintln(os.Stderr, "flint host:", err)
		os.Exit(1)
	}
}

// parseArgs recognises host mode and its readiness descriptor. Only the first
// argument can select host mode, so a file named "--agent-host" given to the
// editor later on the command line cannot.
func parseArgs(args []string) (isHost bool, readyFD int, err error) {
	if len(args) < 2 || args[1] != agentHostFlag {
		return false, 0, nil
	}
	for _, a := range args[2:] {
		v, ok := strings.CutPrefix(a, readyFDFlag)
		if !ok {
			return true, 0, fmt.Errorf("unknown argument %q", a)
		}
		fd, convErr := strconv.Atoi(v)
		if convErr != nil || fd < 3 || fd > 1<<16 { // 0-2 are stdio
			return true, 0, fmt.Errorf("bad %s%q", readyFDFlag, v)
		}
		readyFD = fd
	}
	return true, readyFD, nil
}

func runProcess(o Options, ready io.WriteCloser) error {
	// The host survives its starter's terminal going away.
	signal.Ignore(syscall.SIGHUP)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	dir, err := config.Dir()
	if err != nil {
		closeQuietly(ready)
		return err
	}
	return run(ctx, runConfig{ConfigDir: dir, Version: o.Version, Drivers: o.Drivers, AllowShell: o.AllowShell, Idle: DefaultIdle, Ready: ready})
}

func closeQuietly(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// runConfig is run's input: Main fills it from the process, tests from their
// own.
type runConfig struct {
	ConfigDir string
	Version   string
	Drivers   func(*as.Manager)
	// Idle is how long the host waits with no sessions and no clients before
	// it exits (DefaultIdle when zero).
	Idle time.Duration
	// HelloTimeout is how long a connection has to say hello
	// (agenthost.DefaultHelloTimeout when zero).
	HelloTimeout time.Duration
	// AllowShell is Options.AllowShell.
	AllowShell bool
	// Ready, when set, receives "ready\n" once the host listens and is then
	// closed; it is closed without a message when the host fails first, so
	// the starter wakes up either way.
	Ready io.WriteCloser
}

// run is the host. It returns when ctx ends (a signal), when the host has
// been idle for cfg.Idle, or after host.shutdown; on every path it removes the
// socket and the token before it releases the lock.
func run(ctx context.Context, cfg runConfig) (err error) {
	closeReady := sync.OnceFunc(func() { closeQuietly(cfg.Ready) })
	defer closeReady() // a failure before "ready" still wakes the starter
	if !peerCredSupported {
		return errors.New("this platform cannot identify the peers of a Unix socket, so the host will not start")
	}
	if cfg.Idle <= 0 {
		cfg.Idle = DefaultIdle
	}
	paths := pathsIn(RunDir(cfg.ConfigDir))
	if err := prepareDir(paths.Dir); err != nil {
		return err
	}
	lock, err := acquireLock(paths.Lock)
	if err != nil {
		return err
	}
	defer lock.release() // runs last: the files go first

	// The token exists before anything listens, so a client that can connect
	// always finds the token that matches.
	token, err := writeToken(paths.Token)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(paths.Token) }()

	ln, err := listen(paths.Sock)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(paths.Sock) }() // Close unlinks it too; this covers a failed Close

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		terms        *terminal.Manager
		agents       *as.Manager
		watch        *idleWatch
		srv          *agenthost.Server
		sessionsDone atomic.Bool // host.shutdown{stopSessions} already stopped them
	)
	srv, err = agenthost.New(agenthost.Config{
		Token: token, Version: cfg.Version, HelloTimeout: cfg.HelloTimeout, AllowShell: cfg.AllowShell,
		ReplayKiB: hostReplayKiB(cfg.ConfigDir),
		// srv is assigned before the accept loop starts, the first moment a
		// client can ask.
		OnSettingsChanged: func() { srv.SetReplayKiB(hostReplayKiB(cfg.ConfigDir)) },
		Terminals: func(ev terminal.Events) sessions.Terminals {
			terms = terminal.NewManager(ev)
			return terms
		},
		Agents: func(emit func(as.Event)) sessions.Agents {
			agents = as.NewManager(emit)
			if cfg.Drivers != nil {
				cfg.Drivers(agents)
			}
			return agents
		},
		// watch is assigned before the accept loop starts, which is the first
		// moment this can be called.
		OnChange: func() { watch.Poke() },
		OnShutdown: func(stopSessions bool) {
			if stopSessions {
				sessionsDone.Store(true)
			}
			cancel()
		},
	})
	if err != nil {
		_ = ln.Close()
		return err
	}
	watch = newIdleWatch(cfg.Idle, func() bool { return srv.Load().Idle() }, cancel)
	defer watch.Stop()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		acceptLoop(ln, uint32(os.Getuid()), maxConns, func(c net.Conn) { _ = srv.Serve(c) })
	}()

	slog.Info("agent host listening", "dir", paths.Dir, "pid", os.Getpid())
	if cfg.Ready != nil {
		_, werr := io.WriteString(cfg.Ready, "ready\n")
		closeReady()
		if werr != nil {
			slog.Warn("agent host: could not say ready", "error", werr)
		}
	}

	<-ctx.Done()
	slog.Info("agent host stopping")
	watch.Stop()
	_ = ln.Close()
	<-loopDone
	srv.Close()
	if !sessionsDone.Load() {
		closeSessions(terms, agents)
	}
	return nil
}

// hostReplayKiB reads sessions.replayKiB from settings.json, the one setting
// the host's terminals take from it today: the shell is the login shell and
// its environment comes with each term.start, as in the window. A file that
// cannot be read gives the default, as it does in the window.
func hostReplayKiB(configDir string) int {
	s, err := settings.Load(settings.Path(configDir))
	if err != nil {
		slog.Warn("agent host: settings", "error", err)
	}
	return s.Sessions.ReplayKiB
}

// closeSessions hangs up the terminals and stops the chats, for at most
// closeGrace: an agent that will not stop must not keep the host (and its
// lock) alive.
func closeSessions(terms *terminal.Manager, agents *as.Manager) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if terms != nil {
			terms.CloseAll()
		}
		if agents != nil {
			_ = agents.Close()
		}
	}()
	select {
	case <-done:
	case <-time.After(closeGrace):
		slog.Warn("agent host: sessions did not stop in time", "grace", closeGrace)
	}
}
