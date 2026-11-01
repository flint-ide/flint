package remote

import (
	"encoding/json"
	"strings"
	"testing"

	as "flint/internal/agentsession"
)

const envContent = "DB_PASS=hunter2\nSTRIPE_KEY=rk_live_51Habcdefghijklmnop\nclient-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVk"

func leaks(t *testing.T, e as.Event) []string {
	t.Helper()
	b, _ := json.Marshal(e)
	var out []string
	for _, s := range []string{"hunter2", "rk_live_51H", "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVk", "abc123@o1.ingest.sentry.io"} {
		if strings.Contains(string(b), s) {
			out = append(out, s)
		}
	}
	return out
}

// The review's proof of concept: a Read of .env sent its output and a Write
// to .env its input, only pattern-redacted.
func TestSecretFileReadAndWriteAreWithheld(t *testing.T) {
	e := as.Event{SessionID: "s", Seq: 1, Kind: as.KindToolCall, ToolCall: &as.ToolCall{ID: "1", Title: "Read .env", Paths: []string{"/Users/x/p/.env"}, Output: envContent,
		Input: `{"file_path":".env","content":"SENTRY_DSN=https://abc123@o1.ingest.sentry.io/1"}`}}
	sc := newSecretCalls()
	r := redactEvent(e, sc.observe(e))
	if l := leaks(t, r); len(l) > 0 {
		t.Fatalf("leaked %v: %+v", l, r.ToolCall)
	}
	if r.ToolCall.Title == "" || r.ToolCall.Paths[0] == "" {
		t.Fatal("the call itself must still be shown")
	}
	// A write found only through its JSON input, as a permission request.
	w := as.Event{SessionID: "s", Seq: 2, Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p", ToolCall: as.ToolCall{
		ID: "2", Kind: as.ToolEdit, Input: `{"file_path":"/p/config/.env.production","content":"` + strings.ReplaceAll(envContent, "\n", `\n`) + `"}`}}}
	if l := leaks(t, redactEvent(w, sc.observe(w))); len(l) > 0 {
		t.Fatalf("permission request leaked %v", l)
	}
}

func TestSecretCallDetection(t *testing.T) {
	for _, tc := range []as.ToolCall{
		{Paths: []string{"/p/.env"}},
		{Diffs: []as.Diff{{Path: "/p/id_ed25519"}}},
		{Input: `{"file_path":"/p/.env.local"}`},
		{Input: `{"notebook_path":"/p/.npmrc"}`},
		{Input: `{"edits":[{"filePath":"a.go"},{"path":"/home/x/.kube/config"}]}`},
		{Input: `{"command":"cat .env | head"}`},
		{Input: `{"cmd":["cat","/p/server.key"]}`},
		{Input: "source ./.env && go run ."},
		{Input: "docker run --env-file=.env app"},
		{Input: "cat 'prod.kubeconfig'"},
	} {
		if !secretCall(tc) {
			t.Errorf("%+v not seen as touching a credentials file", tc)
		}
	}
	for _, tc := range []as.ToolCall{
		{Paths: []string{"/p/main.go"}},
		{Input: `{"file_path":"/p/README.md","content":"put secrets in .env"}`},
		{Input: "go test ./..."},
		{Input: `{"pattern":"TODO"}`},
	} {
		if secretCall(tc) {
			t.Errorf("%+v seen as touching a credentials file", tc)
		}
	}
}

// Agents send a call's paths when it starts and its output in later updates:
// the stream remembers the call, also across a reconnect with since.
func TestSecretCallUpdatesAreWithheld(t *testing.T) {
	start := as.Event{SessionID: "s", Seq: 1, Kind: as.KindToolCall, ToolCall: &as.ToolCall{ID: "t1", Kind: as.ToolRead, Paths: []string{"/p/.env"}}}
	update := as.Event{SessionID: "s", Seq: 2, Kind: as.KindToolCallUpdate, ToolCall: &as.ToolCall{ID: "t1", Status: as.StatusCompleted, Output: envContent}}
	other := as.Event{SessionID: "s", Seq: 3, Kind: as.KindToolCall, ToolCall: &as.ToolCall{ID: "t2", Paths: []string{"/p/main.go"}}}
	otherUpdate := as.Event{SessionID: "s", Seq: 4, Kind: as.KindToolCallUpdate, ToolCall: &as.ToolCall{ID: "t2", Output: "package main"}}
	sc := newSecretCalls()
	for _, e := range []as.Event{start, update} {
		if l := leaks(t, redactEvent(e, sc.observe(e))); len(l) > 0 {
			t.Fatalf("seq %d leaked %v", e.Seq, l)
		}
	}
	sc.observe(other)
	if r := redactEvent(otherUpdate, sc.observe(otherUpdate)); r.ToolCall.Output != "package main" {
		t.Fatalf("an ordinary call's output was withheld: %q", r.ToolCall.Output)
	}
	// An update of a call this stream never saw start: withheld.
	fresh := newSecretCalls()
	if r := redactEvent(update, fresh.observe(update)); r.ToolCall.Output != secretFileText {
		t.Fatalf("update of an unseen call: %q", r.ToolCall.Output)
	}
}

func TestSecretCallsAreBounded(t *testing.T) {
	sc := newSecretCalls()
	sc.maxCap = 10
	for i := range 25 {
		sc.observe(as.Event{SessionID: "s", Kind: as.KindToolCall, ToolCall: &as.ToolCall{ID: string(rune('a' + i))}})
	}
	if len(sc.seen) > 10 {
		t.Fatalf("remembered %d calls", len(sc.seen))
	}
}
