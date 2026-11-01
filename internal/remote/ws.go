package remote

// The server side of WebSocket (RFC 6455), standard library only: the
// handshake, unfragmented text frames out, and frames in with every check the
// RFC asks of a server (masking, reserved bits, opcodes, control frame
// rules, fragmentation order, UTF-8, size). No extensions, no subprotocols.
// In phase 1 the client sends nothing but control frames; data messages are
// read, checked and passed to the caller, which drops them.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Opcodes.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xa
)

// Close codes.
const (
	closeNormal        = 1000
	closeGoingAway     = 1001
	closeProtocolError = 1002
	closeInvalidData   = 1007
	closePolicy        = 1008
	closeTooBig        = 1009
	closeInternal      = 1011
	closeTryAgainLater = 1013
)

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	// maxClientMessage bounds one message a client sends (all its fragments).
	maxClientMessage = 64 << 10
	maxControl       = 125
	writeTimeout     = 10 * time.Second
)

// closeError ends the read loop: the peer closed, or broke the protocol (the
// server has already sent its close frame).
type closeError struct {
	code   int
	reason string
}

func (e *closeError) Error() string { return fmt.Sprintf("websocket closed: %d %s", e.code, e.reason) }

// acceptKey is Sec-WebSocket-Accept for a client's Sec-WebSocket-Key.
func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// headerHas reports whether a comma-separated header lists token.
func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// checkHandshake validates an opening handshake; it returns the accept key,
// or the HTTP status to refuse it with.
func checkHandshake(r *http.Request) (string, int) {
	switch {
	case r.Method != http.MethodGet:
		return "", http.StatusMethodNotAllowed
	case !r.ProtoAtLeast(1, 1):
		return "", http.StatusBadRequest
	case !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket"):
		return "", http.StatusUpgradeRequired
	case r.Header.Get("Sec-WebSocket-Version") != "13":
		return "", http.StatusUpgradeRequired
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		return "", http.StatusBadRequest
	}
	return acceptKey(key), 0
}

// upgrade completes the handshake and takes the connection over. On failure
// the response has been written.
func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	accept, status := checkHandshake(r)
	if status != 0 {
		if status == http.StatusUpgradeRequired {
			w.Header().Set("Sec-WebSocket-Version", "13")
			w.Header().Set("Upgrade", "websocket")
		}
		http.Error(w, "websocket handshake required", status)
		return nil, fmt.Errorf("handshake: %d", status)
	}
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, "websocket unsupported", http.StatusInternalServerError)
		return nil, err
	}
	// The HTTP server's deadlines no longer apply; the stream sets its own.
	_ = conn.SetDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n")
	if err == nil {
		err = rw.Flush()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return newWSConn(conn, rw.Reader), nil
}

// wsConn is one server-side WebSocket connection. Writes are serialized; one
// goroutine runs readLoop.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
	// readTimeout ends the connection when nothing (not even a pong) arrives.
	readTimeout time.Duration

	wmu        sync.Mutex
	closeSent  bool
	closeOnce  sync.Once
	writeError error
	// hurried: the stream is ending; writes get closeWait, not writeTimeout.
	hurried atomic.Bool
}

// hurry caps the current and every later write at closeWait, so a client
// that stopped reading can't hold the server for writeTimeout.
func (c *wsConn) hurry() {
	c.hurried.Store(true)
	_ = c.conn.SetWriteDeadline(time.Now().Add(closeWait))
}

func newWSConn(conn net.Conn, br *bufio.Reader) *wsConn {
	return &wsConn{conn: conn, br: br, readTimeout: 2*pingInterval + 10*time.Second}
}

func (c *wsConn) writeFrame(op byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closeSent {
		return errors.New("websocket: close already sent")
	}
	if c.writeError != nil {
		return c.writeError
	}
	var hdr [10]byte
	hdr[0] = 0x80 | op
	n := 2
	switch l := len(payload); {
	case l <= 125:
		hdr[1] = byte(l)
	case l <= 0xffff:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(l))
		n = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(l))
		n = 10
	}
	timeout := writeTimeout
	if c.hurried.Load() {
		timeout = closeWait
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(timeout))
	if timeout != closeWait && c.hurried.Load() { // hurry ran between the two lines above
		_ = c.conn.SetWriteDeadline(time.Now().Add(closeWait))
	}
	bufs := net.Buffers{hdr[:n]}
	if len(payload) > 0 {
		bufs = append(bufs, payload)
	}
	if _, err := bufs.WriteTo(c.conn); err != nil { // one writev on TCP
		c.writeError = err
		return err
	}
	if op == opClose {
		c.closeSent = true
	}
	return nil
}

func (c *wsConn) writeText(p []byte) error { return c.writeFrame(opText, p) }
func (c *wsConn) ping() error              { return c.writeFrame(opPing, nil) }

// sendClose sends a close frame once (later calls do nothing).
func (c *wsConn) sendClose(code int, reason string) {
	c.closeOnce.Do(func() {
		if len(reason) > maxControl-2 {
			reason = reason[:maxControl-2]
		}
		p := make([]byte, 2+len(reason))
		binary.BigEndian.PutUint16(p, uint16(code))
		copy(p[2:], reason)
		_ = c.writeFrame(opClose, p)
	})
}

// close ends the TCP connection (readLoop returns).
func (c *wsConn) close() error { return c.conn.Close() }

// fail sends a close frame with code and returns the error that ends readLoop.
func (c *wsConn) fail(code int, reason string) error {
	c.sendClose(code, reason)
	return &closeError{code: code, reason: reason}
}

// readLoop reads frames until the connection ends: it answers pings, ends on
// a close frame (answering it) or a protocol error (sending a close frame
// first), and passes each complete data message to onMessage.
func (c *wsConn) readLoop(onMessage func([]byte)) error {
	var (
		msg      []byte
		inFrag   bool
		fragText bool
	)
	for {
		if c.readTimeout > 0 {
			_ = c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
		}
		var hdr [2]byte
		if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
			return err
		}
		fin, op := hdr[0]&0x80 != 0, hdr[0]&0x0f
		if hdr[0]&0x70 != 0 {
			return c.fail(closeProtocolError, "reserved bits set")
		}
		if hdr[1]&0x80 == 0 {
			return c.fail(closeProtocolError, "client frames must be masked")
		}
		length := uint64(hdr[1] & 0x7f)
		switch length {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.br, b[:]); err != nil {
				return err
			}
			length = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.br, b[:]); err != nil {
				return err
			}
			length = binary.BigEndian.Uint64(b[:])
			if length>>63 != 0 {
				return c.fail(closeProtocolError, "invalid length")
			}
		}
		control := op >= opClose
		switch {
		case op != opContinuation && op != opText && op != opBinary && op != opClose && op != opPing && op != opPong:
			return c.fail(closeProtocolError, "unknown opcode")
		case control && (!fin || length > maxControl):
			return c.fail(closeProtocolError, "invalid control frame")
		case op == opContinuation && !inFrag:
			return c.fail(closeProtocolError, "continuation without a message")
		case (op == opText || op == opBinary) && inFrag:
			return c.fail(closeProtocolError, "new message inside a fragmented one")
		case !control && uint64(len(msg))+length > maxClientMessage:
			return c.fail(closeTooBig, "message too big")
		}
		var mask [4]byte
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return err
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return err
			}
		case opPong:
			// Any frame resets the read deadline; nothing else to do.
		case opClose:
			return c.peerClosed(payload)
		default:
			if op != opContinuation {
				inFrag, fragText, msg = true, op == opText, msg[:0]
			}
			msg = append(msg, payload...)
			if fin {
				inFrag = false
				if fragText && !utf8.Valid(msg) {
					return c.fail(closeInvalidData, "invalid UTF-8")
				}
				onMessage(msg)
				msg = nil
			}
		}
	}
}

// validCloseCode reports a code a peer may send (RFC 6455 section 7.4):
// the defined codes except 1004 and the never-sent 1005, 1006 and 1015, and
// the ranges for libraries and applications.
func validCloseCode(code int) bool {
	switch {
	case code >= 1000 && code <= 1003, code >= 1007 && code <= 1014:
		return true
	case code >= 3000 && code <= 4999:
		return true
	}
	return false
}

// peerClosed answers a close frame and returns the code it carried.
func (c *wsConn) peerClosed(payload []byte) error {
	switch {
	case len(payload) == 1:
		return c.fail(closeProtocolError, "invalid close frame")
	case len(payload) >= 2:
		code := int(binary.BigEndian.Uint16(payload))
		if !validCloseCode(code) {
			return c.fail(closeProtocolError, "invalid close code")
		}
		if !utf8.Valid(payload[2:]) {
			return c.fail(closeInvalidData, "invalid close reason")
		}
		c.sendClose(closeNormal, "")
		return &closeError{code: code, reason: string(payload[2:])}
	}
	c.sendClose(closeNormal, "")
	return &closeError{code: closeNormal}
}
