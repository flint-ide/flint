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

// The review's proof of concept: what <img src="http://127.0.0.1:7419/api/pair">
// on any web page sends (a GET with an allowed Host, no Origin, Sec-Fetch-*)
// burned the pairing budget, so the real phone then got 429.
func TestWebPagesCanNotLockOutPairing(t *testing.T) {
	e := newEnv(t, 120)
	for i := range 25 {
		req, _ := http.NewRequest(http.MethodGet, e.base+"/api/pair", nil)
		req.Header.Set("Sec-Fetch-Mode", "no-cors")
		req.Header.Set("Sec-Fetch-Dest", "image")
		resp, err := e.hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("browser request %d: %d", i, resp.StatusCode)
		}
	}
	// Plain GETs and wrong content types (a form post) are refused uncharged.
	for range 25 {
		if got := e.request(t, http.MethodGet, "/api/pair", nil, ""); got != http.StatusMethodNotAllowed {
			t.Fatalf("GET pair: %d", got)
		}
		if got := e.request(t, http.MethodPost, "/api/pair", map[string]string{"Content-Type": "text/plain"}, "x"); got != http.StatusUnsupportedMediaType {
			t.Fatalf("text pair: %d", got)
		}
	}
	p, _ := e.devices.BeginPairing()
	if _, _, err := client.Pair(context.Background(), e.hc, e.base, p.Secret, "phone"); err != nil {
		t.Fatalf("the phone can't pair after a web page tried: %v", err)
	}
	// Every browser-marked request is refused, with a token too.
	for _, h := range []map[string]string{{"Origin": "http://127.0.0.1:7419"}, {"Sec-Fetch-Site": "cross-site"}, {"Origin": "null"}} {
		if got := e.request(t, http.MethodGet, "/api/chats", h, ""); got != http.StatusForbidden {
			t.Fatalf("%v: %d", h, got)
		}
	}
}

func TestPairingAttemptsHaveTheirOwnBudget(t *testing.T) {
	e := newEnv(t, 120)
	got := map[int]int{}
	for range 15 {
		got[e.request(t, http.MethodPost, "/api/pair", map[string]string{"Content-Type": "application/json"}, `{"secret":"AAAAAAAAAAAAAAAA","name":"x"}`)]++
	}
	if got[http.StatusTooManyRequests] == 0 || got[http.StatusUnauthorized] == 0 {
		t.Fatalf("pairing attempts: %v", got)
	}
	// Bad tokens are a separate budget: still 401, not 429.
	if s := e.request(t, http.MethodGet, "/api/chats", map[string]string{"Authorization": "Bearer 0123456789abcdef.x"}, ""); s != http.StatusUnauthorized {
		t.Fatalf("bad token after pairing attempts: %d", s)
	}
}

func TestRequestsWithoutATokenCostNothingAndFloodNoLog(t *testing.T) {
	e := newEnv(t, 120)
	for i := range 100 {
		if got := e.request(t, http.MethodGet, "/api/chats", nil, ""); got != http.StatusUnauthorized {
			t.Fatalf("request %d without a token: %d", i, got)
		}
		_ = e.request(t, http.MethodGet, "/api/chats", map[string]string{"Host": "evil.example"}, "")
	}
	lines := strings.Count(e.logs.all(), "op=chats.list status=")
	if lines > 15 {
		t.Fatalf("%d audit lines for 200 refused requests", lines)
	}
	// Paired devices are still logged in full.
	c := e.pair(t, "phone")
	if _, err := c.Chats(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.logs.all(), `("phone") op=chats.list status=200`) {
		t.Fatal("a device's request was not logged")
	}
}

// A device's budget is charged before its token is checked, so wrong
// secrets for a device can't make Flint compare hashes or read the Keychain
// without limit.
func TestRateLimitComesBeforeAuthentication(t *testing.T) {
	e := newEnv(t, 10) // burst 5
	c := e.pair(t, "phone")
	for range 5 {
		if _, err := c.Chats(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	id := strings.SplitN(c.Token, ".", 2)[0]
	if got := e.request(t, http.MethodGet, "/api/chats", map[string]string{"Authorization": "Bearer " + id + ".wrong"}, ""); got != http.StatusTooManyRequests {
		t.Fatalf("wrong secret for a device over its budget: %d, want 429", got)
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
