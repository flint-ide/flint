package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"flint/internal/completions"
	"flint/internal/settings"
)

// memSecrets stands in for the Keychain and counts reads.
type memSecrets struct {
	mu    sync.Mutex
	v     map[string]string
	reads int
}

func (m *memSecrets) Get(account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	return m.v[account], nil
}

func (m *memSecrets) Set(account, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.v == nil {
		m.v = map[string]string{}
	}
	m.v[account] = v
	return nil
}

func completionsApp(endpoint string, enabled bool) *CompletionsAPI {
	s := settings.Default()
	s.Completions.Enabled = enabled
	s.Completions.Provider = completions.OpenAI
	s.Completions.Endpoint = endpoint
	return &CompletionsAPI{app: &App{settings: s}, client: completions.New(), secrets: &memSecrets{}}
}

func TestCompletionsComplete(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"choices":[{"text":"return x"}]}`)
	}))
	defer srv.Close()
	api := completionsApp(srv.URL, true)
	if err := api.SetAPIKey("  sk-local  "); err != nil {
		t.Fatal(err)
	}
	if ok, _ := api.HasAPIKey(); !ok {
		t.Error("HasAPIKey = false after SetAPIKey")
	}
	got, err := api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "func f() int {\n\t"})
	if err != nil || got != "return x" {
		t.Fatalf("Complete = %q, %v", got, err)
	}
	if auth != "Bearer sk-local" {
		t.Errorf("Authorization = %q", auth)
	}
	if err := api.SetAPIKey("a\nb"); err == nil {
		t.Error("API key with a newline accepted")
	}
}

func TestCompletionsOffByDefault(t *testing.T) {
	api := completionsApp("http://localhost:1", false)
	if _, err := api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "a"}); err == nil {
		t.Error("Complete ran with completions off")
	}
}

func TestCompletionsNewRequestCancelsTheEditorsPrevious(t *testing.T) {
	started := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	api := completionsApp(srv.URL, true)
	first := make(chan error, 1)
	go func() {
		_, err := api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "a"})
		first <- err
	}()
	<-started
	go func() { _, _ = api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "ab"}) }()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("first request: %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the editor's previous request was not cancelled")
	}
	<-started
}

func TestCompletionsAPIKeyBelongsToItsEndpoint(t *testing.T) {
	var mu sync.Mutex
	auth := map[string]string{}
	handler := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			auth[name] = r.Header.Get("Authorization")
			mu.Unlock()
			_, _ = io.WriteString(w, `{"choices":[{"text":"x"}]}`)
		}
	}
	a := httptest.NewServer(handler("a"))
	defer a.Close()
	b := httptest.NewServer(handler("b"))
	defer b.Close()

	api := completionsApp(a.URL, true)
	if err := api.SetAPIKey("sk-for-a"); err != nil {
		t.Fatal(err)
	}
	// Another endpoint (another origin) doesn't get a's key.
	api.app.settings.Completions.Endpoint = b.URL + "/v1"
	if ok, _ := api.HasAPIKey(); ok {
		t.Error("HasAPIKey = true for an endpoint without a key")
	}
	if _, err := api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "p"}); err != nil {
		t.Fatal(err)
	}
	api.app.settings.Completions.Endpoint = a.URL + "/"
	if _, err := api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "p"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if auth["b"] != "" || auth["a"] != "Bearer sk-for-a" {
		t.Errorf("Authorization: a %q, b %q", auth["a"], auth["b"])
	}
}

func TestCompletionsReadTheKeychainOncePerOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"text":"x"}]}`)
	}))
	defer srv.Close()
	api := completionsApp(srv.URL, true)
	secrets := api.secrets.(*memSecrets)
	for range 5 {
		if _, err := api.Complete(context.Background(), "tab-1", completions.Request{Prefix: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	if secrets.reads != 1 {
		t.Errorf("%d Keychain reads for 5 requests, want 1", secrets.reads)
	}
	// Setting a key refreshes the cache without another read.
	if err := api.SetAPIKey("sk-new"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := api.HasAPIKey(); !ok || secrets.reads != 1 {
		t.Errorf("after SetAPIKey: has key %v, %d reads", ok, secrets.reads)
	}
}
