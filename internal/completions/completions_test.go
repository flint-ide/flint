package completions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckEndpoint(t *testing.T) {
	ok := []string{
		"http://localhost:11434",
		"http://127.0.0.1:8080/",
		"http://[::1]:1234",
		"https://api.example.com",
		"https://api.example.com/v1",
		"https://example.com:8443/openai/v1/",
	}
	for _, e := range ok {
		if err := CheckEndpoint(e); err != nil {
			t.Errorf("CheckEndpoint(%q) = %v, want nil", e, err)
		}
	}
	bad := map[string]string{
		"":                                 "empty",
		"localhost:11434":                  "http",
		"ftp://localhost":                  "http",
		"http://example.com":               "https",
		"http://192.168.1.10:11434":        "https",
		"http://localhost.example.com":     "https",
		"http://127.0.0.2":                 "https",
		"https://user:pw@example.com":      "credentials",
		"https://example.com/?key=x":       "query",
		"https://example.com/#x":           "query",
		"https://":                         "host",
		"https://exa mple.com":             "invalid",
		"https://example.com/" + long(300): "long",
	}
	for e, want := range bad {
		if err := CheckEndpoint(e); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckEndpoint(%q) = %v, want containing %q", e, err, want)
		}
	}
}

func long(n int) string { return strings.Repeat("a", n) }

func TestIsLocal(t *testing.T) {
	for e, want := range map[string]bool{
		"http://localhost:11434": true, "http://127.0.0.1": true, "http://[::1]:1": true,
		"https://localhost": true, "https://example.com": false, "http://10.0.0.1": false,
	} {
		if got := IsLocal(e); got != want {
			t.Errorf("IsLocal(%q) = %v, want %v", e, got, want)
		}
	}
}

func TestOrigin(t *testing.T) {
	for e, want := range map[string]string{
		"https://API.example.com/v1":   "https://api.example.com",
		"https://api.example.com:8443": "https://api.example.com:8443",
		"http://localhost:11434/":      "http://localhost:11434",
	} {
		if got, err := Origin(e); err != nil || got != want {
			t.Errorf("Origin(%q) = %q, %v; want %q", e, got, err, want)
		}
	}
	if _, err := Origin("http://example.com"); err == nil {
		t.Error("Origin accepted an invalid endpoint")
	}
}

func TestCheckModel(t *testing.T) {
	for _, m := range []string{"qwen2.5-coder:1.5b-base", "gpt-3.5-turbo-instruct", "org/model@v1"} {
		if err := CheckModel(m); err != nil {
			t.Errorf("CheckModel(%q) = %v", m, err)
		}
	}
	for _, m := range []string{"", "a b", "x\n", long(201)} {
		if err := CheckModel(m); err == nil {
			t.Errorf("CheckModel(%q) accepted", m)
		}
	}
}

// decode reads the JSON body a fake server got.
func decode(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("request body: %v", err)
	}
	return body
}

func TestOllamaRequest(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		got = decode(t, r)
		if r.Header.Get("Authorization") != "" {
			t.Error("Ollama got an Authorization header without an API key")
		}
		_, _ = io.WriteString(w, `{"model":"m","response":"return a + b\n\n","done":true}`)
	}))
	defer srv.Close()

	c := New()
	cfg := Config{Provider: Ollama, Endpoint: srv.URL + "/", Model: "qwen", MaxTokens: 32}
	text, err := c.Complete(context.Background(), cfg, "", Request{Prefix: "func add(a, b int) int {\n\t", Suffix: "\n}\n"})
	if err != nil {
		t.Fatal(err)
	}
	if text != "return a + b" {
		t.Errorf("text = %q", text)
	}
	if path != "/api/generate" {
		t.Errorf("path = %s", path)
	}
	if got["model"] != "qwen" || got["prompt"] != "func add(a, b int) int {\n\t" || got["suffix"] != "\n}\n" || got["stream"] != false {
		t.Errorf("body = %v", got)
	}
	opts, _ := got["options"].(map[string]any)
	if opts["num_predict"] != float64(32) {
		t.Errorf("options = %v", opts)
	}
}

func TestOllamaEmptySuffixStillFillsInTheMiddle(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decode(t, r)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer srv.Close()
	if _, err := New().Complete(context.Background(), Config{Provider: Ollama, Endpoint: srv.URL, Model: "m", MaxTokens: 8}, "", Request{Prefix: "a"}); err != nil {
		t.Fatal(err)
	}
	// Without a suffix Ollama would use the model's chat template.
	if got["suffix"] == "" || got["suffix"] == nil {
		t.Errorf("suffix = %v, want a non-empty one", got["suffix"])
	}
}

func TestOpenAIRequest(t *testing.T) {
	for _, tc := range []struct{ endpoint, wantPath string }{
		{"", "/v1/completions"},
		{"/v1", "/v1/completions"},
		{"/openai/v1/", "/openai/v1/completions"},
		{"/proxy", "/proxy/v1/completions"},
	} {
		var got map[string]any
		var path, auth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, auth = r.URL.Path, r.Header.Get("Authorization")
			got = decode(t, r)
			_, _ = io.WriteString(w, `{"choices":[{"text":"x := 1","index":0}]}`)
		}))
		cfg := Config{Provider: OpenAI, Endpoint: srv.URL + tc.endpoint, Model: "m", MaxTokens: 16}
		text, err := New().Complete(context.Background(), cfg, "sk-test", Request{Prefix: "p", Suffix: "s"})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if text != "x := 1" || path != tc.wantPath || auth != "Bearer sk-test" {
			t.Errorf("%s: text %q path %s auth %q", tc.endpoint, text, path, auth)
		}
		if got["prompt"] != "p" || got["suffix"] != "s" || got["max_tokens"] != float64(16) || got["model"] != "m" {
			t.Errorf("body = %v", got)
		}
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		status   int
		body     string
		want     string
	}{
		{"ollama model missing", Ollama, 404, `{"error":"model \"qwen\" not found, try pulling it first"}`, "ollama pull qwen"},
		{"openai error object", OpenAI, 401, `{"error":{"message":"Incorrect API key provided"}}`, "Incorrect API key"},
		{"openai error string", OpenAI, 500, `{"error":"boom"}`, "boom"},
		{"plain status", OpenAI, 502, `<html>bad gateway</html>`, "502"},
		{"no choices", OpenAI, 200, `{"choices":[]}`, ""},
		{"bad json", Ollama, 200, `not json`, "invalid response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			text, err := New().Complete(context.Background(), Config{Provider: tt.provider, Endpoint: srv.URL, Model: "qwen", MaxTokens: 8}, "", Request{Prefix: "a"})
			if tt.want == "" {
				if err != nil || text != "" {
					t.Errorf("got %q, %v; want no suggestion and no error", text, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestResponseSizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"response":"`+strings.Repeat("a", maxResponseBytes+10)+`"}`)
	}))
	defer srv.Close()
	_, err := New().Complete(context.Background(), Config{Provider: Ollama, Endpoint: srv.URL, Model: "m", MaxTokens: 8}, "", Request{Prefix: "a"})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want too large", err)
	}
}

func TestNotRunning(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // nothing listens there now
	_, err = New().Complete(context.Background(), Config{Provider: Ollama, Endpoint: "http://" + addr, Model: "m", MaxTokens: 8}, "", Request{Prefix: "a"})
	if err == nil || !strings.Contains(err.Error(), "Ollama is not running") {
		t.Errorf("err = %v, want Ollama is not running", err)
	}
}

func TestRefusesInvalidRequests(t *testing.T) {
	c := New()
	cfg := Config{Provider: Ollama, Endpoint: "http://example.com", Model: "m", MaxTokens: 8}
	if _, err := c.Complete(context.Background(), cfg, "", Request{Prefix: "a"}); err == nil {
		t.Error("plain http to a remote host accepted")
	}
	cfg.Endpoint, cfg.Provider = "http://localhost:1", "gemini"
	if _, err := c.Complete(context.Background(), cfg, "", Request{Prefix: "a"}); err == nil {
		t.Error("unknown provider accepted")
	}
	cfg.Provider = Ollama
	if _, err := c.Complete(context.Background(), cfg, "", Request{Prefix: long(MaxContextBytes + 1)}); err == nil {
		t.Error("oversized prefix accepted")
	}
}

func TestCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := New().Complete(ctx, Config{Provider: Ollama, Endpoint: srv.URL, Model: "m", MaxTokens: 8}, "", Request{Prefix: "a"})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Complete did not return after cancel")
	}
}

func TestLatestCancelsThePreviousRequestOfAnEditor(t *testing.T) {
	var l Latest
	a, doneA := l.Start(context.Background(), "editor-1")
	b, doneB := l.Start(context.Background(), "editor-2")
	if a.Err() != nil || b.Err() != nil {
		t.Fatal("fresh contexts are cancelled")
	}
	c, doneC := l.Start(context.Background(), "editor-1")
	if !errors.Is(a.Err(), context.Canceled) {
		t.Error("the earlier request of editor-1 was not cancelled")
	}
	if b.Err() != nil || c.Err() != nil {
		t.Error("another editor's or the new request was cancelled")
	}
	doneA() // finishing a superseded request leaves the new one alone
	if c.Err() != nil {
		t.Error("done of a superseded request cancelled the new one")
	}
	doneB()
	doneC()
	if n := l.Len(); n != 0 {
		t.Errorf("%d requests still tracked", n)
	}
	if !errors.Is(c.Err(), context.Canceled) {
		t.Error("done does not release the context")
	}
}

func TestRedactsForRemoteEndpoints(t *testing.T) {
	var got map[string]any
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got = decode(t, r)
		_, _ = io.WriteString(w, `{"choices":[{"text":"x"}]}`)
	}))
	defer srv.Close()
	// example.com is in httptest's certificate; dial the test server for it.
	transport := srv.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	c := &Client{HTTP: &http.Client{Transport: transport}}
	secret := "token = ghp_abcdefghijklmnopqrstuvwxyz0123"
	cfg := Config{Provider: OpenAI, Endpoint: "https://example.com", Model: "m", MaxTokens: 8}
	if _, err := c.Complete(context.Background(), cfg, "", Request{Prefix: secret, Suffix: secret}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got["prompt"].(string), "ghp_") || strings.Contains(got["suffix"].(string), "ghp_") {
		t.Errorf("secret sent to a remote endpoint: %v", got)
	}

	// A local server gets the text as it is.
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decode(t, r)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer local.Close()
	if _, err := New().Complete(context.Background(), Config{Provider: Ollama, Endpoint: local.URL, Model: "m", MaxTokens: 8}, "", Request{Prefix: secret}); err != nil {
		t.Fatal(err)
	}
	if got["prompt"] != secret {
		t.Errorf("local prompt = %v", got["prompt"])
	}
}

func TestSecretFilesAreNotSent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer srv.Close()
	cfg := Config{Provider: Ollama, Endpoint: srv.URL, Model: "m", MaxTokens: 8}
	for _, p := range []string{"/p/.env", "/p/certs/tls.key", "/u/.kube/config"} {
		if text, err := New().Complete(context.Background(), cfg, "", Request{Path: p, Prefix: "A="}); text != "" || err != nil {
			t.Errorf("%s: %q, %v", p, text, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("%d requests for secret files", calls.Load())
	}
	if text, _ := New().Complete(context.Background(), cfg, "", Request{Path: "/p/main.go", Prefix: "a"}); text != "x" {
		t.Errorf("ordinary file: %q", text)
	}
}

func TestRedactsKeyNamesForRemoteEndpoints(t *testing.T) {
	var got map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decode(t, r)
		_, _ = io.WriteString(w, `{"choices":[{"text":"x"}]}`)
	}))
	defer srv.Close()
	transport := srv.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	c := &Client{HTTP: &http.Client{Transport: transport}}
	prefix := "DB_PASSWORD=hunter2\nconst dbPassword = \"hunter2\"\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow"
	cfg := Config{Provider: OpenAI, Endpoint: "https://example.com", Model: "m", MaxTokens: 8}
	if _, err := c.Complete(context.Background(), cfg, "", Request{Path: "/p/db.ts", Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	if p := got["prompt"].(string); strings.Contains(p, "hunter2") || strings.Contains(p, "MIIEow") {
		t.Errorf("secret sent to a remote endpoint: %q", p)
	}
}

func TestDoesNotFollowRedirects(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err := New().Complete(context.Background(), Config{Provider: OpenAI, Endpoint: srv.URL, Model: "m", MaxTokens: 8}, "sk-x", Request{Prefix: "a"})
	if err == nil || hits.Load() != 0 {
		t.Errorf("redirect followed (err %v, %d hits)", err, hits.Load())
	}
}
