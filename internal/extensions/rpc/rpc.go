// Package rpc is JSON-RPC 2.0 over a pair of byte streams, one message per
// line (UTF-8 JSON without raw newlines), as MCP's stdio transport: how Flint
// talks to an external extension's process (docs/design/extensions.md). Both
// sides send requests, responses and notifications.
//
// The connection is defensive: a message larger than the limit, a line that
// isn't JSON-RPC or a flood of output closes it (the owner then stops the
// process), requests from the other side run with bounded concurrency, and a
// request that times out is cancelled with $/cancelRequest, as in LSP.
package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
)

// JSON-RPC error codes; CodePermission is Flint's: the extension's manifest
// doesn't declare the permission the method needs.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
	CodePermission     = -32001
	CodeCancelled      = -32800
)

// Defaults of Options.
const (
	DefaultMaxMessage    = 4 << 20
	DefaultMaxConcurrent = 16
	// notifyWait bounds how long a notification waits for the writer.
	notifyWait = 2 * time.Second
	// outQueue is how many messages wait for the writer.
	outQueue = 64
)

// ErrClosed is returned by calls on a closed connection.
var ErrClosed = errors.New("connection closed")

// Error is a JSON-RPC error object; handlers return one to choose the code.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Errorf builds an *Error.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Options configure a Conn.
type Options struct {
	// OnRequest answers a request of the other side, each in its own
	// goroutine; ctx ends when the other side cancels it or the connection
	// closes. Return an *Error to pick the error code. Nil answers
	// "method not found".
	OnRequest func(ctx context.Context, method string, params json.RawMessage) (any, error)
	// OnNotify handles a notification on the reading goroutine, in order:
	// it must not block.
	OnNotify func(method string, params json.RawMessage)
	// MaxMessage bounds a message in bytes, both ways (DefaultMaxMessage).
	MaxMessage int
	// MaxConcurrent bounds the other side's requests in flight
	// (DefaultMaxConcurrent); more are answered with an error.
	MaxConcurrent int
	// MaxRate, when set, closes the connection after the other side wrote
	// more than MaxRate bytes per second for RateWindow seconds in a row.
	MaxRate    int
	RateWindow int
}

// Conn is one connection. Create it with New, then run Serve with the input.
type Conn struct {
	o    Options
	out  chan []byte
	done chan struct{}
	err  error // set before done is closed
	once sync.Once

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan *message
	incoming map[string]context.CancelFunc
	sem      chan struct{}
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type resultReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type errorReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *Error          `json:"error"`
}

// New starts the writer to w; Serve reads the other direction.
func New(w io.Writer, o Options) *Conn {
	if o.MaxMessage <= 0 {
		o.MaxMessage = DefaultMaxMessage
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = DefaultMaxConcurrent
	}
	if o.RateWindow <= 0 {
		o.RateWindow = 5
	}
	c := &Conn{
		o: o, out: make(chan []byte, outQueue), done: make(chan struct{}),
		pending: map[int64]chan *message{}, incoming: map[string]context.CancelFunc{},
		sem: make(chan struct{}, o.MaxConcurrent),
	}
	go c.write(w)
	return c
}

func (c *Conn) write(w io.Writer) {
	for {
		select {
		case data := <-c.out:
			if _, err := w.Write(data); err != nil {
				c.Close(fmt.Errorf("write: %w", err))
				return
			}
		case <-c.done:
			return
		}
	}
}

// Done is closed when the connection is closed.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection closed (nil while open, io.EOF at a clean end).
func (c *Conn) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

// Close closes the connection with err as its reason; pending calls fail and
// handlers' contexts end. Only the first reason is kept.
func (c *Conn) Close(err error) {
	c.once.Do(func() {
		if err == nil {
			err = ErrClosed
		}
		c.err = err
		close(c.done)
		c.mu.Lock()
		for _, cancel := range c.incoming {
			cancel()
		}
		c.mu.Unlock()
	})
}

// Call sends a request and decodes its result into result (nil: ignored).
// When ctx ends first, the request is cancelled with $/cancelRequest.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *message, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(ctx, request{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return msg.Error
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(msg.Result, result); err != nil {
			return fmt.Errorf("%s: invalid result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		_ = c.Notify("$/cancelRequest", map[string]int64{"id": id})
		return ctx.Err()
	case <-c.done:
		return c.err
	}
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyWait)
	defer cancel()
	return c.send(ctx, request{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Conn) send(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) >= c.o.MaxMessage {
		return fmt.Errorf("message of %d bytes is over the limit of %d", len(data), c.o.MaxMessage)
	}
	data = append(data, '\n')
	select {
	case <-c.done:
		return c.err
	default:
	}
	select {
	case c.out <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.err
	}
}

// Serve reads messages from r until it ends or breaks the protocol, then
// closes the connection and returns the reason (io.EOF at a clean end).
func (c *Conn) Serve(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)
	rate := rateLimit{max: c.o.MaxRate, window: c.o.RateWindow}
	for {
		line, err := readLine(br, c.o.MaxMessage)
		if err == nil {
			err = rate.add(len(line), time.Now())
		}
		if err == nil {
			err = c.dispatch(line)
		}
		if err != nil {
			c.Close(err)
			return c.Err()
		}
	}
}

// readLine returns the next non-empty line without its end.
func readLine(br *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		part, err := br.ReadSlice('\n')
		if len(buf)+len(part) > limit+2 { // the line end does not count
			return nil, fmt.Errorf("message larger than %d bytes", limit)
		}
		buf = append(buf, part...)
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil && (len(buf) == 0 || !errors.Is(err, io.EOF)):
			return nil, err
		}
		line := bytes.TrimSpace(buf)
		if len(line) > 0 {
			return line, nil
		}
		if err != nil {
			return nil, err
		}
		buf = buf[:0]
	}
}

func (c *Conn) dispatch(line []byte) error {
	var msg message
	if err := json.Unmarshal(line, &msg); err != nil {
		return fmt.Errorf("not a JSON-RPC message (stdout carries only the protocol; log to stderr): %.80q", line)
	}
	if msg.JSONRPC != "2.0" {
		return fmt.Errorf(`message without "jsonrpc": "2.0": %.80q`, line)
	}
	switch {
	case msg.Method == "" && msg.ID != nil:
		c.response(&msg)
	case msg.Method == "":
		return fmt.Errorf("message without method or id: %.80q", line)
	case msg.ID == nil:
		c.notification(&msg)
	default:
		c.request(&msg)
	}
	return nil
}

func (c *Conn) response(msg *message) {
	id, err := strconv.ParseInt(string(msg.ID), 10, 64)
	if err != nil {
		return // not one of ours (ours are integers)
	}
	c.mu.Lock()
	ch := c.pending[id]
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- msg:
		default: // answered twice
		}
	}
}

func (c *Conn) notification(msg *message) {
	if msg.Method == "$/cancelRequest" {
		var p struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(msg.Params, &p) == nil {
			c.mu.Lock()
			cancel := c.incoming[string(p.ID)]
			c.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		return
	}
	if c.o.OnNotify != nil {
		c.o.OnNotify(msg.Method, msg.Params)
	}
}

func (c *Conn) request(msg *message) {
	if !validID(msg.ID) {
		c.reply(msg.ID, nil, Errorf(CodeInvalidRequest, "id must be a number or a string"))
		return
	}
	select {
	case c.sem <- struct{}{}:
	default:
		c.reply(msg.ID, nil, Errorf(CodeInternal, "too many requests in flight (at most %d)", c.o.MaxConcurrent))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	key := string(msg.ID)
	c.mu.Lock()
	if c.incoming[key] != nil {
		c.mu.Unlock()
		cancel()
		<-c.sem
		c.reply(msg.ID, nil, Errorf(CodeInvalidRequest, "id %s is already in use", key))
		return
	}
	c.incoming[key] = cancel
	c.mu.Unlock()
	select {
	case <-c.done:
		cancel()
	default:
	}
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.incoming, key)
			c.mu.Unlock()
			cancel()
			<-c.sem
		}()
		result, err := c.handle(ctx, msg)
		c.reply(msg.ID, result, err)
	}()
}

func (c *Conn) handle(ctx context.Context, msg *message) (result any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = Errorf(CodeInternal, "internal error in %s", msg.Method)
		}
	}()
	if c.o.OnRequest == nil {
		return nil, Errorf(CodeMethodNotFound, "method not found: %s", msg.Method)
	}
	return c.o.OnRequest(ctx, msg.Method, msg.Params)
}

func (c *Conn) reply(id json.RawMessage, result any, err error) {
	if id == nil {
		id = json.RawMessage("null")
	}
	var v any = resultReply{JSONRPC: "2.0", ID: id, Result: result}
	if err != nil {
		var rpcErr *Error
		if !errors.As(err, &rpcErr) {
			rpcErr = &Error{Code: CodeInternal, Message: err.Error()}
		}
		v = errorReply{JSONRPC: "2.0", ID: id, Error: rpcErr}
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyWait)
	defer cancel()
	if sendErr := c.send(ctx, v); sendErr != nil && err == nil {
		// The result itself could not be sent (too large): say so instead.
		_ = c.send(ctx, errorReply{JSONRPC: "2.0", ID: id, Error: Errorf(CodeInternal, "%v", sendErr)})
	}
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	if id[0] == '"' {
		var s string
		return json.Unmarshal(id, &s) == nil
	}
	var n json.Number
	return json.Unmarshal(id, &n) == nil
}

// rateLimit fails after more than max bytes arrived in each of window
// consecutive seconds.
type rateLimit struct {
	max, window int
	start       time.Time
	bytes, hot  int
}

func (r *rateLimit) add(n int, now time.Time) error {
	if r.max <= 0 {
		return nil
	}
	if elapsed := now.Sub(r.start); elapsed >= time.Second {
		switch {
		case r.bytes > r.max && elapsed < 2*time.Second:
			r.hot++
		default:
			r.hot = 0
		}
		r.start, r.bytes = now, 0
	}
	r.bytes += n
	if r.hot >= r.window {
		return fmt.Errorf("more than %d MiB/s of output for %d s", r.max>>20, r.window)
	}
	return nil
}
