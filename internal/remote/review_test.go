package remote_test

// Regression tests for the security review of phase 1.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/remote"
	"flint/internal/remote/client"
)

// Revoking while a stream is being opened must never leave the stream open:
// the request may authenticate before the revocation and register its stream
// after the cut.
func TestRevokeRaceLeavesNoStreamOpen(t *testing.T) {
	e := newEnv(t, 6000)
	for i := range 200 {
		p, _ := e.devices.BeginPairing()
		dev, token, err := e.devices.Pair(p.Secret, "x")
		if err != nil {
			t.Fatal(err)
		}
		c := &client.Client{Base: e.base, Token: token, HTTP: e.hc}
		done := make(chan struct{})
		go func() {
			time.Sleep(time.Duration(rand.IntN(1500)) * time.Microsecond)
			_ = e.srv.Revoke(dev.ID)
			close(done)
		}()
		s, err := c.Stream(context.Background(), nil)
		<-done
		if err != nil {
			if code := statusOf(err); code != http.StatusUnauthorized && code != http.StatusTooManyRequests {
				t.Fatalf("round %d: stream refused with %v, want 401 (or 429 once bad tokens add up)", i, err)
			}
			continue
		}
		_ = s.SetDeadline(time.Now().Add(2 * time.Second))
		for {
			_, err := s.Next()
			if err == nil {
				continue
			}
			var ce *client.CloseError
			if !errors.As(err, &ce) || ce.Code != 1008 {
				t.Fatalf("round %d: stream outlived the revocation of its device: %v", i, err)
			}
			break
		}
		_ = s.Close()
	}
	waitFor(t, func() bool { return e.srv.Streams() == 0 })
}

func TestRevokeAllRaceLeavesNoStreamOpen(t *testing.T) {
	e := newEnv(t, 6000)
	for i := range 100 {
		p, _ := e.devices.BeginPairing()
		_, token, err := e.devices.Pair(p.Secret, "x")
		if err != nil {
			t.Fatal(err)
		}
		c := &client.Client{Base: e.base, Token: token, HTTP: e.hc}
		done := make(chan struct{})
		go func() {
			time.Sleep(time.Duration(rand.IntN(1500)) * time.Microsecond)
			_ = e.srv.RevokeAll()
			close(done)
		}()
		s, err := c.Stream(context.Background(), nil)
		<-done
		if err != nil {
			continue
		}
		_ = s.SetDeadline(time.Now().Add(2 * time.Second))
		for {
			if _, err := s.Next(); err != nil {
				var ce *client.CloseError
				if !errors.As(err, &ce) || ce.Code != 1008 {
					t.Fatalf("round %d: stream outlived RevokeAll: %v", i, err)
				}
				break
			}
		}
		_ = s.Close()
	}
}

// A client reconnecting after a call's start (since) still gets its update
// from a credentials file withheld: the stream reads the start from the log.
func TestSecretFileOutputIsWithheldAfterAReconnect(t *testing.T) {
	e := newEnv(t, 600)
	e.world.chats = []remote.Chat{{ID: "c1"}}
	e.world.events["c1"] = []as.Event{
		{SessionID: "c1", Seq: 1, Kind: as.KindToolCall, ToolCall: &as.ToolCall{ID: "t1", Kind: as.ToolRead, Paths: []string{"/p/.env"}}},
		{SessionID: "c1", Seq: 2, Kind: as.KindToolCallUpdate, ToolCall: &as.ToolCall{ID: "t1", Output: "DB_PASS=hunter2"}},
	}
	c := e.pair(t, "phone")
	s, err := c.Stream(context.Background(), map[string]uint64{"c1": 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var got []as.Event
	for _, m := range collect(t, s) {
		if m.Type == "agent" {
			got = append(got, *m.Event)
		}
	}
	if len(got) != 1 || got[0].Seq != 2 || strings.Contains(got[0].ToolCall.Output, "hunter2") {
		t.Fatalf("replayed %+v", got)
	}
	// Live too.
	e.srv.AgentEvent(as.Event{SessionID: "c1", Seq: 3, Kind: as.KindToolCallUpdate, ToolCall: &as.ToolCall{ID: "t1", Output: "STRIPE=rk_live_51Habcdefghijklmnop"}})
	if m := next(t, s, "agent"); strings.Contains(m.Event.ToolCall.Output, "rk_live") {
		t.Fatalf("live update leaked %q", m.Event.ToolCall.Output)
	}
}

// openRaw opens a stream with a hand-written handshake (a Host the client
// couldn't dial) and returns the connection after the 101.
func openRaw(t *testing.T, e *env, token, host string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", e.srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	req := "GET /api/stream HTTP/1.1\r\nHost: " + host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nAuthorization: Bearer " + token + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %v %v", resp, err)
	}
	return conn, br
}

// readClose reads frames until a close frame and returns its code.
func readClose(t *testing.T, br *bufio.Reader) int {
	t.Helper()
	for {
		var h [2]byte
		if _, err := io.ReadFull(br, h[:]); err != nil {
			t.Fatalf("no close frame: %v", err)
		}
		n := int(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			_, _ = io.ReadFull(br, b[:])
			n = int(b[0])<<8 | int(b[1])
		case 127:
			var b [8]byte
			_, _ = io.ReadFull(br, b[:])
			n = int(b[4])<<24 | int(b[5])<<16 | int(b[6])<<8 | int(b[7])
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(br, p); err != nil {
			t.Fatal(err)
		}
		if h[0]&0x0f == 0x8 {
			return int(p[0])<<8 | int(p[1])
		}
	}
}

func TestRemovingAnAllowedHostCutsItsStreams(t *testing.T) {
	e := newEnv(t, 600, "laptop.tail1234.ts.net")
	c := e.pair(t, "phone")
	conn, br := openRaw(t, e, c.Token, "laptop.tail1234.ts.net")
	defer conn.Close()
	local, err := c.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	collect(t, local)
	// The same bind (port 0 as in newEnv): applied in place, no restart.
	if err := e.srv.Start(remote.Config{Host: "127.0.0.1", Port: 0, RequestsPerMinute: 600}); err != nil {
		t.Fatal(err)
	}
	if code := readClose(t, br); code != 1008 {
		t.Fatalf("close code %d", code)
	}
	// A loopback stream is unaffected.
	e.srv.TasksChanged()
	next(t, local, "tasks")
}

// A client that stops reading must not hold Stop (or Revoke) for the write
// timeout.
func TestStopDoesNotWaitForAClientThatStoppedReading(t *testing.T) {
	e := newEnv(t, 600)
	e.world.chats = []remote.Chat{{ID: "c1"}}
	c := e.pair(t, "phone")
	conn, _ := openRaw(t, e, c.Token, "127.0.0.1")
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(4096)
	}
	big := strings.Repeat("x", 512<<10)
	for i := range 64 { // far more than the socket buffers hold
		e.srv.AgentEvent(as.Event{SessionID: "c1", Seq: uint64(i + 1), Kind: as.KindMessageChunk, Text: big})
	}
	time.Sleep(300 * time.Millisecond) // the stream is now blocked writing
	start := time.Now()
	e.srv.Stop()
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("Stop took %v with a client that doesn't read", d)
	}
}
