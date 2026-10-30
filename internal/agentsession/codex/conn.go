package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
)

// requestFunc receives a server → client request on the dispatcher
// goroutine, in wire order. It must not block for long: it either replies
// right away or registers the request and replies later via conn.reply.
type requestFunc func(id json.RawMessage, method string, params json.RawMessage)

// notificationFunc handles one notification. Calls are serialised in wire order.
type notificationFunc func(method string, params json.RawMessage)

type callResult struct {
	result json.RawMessage
	err    error
}

// conn is a bidirectional JSON-RPC connection over newline-delimited JSON.
// Requests, notifications and responses are dispatched on one goroutine in
// the order they were read, so a response is never handled before the
// notifications that preceded it.
type conn struct {
	w         io.Writer
	wmu       sync.Mutex
	onRequest requestFunc
	onNotify  notificationFunc
	nextID    atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan callResult
	closeErr error
	readErr  error

	queue   *msgQueue
	drained chan struct{}
}

func newConn(w io.Writer, onRequest requestFunc, onNotify notificationFunc) *conn {
	return &conn{
		w:         w,
		onRequest: onRequest,
		onNotify:  onNotify,
		pending:   map[int64]chan callResult{},
		queue:     newMsgQueue(),
		drained:   make(chan struct{}),
	}
}

func (c *conn) start(r io.Reader) {
	go c.readLoop(r)
	go c.dispatchLoop()
}

// Drained is closed once input ended and every message read was dispatched.
func (c *conn) Drained() <-chan struct{} { return c.drained }

// ReadErr returns the fatal (non-EOF) read error, if any.
func (c *conn) ReadErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readErr
}

func (c *conn) readLoop(r io.Reader) {
	defer c.queue.close()
	lr := newLineReader(r, maxMessageBytes)
	for {
		line, err := lr.next()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				c.mu.Lock()
				c.readErr = err
				c.mu.Unlock()
			}
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m inboundMessage
		if json.Unmarshal(line, &m) != nil {
			continue // not JSON (e.g. a stray log line); ignore it
		}
		c.queue.push(&m)
	}
}

func (c *conn) dispatchLoop() {
	defer close(c.drained)
	for {
		m, ok := c.queue.pop()
		if !ok {
			return
		}
		switch {
		case m.Method != "" && m.hasID():
			if c.isOpen() {
				c.onRequest(m.ID, m.Method, m.Params)
			}
		case m.Method != "":
			c.onNotify(m.Method, m.Params)
		case m.hasID():
			c.handleResponse(m)
		}
	}
}

func (c *conn) isOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr == nil
}

func (c *conn) handleResponse(m *inboundMessage) {
	id, ok := parseID(m.ID)
	if !ok {
		return
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch == nil {
		return
	}
	if m.Error != nil {
		ch <- callResult{err: m.Error}
		return
	}
	ch <- callResult{result: m.Result}
}

// call sends a request and waits for its response, decoding the result into
// result (if non-nil). A cancelled ctx abandons the call.
func (c *conn) call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("codex: %s: %w", method, err)
	}
	id := c.nextID.Add(1)
	ch := make(chan callResult, 1)
	c.mu.Lock()
	if c.closeErr != nil {
		err := c.closeErr
		c.mu.Unlock()
		return fmt.Errorf("codex: %s: %w", method, err)
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(outboundRequest{ID: id, Method: method, Params: params}); err != nil {
		c.forget(id)
		return fmt.Errorf("codex: %s: %w", method, err)
	}
	var r callResult
	select {
	case r = <-ch:
	case <-ctx.Done():
		c.forget(id)
		return fmt.Errorf("codex: %s: %w", method, ctx.Err())
	}
	if r.err != nil {
		return fmt.Errorf("codex: %s: %w", method, r.err)
	}
	if result != nil && len(r.result) > 0 {
		if err := json.Unmarshal(r.result, result); err != nil {
			return fmt.Errorf("codex: %s: decode result: %w", method, err)
		}
	}
	return nil
}

func (c *conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// notify sends a notification.
func (c *conn) notify(method string, params any) error {
	if !c.isOpen() {
		return fmt.Errorf("codex: %s: connection closed", method)
	}
	if err := c.send(outboundNotification{Method: method, Params: params}); err != nil {
		return fmt.Errorf("codex: %s: %w", method, err)
	}
	return nil
}

// fire sends a request without waiting; its response is dropped on arrival.
// The dispatcher goroutine uses it, as it cannot wait for a response.
func (c *conn) fire(method string, params any) error {
	if !c.isOpen() {
		return fmt.Errorf("codex: %s: connection closed", method)
	}
	if err := c.send(outboundRequest{ID: c.nextID.Add(1), Method: method, Params: params}); err != nil {
		return fmt.Errorf("codex: %s: %w", method, err)
	}
	return nil
}

// reply answers a server request. Write failures mean the server is gone.
func (c *conn) reply(id json.RawMessage, result any) error {
	return c.send(outboundResult{ID: id, Result: result})
}

// replyError answers a server request with an error.
func (c *conn) replyError(id json.RawMessage, code int, msg string) error {
	return c.send(outboundError{ID: id, Error: &RPCError{Code: code, Message: msg}})
}

// send writes one message as a single line (json.Marshal never emits a raw newline).
func (c *conn) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	data = append(data, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.w.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// shutdown fails every pending call with err and rejects further calls.
// Only the first call has an effect.
func (c *conn) shutdown(err error) {
	c.mu.Lock()
	if c.closeErr != nil {
		c.mu.Unlock()
		return
	}
	c.closeErr = err
	pending := c.pending
	c.pending = map[int64]chan callResult{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- callResult{err: err}
	}
}

// parseID decodes a response id; we send integers but tolerate strings.
func parseID(raw json.RawMessage) (int64, bool) {
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// requestKey renders a request id as a plain string ("7" or "abc").
func requestKey(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}
