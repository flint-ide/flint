// Package lsp runs language servers (gopls, tsserver, pyright, rust-analyzer) and
// exposes the features Flint needs: completion, hover, signature help,
// navigation (definition, type definition, implementations, references,
// symbols), rename, code actions, formatting and diagnostics.
package lsp

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
	"sync/atomic"
)

// maxMessage guards against a corrupt Content-Length allocating gigabytes.
const maxMessage = 256 << 20

// ErrClosed is returned for calls on a connection whose server went away.
var ErrClosed = errors.New("language server connection closed")

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error returned by the server.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

// incoming handles server→client requests and notifications. For requests the
// returned value is sent back as the result.
type incoming func(method string, params json.RawMessage) (any, error)

// conn is a JSON-RPC 2.0 connection with LSP's Content-Length framing.
type conn struct {
	w      io.Writer
	handle incoming

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *message
	closed  bool
	done    chan struct{}
}

func newConn(r io.Reader, w io.Writer, handle incoming) *conn {
	c := &conn{w: w, handle: handle, pending: map[int64]chan *message{}, done: make(chan struct{})}
	go c.readLoop(bufio.NewReaderSize(r, 64<<10))
	return c
}

// Call sends a request and decodes the result into out (which may be nil).
func (c *conn) Call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan *message, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	raw := json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.send(message{ID: &raw, Method: method, Params: mustJSON(params)}); err != nil {
		return err
	}
	select {
	case resp := <-ch:
		if resp == nil {
			return ErrClosed
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	case <-ctx.Done():
		_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	}
}

// Notify sends a notification.
func (c *conn) Notify(method string, params any) error {
	return c.send(message{Method: method, Params: mustJSON(params)})
}

func (c *conn) send(m message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode %s: %w", m.Method, err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.w.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
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
		switch {
		case m.Method != "" && m.ID != nil:
			go c.reply(m) // server→client request; may block (e.g. configuration)
		case m.Method != "":
			_, _ = c.handle(m.Method, m.Params)
		case m.ID != nil:
			id, err := strconv.ParseInt(string(*m.ID), 10, 64)
			if err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			c.mu.Unlock()
			if ch != nil {
				ch <- &m
			}
		}
	}
}

func (c *conn) reply(m message) {
	result, err := c.handle(m.Method, m.Params)
	resp := message{ID: m.ID}
	if err != nil {
		resp.Error = &RPCError{Code: -32601, Message: err.Error()}
	} else {
		resp.Result = mustJSON(result)
	}
	_ = c.send(resp)
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
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > maxMessage {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
