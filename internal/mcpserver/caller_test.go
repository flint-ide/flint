package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

// callerTool answers with the caller id its context carries.
func callerTool() Tool {
	return Tool{
		Name: "who", Title: "Who",
		Call: func(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
			return "caller=" + CallerFrom(ctx), nil
		},
	}
}

const callWho = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"who","arguments":{}}}`

func callerText(t *testing.T, r rpcReply) string {
	t.Helper()
	var res struct {
		Content []struct{ Text string } `json:"content"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil || len(res.Content) != 1 {
		t.Fatalf("result %s: %v", r.Result, err)
	}
	return res.Content[0].Text
}

func TestCallerIdentity(t *testing.T) {
	s := newTestServer(t, Options{Tools: func() []Tool { return []Tool{callerTool()} }})
	var id atomic.Value
	id.Store("")
	url, token, revoke, err := s.Endpoint("/p", func() string { return id.Load().(string) })
	if err != nil {
		t.Fatal(err)
	}
	url2, anon, _, _ := s.Endpoint("/p", nil)

	for _, tc := range []struct {
		name, url, token, set, want string
	}{
		{"id not known yet", url, token, "", "caller="},
		{"id filled in after the token was issued", url, token, "chat-7", "caller=chat-7"},
		{"token without a resolver", url2, anon, "chat-7", "caller="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id.Store(tc.set)
			if got := callerText(t, call(t, tc.url, tc.token, callWho)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	revoke()
	resp, _ := post(t, url, token, callWho, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", resp.StatusCode)
	}
	if got := callerText(t, call(t, url2, anon, callWho)); got != "caller=" {
		t.Errorf("other token affected by the revoke: %q", got)
	}
}

func TestCallerFromEmptyContext(t *testing.T) {
	if got := CallerFrom(context.Background()); got != "" {
		t.Fatalf("CallerFrom = %q", got)
	}
}
