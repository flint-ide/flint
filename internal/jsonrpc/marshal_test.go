package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// HTML-heavy text must not grow sixfold on the wire (encoding/json writes <
// as < unless told not to), or a prompt of a megabyte of markup would
// not fit a 4 MiB message.
func TestMessagesAreNotHTMLEscaped(t *testing.T) {
	var out syncBuffer
	c := New(&out, Options{})
	defer c.Close(nil)
	text := strings.Repeat(`<a href="x">&</a>`, 100)
	if err := c.Notify("n", map[string]string{"text": text}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return out.String() != "" })
	line := out.String()
	if strings.Contains(line, "\\u003c") || strings.Contains(line, "\\u0026") {
		t.Errorf("the message was HTML-escaped: %.80s", line)
	}
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Errorf("a message must be exactly one line: %q", line[len(line)-10:])
	}
	var msg struct{ Params map[string]string }
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.Params["text"] != text {
		t.Errorf("the text did not survive: %v", err)
	}
}

func TestLineSeparatorsStayEscaped(t *testing.T) {
	data, err := Marshal(map[string]string{"t": "a b c\nd"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(data), "\n  ") {
		t.Errorf("raw line separators in %q", data)
	}
}

func TestMarshalMatchesWhatIsSent(t *testing.T) {
	v := map[string]string{"x": "<&>"}
	data, _ := Marshal(v)
	if string(data) != `{"x":"<&>"}` {
		t.Errorf("Marshal = %s", data)
	}
	// A call over the limit fails clearly, before anything is written.
	c := New(io.Discard, Options{MaxMessage: 100})
	defer c.Close(nil)
	err := c.Call(context.Background(), "m", map[string]string{"x": strings.Repeat("a", 200)}, nil)
	if err == nil || !strings.Contains(err.Error(), "over the limit") {
		t.Errorf("oversized call = %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 500 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}
