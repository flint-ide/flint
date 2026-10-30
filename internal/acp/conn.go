package acp

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

// requestFunc serves one inbound request. It runs on its own goroutine; the
// returned value is marshalled as the JSON-RPC result.
type requestFunc func(ctx context.Context, method string, params json.RawMessage) (any, error)

// notificationFunc handles one inbound notification. Calls are serialised
// and in wire order.
type notificationFunc func(method string, params json.RawMessage)

type callResult struct {
	result json.RawMessage
	err    error
}

type cancelRequestParams struct {
	RequestID json.RawMessage `json:"requestId"`
}

// conn is a bidirectional JSON-RPC 2.0 connection over newline-delimited
// JSON. It correlates outbound requests with responses, serves inbound
// requests concurrently, and delivers notifications and responses in the
// order they were received.
type conn struct {
	w         io.Writer
	wmu       sync.Mutex
	onRequest requestFunc
	onNotify  notificationFunc

	nextID     atomic.Int64
	baseCtx    context.Context
	cancelBase context.CancelFunc

	mu       sync.Mutex
	pending  map[int64]chan callResult
	inflight map[string]context.CancelCauseFunc
	closeErr error // non-nil once shut down
	readErr  error // fatal (non-EOF) read error, if any

	queue    *msgQueue
	handlers sync.WaitGroup
	drained  chan struct{} // reader finished and dispatcher drained the queue
}

func newConn(w io.Writer, onRequest requestFunc, onNotify notificationFunc) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &conn{
		w:          w,
		onRequest:  onRequest,
		onNotify:   onNotify,
		baseCtx:    ctx,
		cancelBase: cancel,
		pending:    map[int64]chan callResult{},
		inflight:   map[string]context.CancelCauseFunc{},
		queue:      newMsgQueue(),
		drained:    make(chan struct{}),
	}
}

// start begins reading r and dispatching messages.
func (c *conn) start(r io.Reader) {
	go c.readLoop(r)
	go c.dispatchLoop()
}

// Drained is closed once the input stream ended and every message read
// before that has been dispatched.
func (c *conn) Drained() <-chan struct{} { return c.drained }

// ReadErr returns the fatal read error, if the stream failed for a reason
// other than EOF or being closed.
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
		c.decode(line)
	}
}

// decode parses one line (a message or a batch) and queues the result.
func (c *conn) decode(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	if line[0] != '[' {
		c.decodeOne(line)
		return
	}
	var batch []json.RawMessage
	if err := json.Unmarshal(line, &batch); err != nil {
		c.replyParseError(err)
		return
	}
	for _, raw := range batch {
		c.decodeOne(raw)
	}
}

func (c *conn) decodeOne(raw []byte) {
	var m inboundMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		c.replyParseError(err)
		return
	}
	c.queue.push(&m)
}

func (c *conn) replyParseError(err error) {
	rpcErr := newRPCError(CodeParseError, "Parse error: "+err.Error(), nil)
	c.goTracked(func() {
		_ = c.send(outboundError{JSONRPC: jsonrpcVersion, ID: json.RawMessage("null"), Error: rpcErr})
	})
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
			c.startRequest(m)
		case m.Method != "":
			c.handleNotification(m)
		case m.hasID():
			c.handleResponse(m)
		}
	}
}

func (c *conn) handleNotification(m *inboundMessage) {
	if m.Method != methodCancelRequest {
		c.onNotify(m.Method, m.Params)
		return
	}
	var p cancelRequestParams
	if err := json.Unmarshal(m.Params, &p); err != nil || len(p.RequestID) == 0 {
		return
	}
	c.mu.Lock()
	cancel := c.inflight[idKey(p.RequestID)]
	c.mu.Unlock()
	if cancel != nil {
		cancel(errRequestCancelled)
	}
}

// errRequestCancelled is the context cause for `$/cancel_request`.
var errRequestCancelled = errors.New("acp: request cancelled by agent")

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
		return // late response to an abandoned call
	}
	if m.Error != nil {
		ch <- callResult{err: m.Error}
		return
	}
	ch <- callResult{result: m.Result}
}

func (c *conn) startRequest(m *inboundMessage) {
	key := idKey(m.ID)
	c.mu.Lock()
	if c.closeErr != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancelCause(c.baseCtx)
	c.inflight[key] = cancel
	c.handlers.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.handlers.Done()
		defer func() {
			c.mu.Lock()
			delete(c.inflight, key)
			c.mu.Unlock()
			cancel(nil)
		}()
		result, err := c.onRequest(ctx, m.Method, m.Params)
		c.reply(ctx, m.ID, result, err)
	}()
}

// reply sends the result or error for an inbound request. Write failures
// are ignored: they mean the agent is gone.
func (c *conn) reply(ctx context.Context, id json.RawMessage, result any, err error) {
	if err != nil {
		_ = c.send(outboundError{JSONRPC: jsonrpcVersion, ID: id, Error: toRPCError(ctx, err)})
		return
	}
	raw, mErr := json.Marshal(result)
	if mErr != nil {
		rpcErr := newRPCError(CodeInternalError, "Internal error: "+mErr.Error(), nil)
		_ = c.send(outboundError{JSONRPC: jsonrpcVersion, ID: id, Error: rpcErr})
		return
	}
	_ = c.send(outboundResult{JSONRPC: jsonrpcVersion, ID: id, Result: raw})
}

func toRPCError(ctx context.Context, err error) *RPCError {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	if ctx.Err() != nil {
		return newRPCError(CodeRequestCancelled, "Request cancelled", nil)
	}
	return newRPCError(CodeInternalError, "Internal error: "+err.Error(), nil)
}

// call sends a request and waits for its response, decoding the result into
// result (if non-nil). When ctx is cancelled: with onCancel == nil the call
// is abandoned and `$/cancel_request` is sent; otherwise onCancel runs once
// and the call keeps waiting for the agent's response.
func (c *conn) call(ctx context.Context, method string, params, result any, onCancel func()) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	id := c.nextID.Add(1)
	ch := make(chan callResult, 1)
	c.mu.Lock()
	if c.closeErr != nil {
		err := c.closeErr
		c.mu.Unlock()
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(outboundRequest{JSONRPC: jsonrpcVersion, ID: id, Method: method, Params: params}); err != nil {
		c.forget(id)
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	raw, err := c.await(ctx, id, ch, onCancel)
	if err != nil {
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	if result != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, result); err != nil {
			return fmt.Errorf("acp: %s: decode result: %w", method, err)
		}
	}
	return nil
}

func (c *conn) await(ctx context.Context, id int64, ch chan callResult, onCancel func()) (json.RawMessage, error) {
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
	}
	if onCancel == nil {
		c.forget(id)
		_ = c.notify(methodCancelRequest, cancelRequestParams{RequestID: json.RawMessage(strconv.FormatInt(id, 10))})
		return nil, ctx.Err()
	}
	onCancel()
	r := <-ch
	return r.result, r.err
}

func (c *conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// notify sends a notification.
func (c *conn) notify(method string, params any) error {
	c.mu.Lock()
	closeErr := c.closeErr
	c.mu.Unlock()
	if closeErr != nil {
		return fmt.Errorf("acp: %s: %w", method, closeErr)
	}
	if err := c.send(outboundNotification{JSONRPC: jsonrpcVersion, Method: method, Params: params}); err != nil {
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	return nil
}

// send writes one message as a single line. json.Marshal escapes control
// characters, so the encoded message never contains a raw newline.
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

// goTracked runs fn on a goroutine counted by waitHandlers, unless the
// connection is already shut down.
func (c *conn) goTracked(fn func()) {
	c.mu.Lock()
	if c.closeErr != nil {
		c.mu.Unlock()
		return
	}
	c.handlers.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.handlers.Done()
		fn()
	}()
}

// shutdown fails every pending call with err, cancels every inbound request
// handler and rejects further calls. Only the first call has an effect.
func (c *conn) shutdown(err error) {
	c.mu.Lock()
	if c.closeErr != nil {
		c.mu.Unlock()
		return
	}
	c.closeErr = err
	pending, inflight := c.pending, c.inflight
	c.pending = map[int64]chan callResult{}
	c.inflight = map[string]context.CancelCauseFunc{}
	c.mu.Unlock()

	for _, ch := range pending {
		ch <- callResult{err: err}
	}
	for _, cancel := range inflight {
		cancel(err)
	}
	c.cancelBase()
}

// waitHandlers blocks until every inbound request goroutine has returned.
// Call it only after shutdown.
func (c *conn) waitHandlers() { c.handlers.Wait() }

// parseID decodes a response id; we only ever send integer ids, but tolerate
// agents that echo them back as strings.
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

// idKey normalises a request id for map lookups.
func idKey(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
