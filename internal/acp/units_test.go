package acp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	r := newRingBuffer(8)
	_, _ = r.Write([]byte("abc"))
	if got := string(r.Bytes()); got != "abc" {
		t.Fatalf("Bytes = %q", got)
	}
	_, _ = r.Write([]byte("defgh"))
	_, _ = r.Write([]byte("ij"))
	if got := string(r.Bytes()); got != "cdefghij" {
		t.Fatalf("Bytes after wrap = %q", got)
	}
	_, _ = r.Write([]byte("0123456789XY"))
	if got := string(r.Bytes()); got != "456789XY" {
		t.Fatalf("Bytes after oversized write = %q", got)
	}
	if got := r.Tail(3); got != "9XY" {
		t.Fatalf("Tail = %q", got)
	}
	if n, err := r.Write(nil); n != 0 || err != nil {
		t.Fatalf("empty write = %d, %v", n, err)
	}
}

func TestLineReader(t *testing.T) {
	in := "first\r\n\nsecond\n" + strings.Repeat("x", 300) + "\nlast"
	lr := newLineReader(strings.NewReader(in), 1<<10)
	lr.r = bufioReaderSize(in, 16) // force multi-chunk lines
	want := []string{"first", "", "second", strings.Repeat("x", 300), "last"}
	for _, w := range want {
		got, err := lr.next()
		if err != nil || string(got) != w {
			t.Fatalf("next = %q, %v; want %q", got, err, w)
		}
	}
	if _, err := lr.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}

	tooBig := newLineReader(strings.NewReader(strings.Repeat("y", 100)+"\n"), 10)
	if _, err := tooBig.next(); !errors.Is(err, errMessageTooLarge) {
		t.Fatalf("want errMessageTooLarge, got %v", err)
	}
}

func TestApplyLineWindow(t *testing.T) {
	content := "a\nb\nc\nd\n"
	ptr := func(n int) *int { return &n }
	cases := []struct {
		line, limit *int
		want        string
	}{
		{nil, nil, content},
		{ptr(2), nil, "b\nc\nd\n"},
		{ptr(2), ptr(2), "b\nc\n"},
		{nil, ptr(1), "a\n"},
		{ptr(0), ptr(1), "a\n"},
		{ptr(9), nil, ""},
		{ptr(4), ptr(10), "d\n"},
		{nil, ptr(0), ""},
	}
	for _, tc := range cases {
		got := ApplyLineWindow(content, ReadTextFileRequest{Line: tc.line, Limit: tc.limit})
		if got != tc.want {
			t.Errorf("line=%v limit=%v: got %q want %q", deref(tc.line), deref(tc.limit), got, tc.want)
		}
	}
	if got := ApplyLineWindow("x\ny", ReadTextFileRequest{Line: ptr(2)}); got != "y" {
		t.Errorf("no trailing newline: %q", got)
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestContentBlockJSON(t *testing.T) {
	size := int64(42)
	cases := []struct {
		block ContentBlock
		want  string
	}{
		{TextBlock(""), `{"text":"","type":"text"}`},
		{ResourceLinkBlock("file:///a.go", "a.go"), `{"name":"a.go","type":"resource_link","uri":"file:///a.go"}`},
		{
			ContentBlock{Type: ContentResourceLink, URI: "u", Name: "n", MimeType: "m", Title: "t", Description: "d", Size: &size},
			`{"description":"d","mimeType":"m","name":"n","size":42,"title":"t","type":"resource_link","uri":"u"}`,
		},
		{TextResourceBlock("file:///a.go", "", ""), `{"resource":{"text":"","uri":"file:///a.go"},"type":"resource"}`},
		{
			ContentBlock{Type: ContentResource, Resource: &ResourceContents{URI: "u", MimeType: "image/png", Blob: "AAA="}},
			`{"resource":{"blob":"AAA=","mimeType":"image/png","uri":"u"},"type":"resource"}`,
		},
		{ContentBlock{Type: ContentImage, Data: "AAA=", MimeType: "image/png"}, `{"data":"AAA=","mimeType":"image/png","type":"image"}`},
		{ContentBlock{Type: ContentAudio, Data: "AAA=", MimeType: "audio/wav"}, `{"data":"AAA=","mimeType":"audio/wav","type":"audio"}`},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.block)
		if err != nil || string(got) != tc.want {
			t.Errorf("Marshal(%+v) = %s, %v; want %s", tc.block, got, err, tc.want)
		}
	}
	for _, bad := range []ContentBlock{{Type: "video"}, {Type: ContentResource}} {
		if _, err := json.Marshal(bad); err == nil {
			t.Errorf("Marshal(%+v) should fail", bad)
		}
	}
}

func TestPermissionOutcomeJSON(t *testing.T) {
	got, _ := json.Marshal(permissionResponse{Outcome: Selected("allow-once")})
	if string(got) != `{"outcome":{"outcome":"selected","optionId":"allow-once"}}` {
		t.Errorf("selected = %s", got)
	}
	got, _ = json.Marshal(permissionResponse{Outcome: Cancelled()})
	if string(got) != `{"outcome":{"outcome":"cancelled"}}` {
		t.Errorf("cancelled = %s", got)
	}
	req := PermissionRequest{Options: []PermissionOption{{OptionID: "a"}}}
	if err := req.checkOutcome(PermissionOutcome{Outcome: "maybe"}); err == nil {
		t.Error("invalid outcome accepted")
	}
}

func TestRPCErrorString(t *testing.T) {
	e := errMethodNotFound("terminal/create")
	if !strings.Contains(e.Error(), "-32601") || !strings.Contains(e.Error(), "terminal/create") {
		t.Errorf("Error() = %q", e.Error())
	}
	if got := (&RPCError{Code: 1, Message: "m"}).Error(); got != "rpc error 1: m" {
		t.Errorf("Error() = %q", got)
	}
}

func TestParseIDAndKey(t *testing.T) {
	for raw, want := range map[string]int64{`7`: 7, `"8"`: 8} {
		if got, ok := parseID(json.RawMessage(raw)); !ok || got != want {
			t.Errorf("parseID(%s) = %d, %v", raw, got, ok)
		}
	}
	if _, ok := parseID(json.RawMessage(`{}`)); ok {
		t.Error("parseID({}) should fail")
	}
	if idKey(json.RawMessage(` 5 `)) != "5" {
		t.Error("idKey should compact")
	}
}

func TestResolveCommandUsesAgentPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH resolution delegates to exec.LookPath on windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-acp-adapter")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveCommand("fake-acp-adapter", []string{"PATH=/nonexistent", "PATH=relative:" + dir})
	if err != nil || got != bin {
		t.Fatalf("resolveCommand = %q, %v; want %q", got, err, bin)
	}
	if _, err := resolveCommand("", nil); err == nil {
		t.Error("empty command accepted")
	}
	if got, err := resolveCommand(bin, nil); err != nil || got != bin {
		t.Errorf("absolute command = %q, %v", got, err)
	}
	if lookupEnv([]string{"A=1", "A=2"}, "A") != "2" {
		t.Error("lookupEnv should prefer the last entry")
	}
}

func TestAgentExitedErrorMessage(t *testing.T) {
	e := &AgentExitedError{Err: errors.New("exit status 2"), Stderr: "  boom\n"}
	if e.Error() != "acp: agent exited: exit status 2; stderr: boom" {
		t.Errorf("Error() = %q", e.Error())
	}
	if (&AgentExitedError{}).Error() != "acp: agent exited" {
		t.Error("clean exit message")
	}
}

func bufioReaderSize(s string, size int) *bufio.Reader {
	return bufio.NewReaderSize(strings.NewReader(s), size)
}
