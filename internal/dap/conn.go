// Package dap is a client for the Debug Adapter Protocol: it starts a debug
// adapter (Delve's `dlv dap` for Go), drives the launch handshake and exposes the
// requests Flint's debugger needs — breakpoints, stepping, stack, variables.
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// maxMessage guards against a corrupt Content-Length allocating gigabytes.
const maxMessage = 64 << 20

// ErrClosed is returned for requests on a connection whose adapter went away.
var ErrClosed = errors.New("debug adapter connection closed")

// message is any DAP protocol message: request, response or event.
type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    *bool           `json:"success,omitempty"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Event      string          `json:"event,omitempty"`
}

// Error is a failed response from the adapter.
type Error struct {
	Command string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Command, e.Message) }

// conn is a DAP connection with Content-Length framing. Events go to onEvent in
// the order they arrive; reverse requests from the adapter are refused.
type conn struct {
	w       io.Writer
	onEvent func(event string, body json.RawMessage)

	writeMu sync.Mutex
	seq     int

	mu      sync.Mutex
	pending map[int]chan *message
	closed  bool
	done    chan struct{}
}

func newConn(r io.Reader, w io.Writer, onEvent func(string, json.RawMessage)) *conn {
	c := &conn{w: w, onEvent: onEvent, pending: map[int]chan *message{}, done: make(chan struct{})}
	go c.readLoop(bufio.NewReaderSize(r, 64<<10))
	return c
}

// Request sends command and decodes the response body into out (which may be nil).
func (c *conn) Request(ctx context.Context, command string, args, out any) error {
	ch := make(chan *message, 1)
	seq, err := c.send(message{Type: "request", Command: command, Arguments: mustJSON(args)}, ch)
	if err != nil {
		return err
	}
	defer func() {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
	}()
	select {
	case resp := <-ch:
		if resp.Success == nil || !*resp.Success {
			return &Error{Command: command, Message: responseError(resp)}
		}
		if out == nil || len(resp.Body) == 0 || string(resp.Body) == "null" {
			return nil
		}
		return json.Unmarshal(resp.Body, out)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	}
}

// Done is closed once the adapter's side of the connection ends.
func (c *conn) Done() <-chan struct{} { return c.done }

// send numbers m and writes it; a non-nil ch is registered for the response
// before the write so a fast reply can't be missed.
func (c *conn) send(m message, ch chan *message) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.seq++
	m.Seq = c.seq
	if ch != nil {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return 0, ErrClosed
		}
		c.pending[m.Seq] = ch
		c.mu.Unlock()
	}
	body, err := json.Marshal(m)
	if err != nil {
		return 0, fmt.Errorf("encode %s: %w", m.Command, err)
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		return 0, fmt.Errorf("write %s: %w", m.Command, err)
	}
	return m.Seq, nil
}

func (c *conn) readLoop(r *bufio.Reader) {
	defer c.shutdown()
	for {
		body, err := readMessage(r)
		if err != nil {
			return
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			continue // skip garbage rather than kill the session
		}
		switch m.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[m.RequestSeq]
			c.mu.Unlock()
			if ch != nil {
				ch <- &m
			}
		case "event":
			c.onEvent(m.Event, m.Body)
		case "request":
			// runInTerminal / startDebugging: Flint runs the debuggee itself.
			f := false
			go func() {
				_, _ = c.send(message{Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: &f, Message: "not supported"}, nil)
			}()
		}
	}
}

func (c *conn) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
}

// responseError picks the most useful text of a failed response: the formatted
// error in the body, then the short message.
func responseError(m *message) string {
	var body struct {
		Error *struct {
			Format string `json:"format"`
		} `json:"error"`
	}
	if json.Unmarshal(m.Body, &body) == nil && body.Error != nil && body.Error.Format != "" {
		return body.Error.Format
	}
	if m.Message != "" {
		return m.Message
	}
	return "request failed"
}

// readMessage reads one Content-Length framed body.
func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if length >= 0 {
				break
			}
			continue // tolerate stray blank lines between messages
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > maxMessage {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
			length = n
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
