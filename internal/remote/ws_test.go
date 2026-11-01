package remote

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// clientFrame encodes a frame as a client sends it (masked unless mask is nil).
func clientFrame(fin bool, op byte, payload []byte, mask []byte) []byte {
	var b bytes.Buffer
	b0 := op
	if fin {
		b0 |= 0x80
	}
	b.WriteByte(b0)
	maskBit := byte(0)
	if mask != nil {
		maskBit = 0x80
	}
	switch n := len(payload); {
	case n <= 125:
		b.WriteByte(maskBit | byte(n))
	case n <= 0xffff:
		b.WriteByte(maskBit | 126)
		_ = binary.Write(&b, binary.BigEndian, uint16(n))
	default:
		b.WriteByte(maskBit | 127)
		_ = binary.Write(&b, binary.BigEndian, uint64(n))
	}
	if mask != nil {
		b.Write(mask)
		masked := make([]byte, len(payload))
		for i := range payload {
			masked[i] = payload[i] ^ mask[i%4]
		}
		b.Write(masked)
	} else {
		b.Write(payload)
	}
	return b.Bytes()
}

var testMask = []byte{1, 2, 3, 4}

// serverFrame reads one frame the server wrote (never masked).
func serverFrame(t *testing.T, r *bufio.Reader) (op byte, payload []byte) {
	t.Helper()
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if h[0]&0x80 == 0 || h[0]&0x70 != 0 {
		t.Fatalf("server frame header %08b: want FIN and no RSV", h[0])
	}
	if h[1]&0x80 != 0 {
		t.Fatal("server frames must not be masked")
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var l uint16
		_ = binary.Read(r, binary.BigEndian, &l)
		n = uint64(l)
	case 127:
		_ = binary.Read(r, binary.BigEndian, &n)
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}
	return h[0] & 0x0f, payload
}

// pipeConn returns a server-side wsConn and the client's end of the pipe.
func pipeConn(t *testing.T) (*wsConn, net.Conn, *bufio.Reader) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	return newWSConn(server, bufio.NewReader(server)), client, bufio.NewReader(client)
}

func TestHandshakeAcceptKey(t *testing.T) {
	// The example of RFC 6455 section 1.3.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("accept = %q", got)
	}
}

func TestHandshakeChecks(t *testing.T) {
	valid := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
		r.Header.Set("Connection", "keep-alive, Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		return r
	}
	if _, status := checkHandshake(valid()); status != 0 {
		t.Fatalf("valid handshake refused with %d", status)
	}
	for name, tc := range map[string]struct {
		change func(*http.Request)
		want   int
	}{
		"post":         {func(r *http.Request) { r.Method = http.MethodPost }, http.StatusMethodNotAllowed},
		"no upgrade":   {func(r *http.Request) { r.Header.Del("Upgrade") }, http.StatusUpgradeRequired},
		"no conn":      {func(r *http.Request) { r.Header.Set("Connection", "close") }, http.StatusUpgradeRequired},
		"version 8":    {func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "8") }, http.StatusUpgradeRequired},
		"no key":       {func(r *http.Request) { r.Header.Del("Sec-WebSocket-Key") }, http.StatusBadRequest},
		"short key":    {func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "c2hvcnQ=") }, http.StatusBadRequest},
		"invalid key":  {func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "not base64!!") }, http.StatusBadRequest},
		"old protocol": {func(r *http.Request) { r.Proto, r.ProtoMinor = "HTTP/1.0", 0 }, http.StatusBadRequest},
	} {
		r := valid()
		tc.change(r)
		if _, status := checkHandshake(r); status != tc.want {
			t.Errorf("%s: status %d, want %d", name, status, tc.want)
		}
	}
}

func TestWriteTextEncodesEveryLengthForm(t *testing.T) {
	c, client, r := pipeConn(t)
	for _, n := range []int{0, 125, 126, 65535, 65536, 200_000} {
		payload := bytes.Repeat([]byte("x"), n)
		go func() { _ = c.writeText(payload) }()
		op, got := serverFrame(t, r)
		if op != opText || len(got) != n {
			t.Fatalf("n=%d: op %d, %d bytes", n, op, len(got))
		}
	}
	_ = client
}

// readResult runs readLoop on c and returns its error.
func readResult(c *wsConn) chan error {
	done := make(chan error, 1)
	go func() { done <- c.readLoop(func([]byte) {}) }()
	return done
}

func closeCode(t *testing.T, err error) int {
	t.Helper()
	var ce *closeError
	if !errors.As(err, &ce) {
		t.Fatalf("readLoop error %v, want a close error", err)
	}
	return ce.code
}

func TestMalformedFramesCloseTheConnection(t *testing.T) {
	big := bytes.Repeat([]byte("a"), maxClientMessage+1)
	cases := []struct {
		name  string
		bytes []byte
		code  int
	}{
		{"unmasked", clientFrame(true, opText, []byte("hi"), nil), closeProtocolError},
		{"rsv bit", func() []byte { f := clientFrame(true, opText, []byte("hi"), testMask); f[0] |= 0x40; return f }(), closeProtocolError},
		{"unknown opcode", clientFrame(true, 0x3, []byte("hi"), testMask), closeProtocolError},
		{"fragmented ping", clientFrame(false, opPing, []byte("hi"), testMask), closeProtocolError},
		{"long ping", clientFrame(true, opPing, bytes.Repeat([]byte("p"), 126), testMask), closeProtocolError},
		{"stray continuation", clientFrame(true, opContinuation, []byte("hi"), testMask), closeProtocolError},
		{"64-bit length with top bit", []byte{0x81, 0x80 | 127, 0x80, 0, 0, 0, 0, 0, 0, 1, 1, 2, 3, 4}, closeProtocolError},
		{"too big", clientFrame(true, opText, big, testMask), closeTooBig},
		{"invalid utf-8", clientFrame(true, opText, []byte{0xff, 0xfe}, testMask), closeInvalidData},
		{"close with one byte", clientFrame(true, opClose, []byte{1}, testMask), closeProtocolError},
		{"new message inside a fragmented one", append(clientFrame(false, opText, []byte("a"), testMask), clientFrame(true, opText, []byte("b"), testMask)...), closeProtocolError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, client, r := pipeConn(t)
			done := readResult(c)
			go func() { _, _ = client.Write(tc.bytes) }()
			// The server answers with a close frame carrying the code.
			op, payload := serverFrame(t, r)
			if op != opClose || len(payload) < 2 || int(binary.BigEndian.Uint16(payload)) != tc.code {
				t.Fatalf("server sent op %d %v, want close %d", op, payload, tc.code)
			}
			select {
			case err := <-done:
				if got := closeCode(t, err); got != tc.code {
					t.Fatalf("readLoop code %d, want %d", got, tc.code)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("readLoop did not end")
			}
		})
	}
}

func TestOversizedLengthIsRefusedBeforeReading(t *testing.T) {
	c, client, r := pipeConn(t)
	done := readResult(c)
	// Claims 1 GiB; only the header is sent. The server must not wait for the body.
	hdr := []byte{0x81, 0x80 | 127, 0, 0, 0, 0, 0x40, 0, 0, 0, 1, 2, 3, 4}
	go func() { _, _ = client.Write(hdr) }()
	if op, _ := serverFrame(t, r); op != opClose {
		t.Fatalf("op %d", op)
	}
	if code := closeCode(t, <-done); code != closeTooBig {
		t.Fatalf("code %d", code)
	}
}

func TestPingPongFragmentsAndClose(t *testing.T) {
	c, client, r := pipeConn(t)
	var got [][]byte
	done := make(chan error, 1)
	msgs := make(chan []byte, 4)
	go func() {
		done <- c.readLoop(func(p []byte) { msgs <- append([]byte{}, p...) })
	}()
	go func() {
		_, _ = client.Write(clientFrame(true, opPing, []byte("abc"), testMask))
	}()
	if op, p := serverFrame(t, r); op != opPong || string(p) != "abc" {
		t.Fatalf("ping answered with op %d %q", op, p)
	}
	// A fragmented text message with a ping in the middle, then a pong (ignored).
	go func() {
		_, _ = client.Write(clientFrame(false, opText, []byte("hel"), testMask))
		_, _ = client.Write(clientFrame(true, opPing, nil, testMask))
		_, _ = client.Write(clientFrame(true, opContinuation, []byte("lo"), testMask))
		_, _ = client.Write(clientFrame(true, opPong, []byte("x"), testMask))
	}()
	if op, _ := serverFrame(t, r); op != opPong {
		t.Fatalf("inner ping answered with %d", op)
	}
	got = append(got, <-msgs)
	if string(got[0]) != "hello" {
		t.Fatalf("message %q", got[0])
	}
	go func() {
		payload := []byte{0x03, 0xe8, 'b', 'y', 'e'}
		_, _ = client.Write(clientFrame(true, opClose, payload, testMask))
	}()
	if op, p := serverFrame(t, r); op != opClose || binary.BigEndian.Uint16(p) != closeNormal {
		t.Fatalf("close answered with op %d %v", op, p)
	}
	if code := closeCode(t, <-done); code != closeNormal {
		t.Fatalf("code %d", code)
	}
}

func TestReadDeadlineEndsASilentClient(t *testing.T) {
	c, _, _ := pipeConn(t)
	c.readTimeout = 50 * time.Millisecond
	select {
	case err := <-readResult(c):
		if err == nil || !strings.Contains(err.Error(), "deadline") && !errors.Is(err, io.EOF) {
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Timeout() {
				t.Fatalf("err = %v, want a timeout", err)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a silent client kept the reader alive")
	}
}
