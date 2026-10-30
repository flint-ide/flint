package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/agentsession"
)

func TestApplyHunks(t *testing.T) {
	cases := []struct {
		name, old, diff, want string
	}{
		{"append", "hello\n", "@@ -1 +1,2 @@\n hello\n+world\n", "hello\nworld\n"},
		{"replace middle", "a\nb\nc\n", "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n", "a\nB\nc\n"},
		{"pure insertion", "a\nb\n", "@@ -1,0 +2 @@\n+x\n", "a\nx\nb\n"},
		{"insert at top", "a\n", "@@ -0,0 +1 @@\n+top\n", "top\na\n"},
		{"two hunks with offset", "1\n2\n3\n4\n5\n6\n", "@@ -1,2 +1,2 @@\n-1\n+one\n 2\n@@ -6 +6 @@\n-6\n+six\n", "one\n2\n3\n4\n5\nsix\n"},
		{"shifted hunk", "x\ny\na\nb\n", "@@ -1,2 +1,2 @@\n a\n-b\n+c\n", "x\ny\na\nc\n"},
		{"no newline at eof", "a\nb", "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n", "a\nc"},
		{"git headers", "a\n", "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n", "b\n"},
		{"empty context line", "a\n\nb\n", "@@ -1,3 +1,3 @@\n a\n\n-b\n+c\n", "a\n\nc\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hunks, err := parseHunks(tc.diff)
			if err != nil {
				t.Fatal(err)
			}
			got, err := applyHunks(tc.old, hunks, false)
			if err != nil || got != tc.want {
				t.Fatalf("forward = %q, %v; want %q", got, err, tc.want)
			}
			back, err := applyHunks(got, hunks, true)
			if err != nil || back != tc.old {
				t.Fatalf("reverse = %q, %v; want %q", back, err, tc.old)
			}
		})
	}
}

func TestApplyHunksErrors(t *testing.T) {
	if _, err := parseHunks("no hunks here"); err == nil {
		t.Fatal("parsed a diff without hunks")
	}
	if _, err := parseHunks("@@ -x +1 @@\n+a\n"); err == nil {
		t.Fatal("parsed a bad header")
	}
	if _, err := parseHunks("@@ -1 +1 @@\n?a\n"); err == nil {
		t.Fatal("parsed a bad body line")
	}
	hunks, _ := parseHunks("@@ -1 +1 @@\n-zzz\n+a\n")
	if _, err := applyHunks("hello\n", hunks, false); err == nil {
		t.Fatal("applied a mismatching hunk")
	}
	ins, _ := parseHunks("@@ -5,0 +6 @@\n+x\n")
	if _, err := applyHunks("a\n", ins, false); err == nil {
		t.Fatal("inserted past the end")
	}
}

func TestBuildDiff(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("applied.txt", "hello\nworld\n")
	write("gone.txt", "bye\n")
	write("moved-new.txt", "b\n")

	upd := func(path, diff string) fileUpdateChange {
		var ch fileUpdateChange
		ch.Path, ch.Diff, ch.Kind.Type = path, diff, "update"
		return ch
	}
	// Patch already applied on disk: reconstructed in reverse.
	d, ok := buildDiff(dir, upd("applied.txt", "@@ -1 +1,2 @@\n hello\n+world\n"), true)
	if !ok || d.OldText != "hello\n" || d.NewText != "hello\nworld\n" || d.Path != filepath.Join(dir, "applied.txt") {
		t.Fatalf("applied = %+v %v", d, ok)
	}
	// Pending: the same append applies forward too.
	if d, _ := buildDiff(dir, upd("applied.txt", "@@ -1 +1,2 @@\n hello\n+world\n"), false); d.NewText != "hello\nworld\nworld\n" {
		t.Fatalf("pending = %+v", d)
	}
	// Move: the file is already at its new path.
	mv := upd("moved-old.txt", "@@ -1 +1 @@\n-a\n+b\n")
	mv.Kind.MovePath = "moved-new.txt"
	if d, ok := buildDiff(dir, mv, false); !ok || d.Path != filepath.Join(dir, "moved-new.txt") || d.OldText != "a\n" {
		t.Fatalf("move = %+v %v", d, ok)
	}
	if _, ok := buildDiff(dir, upd("missing.txt", "@@ -1 +1 @@\n-a\n+b\n"), false); ok {
		t.Fatal("reconstructed a missing file")
	}
	if _, ok := buildDiff(dir, upd("applied.txt", "garbage"), false); ok {
		t.Fatal("reconstructed garbage")
	}
	var del fileUpdateChange
	del.Path, del.Kind.Type = "gone.txt", "delete"
	if d, ok := buildDiff(dir, del, false); !ok || d.OldText != "bye\n" || d.NewText != "" {
		t.Fatalf("delete from disk = %+v %v", d, ok)
	}
	del.Diff = "old content\n"
	if d, _ := buildDiff(dir, del, false); d.OldText != "old content\n" {
		t.Fatalf("delete with content = %+v", d)
	}
	var add fileUpdateChange
	add.Path, add.Kind.Type, add.Diff = "new.txt", "add", "@@ -0,0 +1 @@\n+x\n"
	if d, ok := buildDiff(dir, add, false); !ok || d.NewText != "x\n" {
		t.Fatalf("add as hunk = %+v %v", d, ok)
	}
}

func TestFillFileChangeFallback(t *testing.T) {
	dir := t.TempDir()
	var a, b fileUpdateChange
	a.Path, a.Kind.Type, a.Diff = "x.txt", "update", "@@ -1 +1 @@\n-a\n+b\n"
	b.Path, b.Kind.Type, b.Diff = "y.txt", "delete", "y\n"
	var tc agentsession.ToolCall
	fillFileChange(&tc, dir, []fileUpdateChange{a, b}, false)
	if tc.Title != "Edit 2 files" || len(tc.Diffs) != 1 || !strings.Contains(tc.Output, "--- "+filepath.Join(dir, "x.txt")) {
		t.Fatalf("tool call = %+v", tc)
	}
	var del agentsession.ToolCall
	fillFileChange(&del, dir, []fileUpdateChange{b}, false)
	if del.Kind != agentsession.ToolDelete || del.Title != "Delete y.txt" {
		t.Fatalf("delete = %+v", del)
	}
	a.Kind.MovePath = "z.txt"
	if got := editTitle(dir, []fileUpdateChange{a}); got != "Move x.txt → z.txt" {
		t.Fatalf("move title = %q", got)
	}
	if got := editTitle(dir, nil); got != "Edit files" {
		t.Fatalf("empty title = %q", got)
	}
}

func TestDisplayCommand(t *testing.T) {
	cases := map[string]string{
		"/bin/zsh -lc 'cat a.txt'":         "cat a.txt",
		"bash -c 'echo '\\''hi'\\'''":      "echo 'hi'",
		"/bin/bash -lc \"ls -la\"":         "\"ls -la\"",
		"git status":                       "git status",
		"/bin/sh -c 'a' 'b'":               "'a' 'b'",
		"/usr/local/bin/zsh -lc 'go test'": "go test",
	}
	for in, want := range cases {
		if got := displayCommand(in); got != want {
			t.Errorf("displayCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateOutput(t *testing.T) {
	if got := truncateOutput("short"); got != "short" {
		t.Fatal(got)
	}
	long := strings.Repeat("é", maxOutputBytes) // 2 bytes per rune
	got := truncateOutput(long)
	if !strings.HasPrefix(got, truncatedPrefix) || len(got) > maxOutputBytes+len(truncatedPrefix) {
		t.Fatalf("len = %d", len(got))
	}
	if body := strings.TrimPrefix(got, truncatedPrefix); strings.ToValidUTF8(body, "?") != body {
		t.Fatal("cut inside a rune")
	}
	var st itemState
	for range 5 {
		st.appendOutput(strings.Repeat("x", maxOutputBytes))
	}
	if st.output.Len() > 2*maxOutputBytes+len(truncatedPrefix) {
		t.Fatalf("buffer grew to %d", st.output.Len())
	}
}

func TestAttachmentText(t *testing.T) {
	got := attachmentText(agentsession.Attachment{Path: "/p/a.md", Text: "x ```` y", StartLine: 3})
	want := "/p/a.md:3-3\n`````\nx ```` y\n`````"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := attachmentText(agentsession.Attachment{Path: "/p/b.go", Text: "code\n"}); got != "/p/b.go\n```\ncode\n```" {
		t.Fatalf("whole file = %q", got)
	}
	items := buildInput("hi", []agentsession.Attachment{{}, {Path: "/p/c"}})
	if len(items) != 2 || items[1].Text != "Relevant file: /p/c" || items[0].TextElements == nil {
		t.Fatalf("items = %+v", items)
	}
}

func TestToolCallVariants(t *testing.T) {
	ok, fail := true, false
	exit0 := 0
	cases := []struct {
		it                threadItem
		kind, title, stat string
	}{
		{threadItem{Type: "imageView", ID: "1", Path: "img.png"}, agentsession.ToolRead, "View image img.png", agentsession.StatusInProgress},
		{threadItem{Type: "dynamicToolCall", ID: "2", Tool: "lookup", Status: "completed", Success: &fail}, agentsession.ToolOther, "lookup", agentsession.StatusFailed},
		{threadItem{Type: "dynamicToolCall", ID: "3", Tool: "lookup", Status: "completed", Success: &ok}, agentsession.ToolOther, "lookup", agentsession.StatusCompleted},
		{threadItem{Type: "collabAgentToolCall", ID: "4", Tool: "spawnAgent", Status: "inProgress"}, agentsession.ToolOther, "Agent: spawnAgent", agentsession.StatusInProgress},
		{threadItem{Type: "commandExecution", ID: "5", Command: "ls", Status: "declined"}, agentsession.ToolExecute, "Run: ls", agentsession.StatusFailed},
		{threadItem{Type: "commandExecution", ID: "6", Command: "ls", Status: "completed", ExitCode: &exit0, CommandActions: []commandAction{{Type: "read", Path: "f"}}}, agentsession.ToolExecute, "Run: ls", agentsession.StatusCompleted},
	}
	for _, tc := range cases {
		got := toolCallFor("/w", tc.it)
		if got.Kind != tc.kind || got.Title != tc.title || got.Status != tc.stat {
			t.Errorf("%s: got %+v", tc.it.Type, got)
		}
	}
	mcp := toolCallFor("/w", threadItem{Type: "mcpToolCall", Server: "s", Tool: "t", Status: "failed",
		ItemError: &struct {
			Message string `json:"message"`
		}{"denied"}, Arguments: json.RawMessage(`not json`)})
	if mcp.Output != "denied" || mcp.Input != "not json" || mcp.Status != agentsession.StatusFailed {
		t.Fatalf("mcp = %+v", mcp)
	}
	if got := toolCallFor("/w", cases[5].it).Paths; len(got) != 1 || got[0] != "/w/f" {
		t.Fatalf("paths = %v", got)
	}
}

func TestRingBufferAndLineReader(t *testing.T) {
	rb := newRingBuffer(8)
	_, _ = rb.Write([]byte("abc"))
	if got := rb.Tail(100); got != "abc" {
		t.Fatal(got)
	}
	_, _ = rb.Write([]byte("defghij"))
	if got := rb.Tail(100); got != "cdefghij" {
		t.Fatal(got)
	}
	_, _ = rb.Write([]byte("0123456789"))
	if got := rb.Tail(3); got != "789" {
		t.Fatal(got)
	}

	lr := newLineReader(strings.NewReader("one\r\n"+strings.Repeat("x", 100)+"\nlast"), 50)
	if line, err := lr.next(); err != nil || string(line) != "one" {
		t.Fatalf("%q %v", line, err)
	}
	if _, err := lr.next(); !errors.Is(err, errMessageTooLarge) {
		t.Fatalf("oversized line: %v", err)
	}
}

func TestIDsAndErrors(t *testing.T) {
	for raw, want := range map[string]int64{`7`: 7, `"8"`: 8} {
		if got, ok := parseID(json.RawMessage(raw)); !ok || got != want {
			t.Errorf("parseID(%s) = %d %v", raw, got, ok)
		}
	}
	if _, ok := parseID(json.RawMessage(`{}`)); ok {
		t.Error("parsed an object id")
	}
	if got := requestKey(json.RawMessage(`"abc"`)); got != "abc" {
		t.Error(got)
	}
	if got := requestKey(json.RawMessage(` 12 `)); got != "12" {
		t.Error(got)
	}
	e := &RPCError{Code: 1, Message: "m", Data: json.RawMessage(`{"x":1}`)}
	if !strings.Contains(e.Error(), `{"x":1}`) || (&RPCError{Code: 2, Message: "n"}).Error() != "rpc error 2: n" {
		t.Error(e.Error())
	}
	exit := &ExitError{Err: errors.New("exit status 2"), Stderr: " boom \n"}
	if exit.Error() != "codex: app-server exited: exit status 2; stderr: boom" || !errors.Is(exit, exit.Err) {
		t.Error(exit.Error())
	}
	if (&TurnError{Message: "x"}).Error() != "codex: turn failed: x" {
		t.Error("TurnError")
	}
}

func TestStartErrors(t *testing.T) {
	ctx := context.Background()
	sink := func(agentsession.Event) {}
	d := New(Options{Binary: "definitely-not-a-codex-binary"})
	if _, err := d.Start(ctx, t.TempDir(), nil); err == nil {
		t.Fatal("nil sink accepted")
	}
	if _, err := d.Start(ctx, filepath.Join(t.TempDir(), "missing"), sink); err == nil {
		t.Fatal("missing cwd accepted")
	}
	file := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(file, nil, 0o600)
	if _, err := d.Start(ctx, file, sink); err == nil {
		t.Fatal("file cwd accepted")
	}
	if _, err := d.Start(ctx, t.TempDir(), sink); err == nil {
		t.Fatal("missing binary accepted")
	}
	for _, call := range []func() error{
		func() error { _, err := d.Prompt(ctx, "nope", "x", nil); return err },
		func() error { return d.Cancel("nope") },
		func() error { return d.Respond("nope", "1", "accept") },
		func() error { return d.Stop("nope") },
	} {
		if call() == nil {
			t.Fatal("unknown session accepted")
		}
	}
	if _, err := resolveCommand("", nil); err == nil {
		t.Fatal("empty command resolved")
	}
	if got, err := resolveCommand("sh", []string{"PATH=relative:/bin"}); err != nil || got != "/bin/sh" {
		t.Fatalf("resolve sh = %q %v", got, err)
	}
	if got := lookupEnv([]string{"A=1", "A=2"}, "A"); got != "2" {
		t.Fatal(got)
	}
}

// A server that dies during the handshake fails Start with its stderr.
func TestStartHandshakeCrash(t *testing.T) {
	h := newHarness(t, "handshake_crash.jsonl", nil)
	_, err := h.d.Start(context.Background(), h.cwd, h.rec.sink)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || !strings.Contains(exitErr.Stderr, "not logged in") {
		t.Fatalf("Start: %v", err)
	}
}
