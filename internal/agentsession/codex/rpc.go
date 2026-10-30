package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// JSON-RPC error codes used when answering app-server requests.
const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// maxMessageBytes caps one inbound line. Multi-megabyte messages (file
// contents, command output) are normal; this only guards against a runaway
// server exhausting memory.
const maxMessageBytes = 64 << 20

// stderrCapacity bounds how much app-server stderr is retained.
const stderrCapacity = 32 << 10

var errMessageTooLarge = errors.New("codex: inbound message exceeds size limit")

// RPCError is a JSON-RPC error object received from (or sent to) the app-server.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements error.
func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("rpc error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// inboundMessage is any message read from the app-server. The app-server
// speaks JSON-RPC 2.0 without the "jsonrpc" member; which fields are set
// determines the kind (request, notification or response).
type inboundMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

func (m *inboundMessage) hasID() bool {
	return len(m.ID) > 0 && string(m.ID) != "null"
}

type outboundRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type outboundNotification struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type outboundResult struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result"`
}

type outboundError struct {
	ID    json.RawMessage `json:"id"`
	Error *RPCError       `json:"error"`
}

// lineReader splits a byte stream into newline-delimited messages of
// arbitrary (bounded) length.
type lineReader struct {
	r   *bufio.Reader
	max int
}

func newLineReader(r io.Reader, max int) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10), max: max}
}

// next returns the next line without its terminator. A final unterminated
// line is returned before io.EOF.
func (l *lineReader) next() ([]byte, error) {
	var line []byte
	for {
		chunk, err := l.r.ReadSlice('\n')
		if len(line)+len(chunk) > l.max+2 {
			return nil, fmt.Errorf("%w (%d bytes)", errMessageTooLarge, l.max)
		}
		line = append(line, chunk...)
		switch {
		case err == nil:
			return trimEOL(line), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return trimEOL(line), nil
		default:
			return nil, err
		}
	}
}

func trimEOL(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\n"))
	return bytes.TrimSuffix(b, []byte("\r"))
}

// msgQueue is an unbounded FIFO between the stdout reader and the
// dispatcher, so a slow handler never stalls reading the server's stdout.
type msgQueue struct {
	mu     sync.Mutex
	items  []*inboundMessage
	closed bool
	signal chan struct{}
}

func newMsgQueue() *msgQueue { return &msgQueue{signal: make(chan struct{}, 1)} }

func (q *msgQueue) push(m *inboundMessage) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, m)
	}
	q.mu.Unlock()
	wake(q.signal)
}

func (q *msgQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	wake(q.signal)
}

// pop blocks until a message is available or the queue is closed and empty.
func (q *msgQueue) pop() (*inboundMessage, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			m := q.items[0]
			q.items[0] = nil
			q.items = q.items[1:]
			q.mu.Unlock()
			return m, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return nil, false
		}
		<-q.signal
	}
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// ringBuffer is a concurrency-safe io.Writer keeping the last bytes written.
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	pos   int
	total int64
}

func newRingBuffer(capacity int) *ringBuffer { return &ringBuffer{buf: make([]byte, capacity)} }

// Write implements io.Writer; it never fails.
func (r *ringBuffer) Write(p []byte) (int, error) {
	n, size := len(p), len(r.buf)
	if n == 0 || size == 0 {
		return n, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total += int64(n)
	if n >= size {
		copy(r.buf, p[n-size:])
		r.pos = 0
		return n, nil
	}
	written := copy(r.buf[r.pos:], p)
	if written < n {
		copy(r.buf, p[written:])
	}
	r.pos = (r.pos + n) % size
	return n, nil
}

// Tail returns at most the last n retained bytes as valid UTF-8 text.
func (r *ringBuffer) Tail(n int) string {
	r.mu.Lock()
	var b []byte
	if r.total < int64(len(r.buf)) {
		b = append([]byte(nil), r.buf[:r.pos]...)
	} else {
		b = append(append(b, r.buf[r.pos:]...), r.buf[:r.pos]...)
	}
	r.mu.Unlock()
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.ToValidUTF8(string(b), "")
}
