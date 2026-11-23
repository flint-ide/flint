package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

// An answer that arrived is the answer, even when the connection ended right
// behind it: a peer that answers one request and closes (the command line's
// socket) must not have its answer turned into "connection closed". Serve
// hands the answer to the waiting call before it reads the end, so by the
// time the call looks, both can be ready; select would pick one at random.
func TestCallPrefersAnAnswerThatArrivedOverTheEnd(t *testing.T) {
	c := New(io.Discard, Options{})
	c.Close(io.EOF)
	// Each round is deterministic with the fix; without it, select picks
	// the end in half of them, so 64 rounds all passing by luck is 2^-64.
	for i := range 64 {
		ch := make(chan *message, 1)
		ch <- &message{JSONRPC: "2.0", ID: json.RawMessage("1"), Result: json.RawMessage(`"yes"`)}
		msg, err := c.await(context.Background(), 1, ch)
		if err != nil || msg == nil || string(msg.Result) != `"yes"` {
			t.Fatalf("round %d: the answer was lost to the end: %v", i, err)
		}
	}
	// Without an answer, the end is the error.
	if _, err := c.await(context.Background(), 1, make(chan *message, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("no answer: %v, want io.EOF", err)
	}
}

// End to end: a peer that writes its answer and closes at once.
func TestCallGetsTheAnswerOfAPeerThatClosesRightAfter(t *testing.T) {
	for i := range 200 {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		go func() {
			defer sw.Close()
			br := bufio.NewReader(sr)
			line, err := br.ReadBytes('\n')
			_ = sr.Close()
			if err != nil {
				return
			}
			var req struct {
				ID json.RawMessage `json:"id"`
			}
			if json.Unmarshal(line, &req) != nil {
				return
			}
			_, _ = sw.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":"yes"}` + "\n"))
		}()
		c := New(cw, Options{})
		go func() { _ = c.Serve(cr) }()
		var got string
		if err := c.Call(context.Background(), "ask", nil, &got); err != nil || got != "yes" {
			t.Fatalf("round %d: %q %v", i, got, err)
		}
		c.Close(nil)
		_ = cw.Close()
	}
}
