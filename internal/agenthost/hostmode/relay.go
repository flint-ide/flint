package hostmode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flint/internal/agenthost"
	"flint/internal/config"
)

// Exit statuses of "flint --agent-host --stdio". A clean end (the peer
// closed its side, or the host did) is 0. The others are what a caller of
// ssh can tell apart: ssh itself uses 255 for its own failures.
const (
	// ExitRelayHostFailed: no host could be reached or started.
	ExitRelayHostFailed = 76
	// ExitRelayNoPeerCred: the platform cannot identify the peers of a Unix
	// socket, so no host runs here and the relay will not start one.
	ExitRelayNoPeerCred = 77
	// ExitRelayBadHello: the stream did not begin with a hello request.
	ExitRelayBadHello = 78
)

// maxHelloLine bounds the first line of the stream, as the host's own limit
// on a line before hello does: a hello is under 200 bytes.
const maxHelloLine = 4 << 10

// relayAttempts and relayRetryWait bound the retries when two relays start a
// host at the same moment and the loser meets a winner that does not listen
// yet.
const (
	relayAttempts  = 3
	relayRetryWait = 200 * time.Millisecond
)

// RelayError is returned by Relay for a failure with its own exit status.
type RelayError struct {
	Code int
	Err  error
}

func (e *RelayError) Error() string { return e.Err.Error() }
func (e *RelayError) Unwrap() error { return e.Err }

// RelayExitCode is the process exit status for what Relay returned: 0 for
// nil, the error's own code for a *RelayError and ExitRelayHostFailed for
// anything else.
func RelayExitCode(err error) int {
	var re *RelayError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &re):
		return re.Code
	}
	return ExitRelayHostFailed
}

// Relay is "flint --agent-host --stdio": it connects this machine's host for
// configDir (starting one with Spawn when none runs) and passes bytes between
// in/out and the host's socket until either side closes. The host keeps its
// sessions when the relay ends, which is the point: an ssh connection may drop
// and the next one finds the same terminals.
//
// Authentication. The client at the far end of ssh cannot read host.token,
// and the token must never cross the wire. The relay runs as the same user as
// the host and can read it, so it does the token's part itself: the first line
// from in must be a hello request, and the relay forwards it with params.token
// replaced by the local token (protocol and version are the client's, nothing
// else of its params is kept). The reply, which holds no token, goes back as
// any other. After that the relay is a plain pipe. The client's own token, if
// any, is discarded; the token is written only to the host's socket, never to
// out and never to an error or log line. Whoever can start this process
// already runs as the user and could read the token file, so the relay hands
// out nothing it does not already have: it makes the host reachable through
// ssh's authentication instead of a file.
//
// Relay writes nothing to out of its own: every byte is the host's. The
// goroutine that reads in stays blocked until in is closed, so Relay is meant
// for a process that ends right after, or a test that closes in.
func Relay(ctx context.Context, configDir string, in io.Reader, out io.Writer) error {
	if !peerCredSupported {
		return &RelayError{ExitRelayNoPeerCred, errors.New("this platform cannot identify the peers of a Unix socket, so the host will not start")}
	}
	br := bufio.NewReaderSize(in, 2*maxHelloLine)
	line, err := readHelloLine(br)
	switch {
	case err != nil:
		return &RelayError{ExitRelayBadHello, err}
	case line == nil:
		return nil // the peer left before saying anything
	}

	s, err := connectOrSpawn(ctx, configDir)
	if err != nil {
		return &RelayError{ExitRelayHostFailed, err}
	}
	defer s.conn.Close()
	hello, err := withToken(line, s.token)
	if err != nil {
		return &RelayError{ExitRelayBadHello, err}
	}
	if _, err := s.conn.Write(hello); err != nil {
		return &RelayError{ExitRelayHostFailed, fmt.Errorf("write to the host: %w", err)}
	}
	splice(ctx, s.conn, br, out)
	return nil
}

// connectOrSpawn dials the host and starts one when none runs.
func connectOrSpawn(ctx context.Context, configDir string) (hostSocket, error) {
	var lastErr error
	for i := range relayAttempts {
		if i > 0 {
			select {
			case <-time.After(relayRetryWait):
			case <-ctx.Done():
				return hostSocket{}, ctx.Err()
			}
		}
		s, err := dialSocket(ctx, configDir)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, ErrNotRunning) {
			return hostSocket{}, err
		}
		s, err = spawnSocket(ctx, configDir)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, ErrNotRunning) {
			return hostSocket{}, err
		}
		lastErr = err
	}
	return hostSocket{}, lastErr
}

// readHelloLine reads the first line, at most maxHelloLine bytes. It returns
// nil, nil when the stream ends before any byte.
func readHelloLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadSlice('\n')
	switch {
	case errors.Is(err, io.EOF) && len(line) == 0:
		return nil, nil
	case errors.Is(err, bufio.ErrBufferFull) || len(line) > maxHelloLine:
		return nil, fmt.Errorf("the first message is longer than %d bytes", maxHelloLine)
	case err != nil:
		return nil, fmt.Errorf("the stream ended in the middle of the first message")
	}
	return append([]byte(nil), line...), nil
}

// withToken turns the client's hello request into the one the host will
// accept: same id, the client's protocol and version, the local token. A
// first message that is not a hello request is refused.
func withToken(line []byte, token string) ([]byte, error) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, errors.New("the first message is not JSON")
	}
	if req.Method != agenthost.MethodHello {
		return nil, errors.New("the first message must be hello")
	}
	if len(req.ID) == 0 || (req.ID[0] != '"' && (req.ID[0] < '0' || req.ID[0] > '9') && req.ID[0] != '-') {
		return nil, errors.New("hello needs a string or number id")
	}
	var p agenthost.HelloParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return nil, errors.New("bad hello parameters")
	}
	if len(p.Version) > 64 {
		p.Version = p.Version[:64]
	}
	p.Token = token
	b, err := json.Marshal(struct {
		JSONRPC string                `json:"jsonrpc"`
		ID      json.RawMessage       `json:"id"`
		Method  string                `json:"method"`
		Params  agenthost.HelloParams `json:"params"`
	}{"2.0", req.ID, agenthost.MethodHello, p})
	if err != nil {
		return nil, errors.New("bad hello")
	}
	return append(b, '\n'), nil
}

// splice copies both ways until one side ends, then closes the socket and
// waits for the host-to-out copy, so nothing writes to out after it returns.
func splice(ctx context.Context, conn net.Conn, in io.Reader, out io.Writer) {
	hostDone := make(chan struct{})
	go func() {
		defer close(hostDone)
		_, _ = io.Copy(out, conn)
	}()
	inDone := make(chan struct{})
	go func() {
		defer close(inDone)
		_, _ = io.Copy(conn, in)
	}()
	select {
	case <-hostDone:
	case <-inDone:
	case <-ctx.Done():
	}
	_ = conn.Close()
	<-hostDone
}

// runRelay is Main's --stdio mode on the real process: stdin and stdout are
// the stream; anything to say goes to stderr.
func runRelay() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	dir, err := config.Dir()
	if err == nil {
		err = Relay(ctx, dir, os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "flint host relay:", err)
	}
	return RelayExitCode(err)
}
