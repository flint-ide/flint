package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// fakeAdapter answers requests on the other end of a pipe with handle's result.
func fakeAdapter(t *testing.T, handle func(m message) (body any, err string)) (*conn, chan string) {
	t.Helper()
	client, server := net.Pipe()
	events := make(chan string, 8)
	c := newConn(client, client, func(name string, _ json.RawMessage) { events <- name })
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	go func() {
		r := bufio.NewReader(server)
		seq := 100
		for {
			body, err := readMessage(r)
			if err != nil {
				return
			}
			var m message
			_ = json.Unmarshal(body, &m)
			if m.Type != "request" {
				continue
			}
			res, msg := handle(m)
			ok := msg == ""
			seq++
			_ = write(server, message{Seq: seq, Type: "event", Event: "before-" + m.Command})
			seq++
			_ = write(server, message{Seq: seq, Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: &ok, Message: msg, Body: mustJSON(res)})
		}
	}()
	return c, events
}

func write(c net.Conn, m message) error {
	b, _ := json.Marshal(m)
	_, err := c.Write(append([]byte("Content-Length: "+strconv.Itoa(len(b))+"\r\n\r\n"), b...))
	return err
}

func TestRequestDecodesBodyAndDeliversEventsInOrder(t *testing.T) {
	c, events := fakeAdapter(t, func(m message) (any, string) {
		return map[string]any{"threads": []Thread{{ID: 1, Name: "main"}}}, ""
	})
	var out struct {
		Threads []Thread `json:"threads"`
	}
	if err := c.Request(context.Background(), "threads", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Threads) != 1 || out.Threads[0].Name != "main" {
		t.Fatalf("threads = %+v", out.Threads)
	}
	if got := <-events; got != "before-threads" {
		t.Fatalf("event = %q", got)
	}
}

func TestFailedResponseIsAnError(t *testing.T) {
	c, _ := fakeAdapter(t, func(m message) (any, string) {
		return map[string]any{"error": map[string]any{"format": "could not find breakpoint"}}, "failed"
	})
	err := c.Request(context.Background(), "setBreakpoints", nil, nil)
	var de *Error
	if !errors.As(err, &de) || de.Message != "could not find breakpoint" {
		t.Fatalf("err = %v", err)
	}
}

func TestRequestFailsWhenAdapterGoesAway(t *testing.T) {
	client, server := net.Pipe()
	c := newConn(client, client, func(string, json.RawMessage) {})
	go func() {
		_, _ = readMessage(bufio.NewReader(server))
		_ = server.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Request(ctx, "threads", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}
