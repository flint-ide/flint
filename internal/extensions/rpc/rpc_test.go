package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// pair connects two Conns through pipes; both are closed when the test ends.
func pair(t *testing.T, a, b Options) (*Conn, *Conn) {
	t.Helper()
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	ca, cb := New(aw, a), New(bw, b)
	go func() { _ = ca.Serve(ar) }()
	go func() { _ = cb.Serve(br) }()
	t.Cleanup(func() {
		ca.Close(nil)
		cb.Close(nil)
		_ = aw.Close()
		_ = bw.Close()
	})
	return ca, cb
}

func TestCallAndNotify(t *testing.T) {
	notes := make(chan string, 4)
	client, server := pair(t, Options{}, Options{
		OnRequest: func(_ context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "echo":
				return json.RawMessage(params), nil
			case "nil":
				return nil, nil
			case "denied":
				return nil, Errorf(CodePermission, "no permission")
			}
			return nil, errors.New("boom")
		},
		OnNotify: func(method string, _ json.RawMessage) { notes <- method },
	})
	_ = server
	ctx := context.Background()
	var got map[string]int
	if err := client.Call(ctx, "echo", map[string]int{"a": 1}, &got); err != nil || got["a"] != 1 {
		t.Fatalf("echo = %v, %v", got, err)
	}
	if err := client.Call(ctx, "nil", nil, nil); err != nil {
		t.Errorf("nil result: %v", err)
	}
	var rpcErr *Error
	if err := client.Call(ctx, "denied", nil, nil); !errors.As(err, &rpcErr) || rpcErr.Code != CodePermission {
		t.Errorf("denied = %v", err)
	}
	if err := client.Call(ctx, "other", nil, nil); !errors.As(err, &rpcErr) || rpcErr.Code != CodeInternal || rpcErr.Message != "boom" {
		t.Errorf("plain error = %v", err)
	}
	if err := client.Notify("hello", nil); err != nil {
		t.Fatal(err)
	}
	if m := <-notes; m != "hello" {
		t.Errorf("notification = %q", m)
	}
}

func TestMethodNotFoundWithoutHandler(t *testing.T) {
	client, _ := pair(t, Options{}, Options{})
	var rpcErr *Error
	if err := client.Call(context.Background(), "x", nil, nil); !errors.As(err, &rpcErr) || rpcErr.Code != CodeMethodNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestTimeoutCancelsTheRequest(t *testing.T) {
	cancelled := make(chan struct{})
	client, _ := pair(t, Options{}, Options{
		OnRequest: func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := client.Call(ctx, "slow", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the other side never saw $/cancelRequest")
	}
}

func TestCloseFailsPendingCalls(t *testing.T) {
	client, server := pair(t, Options{}, Options{
		OnRequest: func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
			<-ctx.Done()
			return nil, nil
		},
	})
	errc := make(chan error, 1)
	go func() { errc <- client.Call(context.Background(), "hang", nil, nil) }()
	time.Sleep(20 * time.Millisecond)
	server.Close(errors.New("gone"))
	client.Close(errors.New("process exited"))
	if err := <-errc; err == nil || !strings.Contains(err.Error(), "process exited") {
		t.Errorf("err = %v", err)
	}
}

func TestServeRefusesOversizeAndGarbage(t *testing.T) {
	for name, input := range map[string]string{
		"too large":    `{"jsonrpc":"2.0","method":"x","params":"` + strings.Repeat("a", 200) + `"}` + "\n",
		"not json":     "hello from a debug print\n",
		"no version":   `{"method":"x"}` + "\n",
		"no method/id": `{"jsonrpc":"2.0"}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			c := New(io.Discard, Options{MaxMessage: 100})
			err := c.Serve(strings.NewReader(input))
			if err == nil || errors.Is(err, io.EOF) {
				t.Errorf("Serve = %v, want a protocol error", err)
			}
			select {
			case <-c.Done():
			default:
				t.Error("the connection stays open")
			}
		})
	}
}

func TestServeAnswersEachRequest(t *testing.T) {
	var out syncBuffer
	c := New(&out, Options{
		OnRequest: func(_ context.Context, method string, _ json.RawMessage) (any, error) { return method, nil },
	})
	input := `{"jsonrpc":"2.0","id":"a","method":"one"}` + "\r\n\n" + `{"jsonrpc":"2.0","id":{},"method":"bad"}` + "\n"
	pr, pw := io.Pipe()
	go func() { _ = c.Serve(pr) }()
	_, _ = pw.Write([]byte(input))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && strings.Count(out.String(), "\n") < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	_ = pw.Close()
	got := out.String()
	if !strings.Contains(got, `{"jsonrpc":"2.0","id":"a","result":"one"}`) || !strings.Contains(got, `"code":-32600`) {
		t.Errorf("replies = %s", got)
	}
}

func TestRateLimit(t *testing.T) {
	r := rateLimit{max: 10, window: 3}
	now := time.Now()
	var err error
	for i := 0; i < 5 && err == nil; i++ {
		err = r.add(11, now.Add(time.Duration(i)*time.Second))
	}
	if err == nil {
		t.Error("a sustained flood is allowed")
	}
	r = rateLimit{max: 10, window: 3}
	for i := 0; i < 10; i++ {
		if err := r.add(11, now.Add(time.Duration(i)*3*time.Second)); err != nil {
			t.Fatalf("bursts with pauses: %v", err)
		}
	}
}

func FuzzServe(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"x","params":{}}` + "\n"))
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":null}` + "\n"))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":1}}` + "\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c := New(io.Discard, Options{
			MaxMessage: 1 << 10,
			OnRequest:  func(context.Context, string, json.RawMessage) (any, error) { return nil, nil },
			OnNotify:   func(string, json.RawMessage) {},
		})
		_ = c.Serve(bytes.NewReader(data))
		c.Close(nil)
	})
}

// syncBuffer is a bytes.Buffer safe for the writer goroutine and the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
