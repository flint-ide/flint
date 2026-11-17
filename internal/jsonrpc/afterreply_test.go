package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"sync"
	"testing"
	"time"
)

// logWriter records that a write happened, in the same log as the hook.
type logWriter struct {
	w   io.Writer
	log func(string)
}

func (l *logWriter) Write(p []byte) (int, error) {
	n, err := l.w.Write(p) // an io.Pipe returns once the reader took it
	l.log("write")
	return n, err
}

func TestAfterReplyRunsOnceTheReplyIsWritten(t *testing.T) {
	var mu sync.Mutex
	var order []string
	log := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	done := make(chan struct{})

	cr, sw := io.Pipe() // server to client
	sr, cw := io.Pipe() // client to server
	server := New(&logWriter{w: sw, log: log}, Options{
		OnRequest: func(context.Context, string, json.RawMessage) (any, error) { return struct{}{}, nil },
		AfterReply: func(method string) {
			log("after:" + method)
			close(done)
		},
	})
	client := New(cw, Options{})
	go func() { _ = server.Serve(sr) }()
	go func() { _ = client.Serve(cr) }()
	t.Cleanup(func() { server.Close(nil); client.Close(nil); _ = sw.Close(); _ = cw.Close() })

	if err := client.Call(context.Background(), "bye", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AfterReply was not called")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(order, []string{"write", "after:bye"}) {
		t.Errorf("order = %v, want the reply written before the hook", order)
	}
}

// A writer that never accepts the reply must not hold the hook forever.
func TestAfterReplyRunsEvenWhenTheReplyCannotBeWritten(t *testing.T) {
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	called := make(chan string, 1)
	server := New(blockedWriter{stuck}, Options{
		OnRequest:  func(context.Context, string, json.RawMessage) (any, error) { return struct{}{}, nil },
		AfterReply: func(method string) { called <- method },
	})
	t.Cleanup(func() { server.Close(nil) })
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	go func() { _ = server.Serve(pr) }()
	go func() { _, _ = io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"bye"}`+"\n") }()
	select {
	case m := <-called:
		if m != "bye" {
			t.Errorf("method = %q", m)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("AfterReply was not called")
	}
}

type blockedWriter struct{ release chan struct{} }

func (b blockedWriter) Write(p []byte) (int, error) { <-b.release; return len(p), nil }
