package client

import (
	"context"
	"testing"
)

func TestCheckBaseRefusesPlainHTTPOffThisMachine(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:7419", "http://localhost:8000", "http://[::1]:7419", "https://laptop.tail1234.ts.net", "https://192.168.1.5"} {
		if err := CheckBase(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://192.168.1.5:7419", "http://laptop.tail1234.ts.net", "ws://127.0.0.1:7419", "ftp://x", "127.0.0.1:7419", ""} {
		if err := CheckBase(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The token (and the pairing secret) never leave for a refused address:
// the calls fail before any connection.
func TestClientRefusesBeforeSending(t *testing.T) {
	c := &Client{Base: "http://203.0.113.7:7419", Token: "0123456789abcdef.secret"}
	if _, err := c.Chats(context.Background()); err == nil {
		t.Fatal("Chats over plain http to another machine")
	}
	if _, err := c.Stream(context.Background(), nil); err == nil {
		t.Fatal("Stream over plain http to another machine")
	}
	if _, _, err := Pair(context.Background(), nil, "http://203.0.113.7:7419", "ABCD", "phone"); err == nil {
		t.Fatal("Pair over plain http to another machine")
	}
}
