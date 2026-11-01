package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func echoTool() Tool {
	return Tool{
		Name:        "echo",
		Description: "Echoes the root and the text argument.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Call: func(_ context.Context, root string, args json.RawMessage) (string, error) {
			var in struct{ Text string }
			if err := json.Unmarshal(args, &in); err != nil {
				return "", err
			}
			if in.Text == "fail" {
				return "", errors.New("it failed")
			}
			return root + ":" + in.Text, nil
		},
	}
}

func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	if opts.Tools == nil {
		opts.Tools = func() []Tool { return []Tool{echoTool()} }
	}
	s := New(opts)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

type rpcReply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func post(t *testing.T, url, token, body string, header map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		if k == "Host" {
			req.Host = v // the client sends req.Host, not the header
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func call(t *testing.T, url, token, body string) rpcReply {
	t.Helper()
	resp, data := post(t, url, token, body, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q", ct)
	}
	var r rpcReply
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return r
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	s := newTestServer(t, Options{Version: "1.2.3"})
	url, token, _, err := s.Endpoint("/p")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2099-01-01", ProtocolVersion},
	} {
		r := call(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+tc.asked+`","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
		if r.Error != nil {
			t.Fatalf("error %+v", r.Error)
		}
		var res struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools *struct{} `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct{ Name, Version string } `json:"serverInfo"`
		}
		if err := json.Unmarshal(r.Result, &res); err != nil {
			t.Fatal(err)
		}
		if res.ProtocolVersion != tc.want || res.Capabilities.Tools == nil || res.ServerInfo.Name != "flint" || res.ServerInfo.Version != "1.2.3" {
			t.Fatalf("asked %s: %+v", tc.asked, res)
		}
		if string(r.ID) != "1" {
			t.Fatalf("id %s", r.ID)
		}
	}
}

func TestToolsListAndCall(t *testing.T) {
	s := newTestServer(t, Options{})
	url, token, _, err := s.Endpoint("/proj")
	if err != nil {
		t.Fatal(err)
	}
	r := call(t, url, token, `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
	var list struct {
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
			Annotations struct {
				ReadOnly bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &list); err != nil || len(list.Tools) != 1 {
		t.Fatalf("list %s %v", r.Result, err)
	}
	if list.Tools[0].Name != "echo" || !list.Tools[0].Annotations.ReadOnly || !bytes.Contains(list.Tools[0].InputSchema, []byte(`"object"`)) {
		t.Fatalf("tool %+v", list.Tools[0])
	}

	r = call(t, url, token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
	if text, isErr := toolText(t, r); text != "/proj:hi" || isErr {
		t.Fatalf("call: %q %v", text, isErr)
	}
	r = call(t, url, token, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"fail"}}}`)
	if text, isErr := toolText(t, r); text != "it failed" || !isErr {
		t.Fatalf("failing call: %q %v", text, isErr)
	}
	r = call(t, url, token, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`)
	if r.Error == nil || r.Error.Code != codeInvalidParams {
		t.Fatalf("unknown tool: %+v", r)
	}
	r = call(t, url, token, `{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	if r.Error == nil || r.Error.Code != codeMethodNotFound {
		t.Fatalf("unknown method: %+v", r)
	}
	r = call(t, url, token, `{"jsonrpc":"2.0","id":6,"method":"ping"}`)
	if r.Error != nil || string(r.Result) != "{}" {
		t.Fatalf("ping: %+v %s", r.Error, r.Result)
	}
}

func toolText(t *testing.T, r rpcReply) (string, bool) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("rpc error %+v", r.Error)
	}
	var res struct {
		Content []struct{ Type, Text string } `json:"content"`
		IsError bool                          `json:"isError"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil || len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("result %s: %v", r.Result, err)
	}
	return res.Content[0].Text, res.IsError
}

func TestToolOutputIsRedactedAndBounded(t *testing.T) {
	big := strings.Repeat("é", maxToolOutput) // two bytes each
	s := newTestServer(t, Options{Tools: func() []Tool {
		return []Tool{
			{Name: "secret", Call: func(context.Context, string, json.RawMessage) (string, error) {
				return "key AKIAABCDEFGHIJKLMNOP here", nil
			}},
			{Name: "big", Call: func(context.Context, string, json.RawMessage) (string, error) { return big, nil }},
		}
	}})
	url, token, _, _ := s.Endpoint("/p")
	text, _ := toolText(t, call(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"secret"}}`))
	if strings.Contains(text, "AKIA") {
		t.Fatalf("not redacted: %q", text)
	}
	text, _ = toolText(t, call(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"big"}}`))
	if len(text) > maxToolOutput+100 || !strings.HasSuffix(text, truncatedNote) || !json.Valid([]byte(`"`+strings.TrimSuffix(text, truncatedNote)+`"`)) {
		t.Fatalf("not bounded: %d bytes", len(text))
	}
}

func TestNotificationsAndResponsesAreAccepted(t *testing.T) {
	s := newTestServer(t, Options{})
	url, token, _, _ := s.Endpoint("/p")
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":7,"result":{}}`,
	} {
		resp, data := post(t, url, token, body, nil)
		if resp.StatusCode != http.StatusAccepted || len(data) != 0 {
			t.Fatalf("%s: %d %s", body, resp.StatusCode, data)
		}
	}
}

func TestMalformedRequests(t *testing.T) {
	s := newTestServer(t, Options{})
	url, token, _, _ := s.Endpoint("/p")
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{not json`, codeParseError},
		{`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, codeInvalidRequest},
		{`{"jsonrpc":"1.0","id":1,"method":"ping"}`, codeInvalidRequest},
		{`{"jsonrpc":"2.0","id":{"x":1},"method":"ping"}`, codeInvalidRequest},
		{`{"jsonrpc":"2.0","id":null,"method":"ping"}`, codeInvalidRequest},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[1]}`, codeInvalidParams},
	} {
		resp, data := post(t, url, token, tc.body, nil)
		var r rpcReply
		_ = json.Unmarshal(data, &r)
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusOK || r.Error == nil || r.Error.Code != tc.code {
			t.Errorf("%s: %d %s", tc.body, resp.StatusCode, data)
		}
	}
}

func TestAuthAndTransportChecks(t *testing.T) {
	allowed := atomic.Bool{}
	allowed.Store(true)
	s := newTestServer(t, Options{Allow: func(root string) bool { return allowed.Load() && root == "/p" }})
	url, token, _, _ := s.Endpoint("/p")
	_, otherToken, _, _ := s.Endpoint("/other")
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	port := strings.TrimSuffix(strings.TrimPrefix(url, "http://127.0.0.1:"), "/mcp")

	cases := []struct {
		name   string
		token  string
		header map[string]string
		want   int
	}{
		{"ok", token, nil, http.StatusOK},
		{"localhost host", token, map[string]string{"Host": "localhost:" + port}, http.StatusOK},
		{"no token", "", nil, http.StatusUnauthorized},
		{"wrong token", strings.Repeat("a", len(token)), nil, http.StatusUnauthorized},
		{"other project not allowed", otherToken, nil, http.StatusForbidden},
		{"rebinding host", token, map[string]string{"Host": "evil.example:" + port}, http.StatusForbidden},
		{"foreign origin", token, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"own origin", token, map[string]string{"Origin": "http://127.0.0.1:" + port}, http.StatusOK},
		{"bad version", token, map[string]string{"MCP-Protocol-Version": "1999-01-01"}, http.StatusBadRequest},
		{"good version", token, map[string]string{"MCP-Protocol-Version": "2025-06-18"}, http.StatusOK},
		{"wrong content type", token, map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
	}
	for _, tc := range cases {
		resp, data := post(t, url, tc.token, ping, tc.header)
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d (%s), want %d", tc.name, resp.StatusCode, data, tc.want)
		}
		if tc.want == http.StatusUnauthorized && !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: no WWW-Authenticate", tc.name)
		}
	}

	allowed.Store(false) // e.g. the server was switched off in Settings
	if resp, _ := post(t, url, token, ping, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("disallowed project: %d", resp.StatusCode)
	}
	allowed.Store(true)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", resp.StatusCode)
	}
	if resp, _ := post(t, strings.TrimSuffix(url, "/mcp")+"/other", token, ping, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("other path: %d", resp.StatusCode)
	}
	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", maxBody) + `"}}`
	if resp, _ := post(t, url, token, huge, nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("huge body: %d", resp.StatusCode)
	}
}

const pingBody = `{"jsonrpc":"2.0","id":1,"method":"ping"}`

// Every agent session gets its own token, which works until it is revoked.
func TestTokensArePerSessionAndRevocable(t *testing.T) {
	s := newTestServer(t, Options{})
	u1, t1, revoke1, err := s.Endpoint("/a/b/../b")
	if err != nil {
		t.Fatal(err)
	}
	u2, t2, revoke2, _ := s.Endpoint("/a/b")
	if u1 != u2 || t1 == t2 || len(t1) < 40 || s.Live() != 2 {
		t.Fatalf("tokens %q %q, %d live", t1, t2, s.Live())
	}
	if !strings.HasPrefix(u1, "http://127.0.0.1:") || !strings.HasSuffix(u1, "/mcp") {
		t.Fatalf("url %q", u1)
	}
	revoke1()
	revoke1() // twice is fine
	if resp, _ := post(t, u1, t1, pingBody, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", resp.StatusCode)
	}
	if resp, _ := post(t, u2, t2, pingBody, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("other session's token: %d", resp.StatusCode)
	}
	revoke2()
	if s.Live() != 0 {
		t.Fatalf("%d tokens live", s.Live())
	}
	if _, _, _, err := s.Endpoint("relative"); err == nil {
		t.Fatal("relative root accepted")
	}
}

// Closing a project revokes its sessions' tokens, and only those.
func TestRevokeRoot(t *testing.T) {
	s := newTestServer(t, Options{})
	url, ta, _, _ := s.Endpoint("/a")
	_, tb, _, _ := s.Endpoint("/b")
	s.RevokeRoot("/a/")
	if resp, _ := post(t, url, ta, pingBody, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("closed project's token: %d", resp.StatusCode)
	}
	if resp, _ := post(t, url, tb, pingBody, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("open project's token: %d", resp.StatusCode)
	}
}

// Stop (the server switched off) closes the port and revokes every token;
// the next Endpoint listens again.
func TestStopAndListenAgain(t *testing.T) {
	s := newTestServer(t, Options{})
	url, token, _, _ := s.Endpoint("/p")
	s.Stop()
	if s.Listening() || s.Live() != 0 {
		t.Fatal("still listening after Stop")
	}
	client := http.Client{Timeout: time.Second}
	if resp, err := client.Post(url, "application/json", strings.NewReader("{}")); err == nil {
		resp.Body.Close()
		t.Fatal("old port still answers")
	}
	url2, token2, _, err := s.Endpoint("/p")
	if err != nil || token2 == token {
		t.Fatalf("restart: %v", err)
	}
	if resp, _ := post(t, url2, token2, pingBody, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("after restart: %d", resp.StatusCode)
	}
}

// The concurrency limit is per project: a project with all its slots busy
// doesn't block another.
func TestConcurrencyIsLimitedPerProject(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, maxConcurrent)
	s := newTestServer(t, Options{Tools: func() []Tool {
		return []Tool{{Name: "block", Call: func(ctx context.Context, root string, _ json.RawMessage) (string, error) {
			if root == "/busy" {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return "ok", nil
		}}}
	}})
	url, busy, _, _ := s.Endpoint("/busy")
	_, other, _, _ := s.Endpoint("/other")
	callBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"block"}}`
	done := make(chan struct{})
	for range maxConcurrent {
		go func() {
			post(t, url, busy, callBody, nil)
			done <- struct{}{}
		}()
	}
	for range maxConcurrent {
		<-started
	}
	if text, _ := toolText(t, call(t, url, other, callBody)); text != "ok" {
		t.Fatalf("other project blocked: %q", text)
	}
	close(release)
	for range maxConcurrent {
		<-done
	}
}

func TestCloseStopsListenerAndRefusesEndpoints(t *testing.T) {
	s := New(Options{Tools: func() []Tool { return nil }})
	url, _, _, err := s.Endpoint("/p")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	client := http.Client{Timeout: time.Second}
	if resp, err := client.Post(url, "application/json", strings.NewReader("{}")); err == nil {
		resp.Body.Close()
		t.Fatal("server still answers after Close")
	}
	if _, _, _, err := s.Endpoint("/p"); !errors.Is(err, ErrClosed) {
		t.Fatalf("endpoint after close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	// Close without a listener is fine too.
	if err := New(Options{}).Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolCallsAreCancelledOnClose(t *testing.T) {
	started := make(chan struct{})
	s := New(Options{Tools: func() []Tool {
		return []Tool{{Name: "block", Call: func(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		}}}
	}})
	url, token, _, _ := s.Endpoint("/p")
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"block"}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	closed := make(chan struct{})
	go func() {
		_ = s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hangs on a running tool call")
	}
	<-done
}
