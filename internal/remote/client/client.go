// Package client talks to Flint Remote (internal/remote) the way a phone
// does: pair with a one-time secret, list chats and tasks, and follow the
// event stream over WebSocket. It is the reference client for tests and for
// cmd/remotecheck, and the start of the Go side of the mobile app. Standard
// library only.
package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flint/internal/remote"
)

// maxMessage bounds one message from the server (it sends at most 1 MiB).
const maxMessage = 2 << 20

// Client is a paired device.
type Client struct {
	// Base is the server's URL (http://127.0.0.1:7419, https://laptop.tailnet.ts.net).
	Base  string
	Token string
	HTTP  *http.Client
}

// StatusError is a non-2xx answer.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("flint remote: %d %s", e.Status, e.Body) }

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Pair exchanges a pairing secret for a device token.
func Pair(ctx context.Context, hc *http.Client, base, secret, name string) (string, remote.Device, error) {
	body, _ := json.Marshal(map[string]string{"secret": secret, "name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/api/pair", bytes.NewReader(body))
	if err != nil {
		return "", remote.Device{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Token  string        `json:"token"`
		Device remote.Device `json:"device"`
	}
	if err := do(httpClient(hc), req, &out); err != nil {
		return "", remote.Device{}, err
	}
	return out.Token, out.Device, nil
}

func do(hc *http.Client, req *http.Request, out any) error {
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMessage))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return json.Unmarshal(data, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.Base, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	return do(httpClient(c.HTTP), req, out)
}

// Chats lists the open chats.
func (c *Client) Chats(ctx context.Context) ([]remote.Chat, error) {
	var out struct {
		Chats []remote.Chat `json:"chats"`
	}
	err := c.get(ctx, "/api/chats", &out)
	return out.Chats, err
}

// Tasks lists the agent tasks.
func (c *Client) Tasks(ctx context.Context) ([]remote.Task, error) {
	var out struct {
		Tasks []remote.Task `json:"tasks"`
	}
	err := c.get(ctx, "/api/tasks", &out)
	return out.Tasks, err
}

// CloseError is the server's close frame.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string { return fmt.Sprintf("stream closed: %d %s", e.Code, e.Reason) }

// Stream is an open event stream.
type Stream struct {
	conn net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex
}

// SinceQuery encodes the last Seq per chat for ?since=.
func SinceQuery(since map[string]uint64) string {
	parts := make([]string, 0, len(since))
	for id, seq := range since {
		parts = append(parts, id+":"+strconv.FormatUint(seq, 10))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// Stream opens the event stream, replaying each chat after since[chat].
func (c *Client) Stream(ctx context.Context, since map[string]uint64) (*Stream, error) {
	u, err := url.Parse(strings.TrimSuffix(c.Base, "/") + "/api/stream")
	if err != nil {
		return nil, err
	}
	if len(since) > 0 {
		u.RawQuery = url.Values{"since": {SinceQuery(since)}}.Encode()
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	host := net.JoinHostPort(u.Hostname(), port)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		tc := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tc
	}
	var keyb [16]byte
	_, _ = rand.Read(keyb[:])
	key := base64.StdEncoding.EncodeToString(keyb[:])
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\nAuthorization: Bearer " + c.Token + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = conn.Close()
		return nil, &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		_ = conn.Close()
		return nil, errors.New("flint remote: bad Sec-WebSocket-Accept")
	}
	_ = conn.SetDeadline(time.Time{})
	return &Stream{conn: conn, br: br}, nil
}

// Next returns the next message. It answers pings itself and returns a
// *CloseError when the server closes the stream.
func (s *Stream) Next() (remote.StreamMessage, error) {
	var msg []byte
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(s.br, hdr[:]); err != nil {
			return remote.StreamMessage{}, err
		}
		op := hdr[0] & 0x0f
		if hdr[1]&0x80 != 0 {
			return remote.StreamMessage{}, errors.New("flint remote: masked server frame")
		}
		n := uint64(hdr[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(s.br, b[:]); err != nil {
				return remote.StreamMessage{}, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(s.br, b[:]); err != nil {
				return remote.StreamMessage{}, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxMessage || uint64(len(msg))+n > maxMessage {
			return remote.StreamMessage{}, errors.New("flint remote: message too big")
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(s.br, payload); err != nil {
			return remote.StreamMessage{}, err
		}
		switch op {
		case 0x9: // ping
			if err := s.write(0xa, payload); err != nil {
				return remote.StreamMessage{}, err
			}
			continue
		case 0xa:
			continue
		case 0x8:
			ce := &CloseError{Code: 1005}
			if len(payload) >= 2 {
				ce.Code, ce.Reason = int(binary.BigEndian.Uint16(payload)), string(payload[2:])
			}
			_ = s.write(0x8, payload[:min(2, len(payload))])
			return remote.StreamMessage{}, ce
		}
		msg = append(msg, payload...)
		if hdr[0]&0x80 == 0 {
			continue
		}
		var m remote.StreamMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			return remote.StreamMessage{}, err
		}
		return m, nil
	}
}

// write sends a masked frame, as clients must.
func (s *Stream) write(op byte, payload []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	frame := []byte{0x80 | op}
	switch n := len(payload); {
	case n <= 125:
		frame = append(frame, 0x80|byte(n))
	case n <= 0xffff:
		frame = append(frame, 0x80|126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 0x80|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := s.conn.Write(frame)
	return err
}

// SetDeadline bounds the next reads (tests).
func (s *Stream) SetDeadline(t time.Time) error { return s.conn.SetDeadline(t) }

// Close sends a close frame and closes the connection.
func (s *Stream) Close() error {
	_ = s.write(0x8, []byte{0x03, 0xe8})
	return s.conn.Close()
}
