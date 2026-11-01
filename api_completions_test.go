package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flint/internal/completions"
	"flint/internal/settings"
)

type memSecrets struct{ v string }

func (m *memSecrets) Get() (string, error) { return m.v, nil }
func (m *memSecrets) Set(v string) error   { m.v = v; return nil }

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
	api.Cancel("tab-1")
}
