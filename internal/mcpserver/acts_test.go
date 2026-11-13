package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// actTool counts its runs; with plan it checks its arguments first.
func actTool(ran *atomic.Int32, plan bool) Tool {
	t := Tool{
		Name: "act", Title: "Act", Acts: true,
		Call: func(_ context.Context, root string, _ json.RawMessage) (string, error) {
			ran.Add(1)
			return "did it in " + root, nil
		},
	}
	if plan {
		t.Call = nil
		t.Plan = func(_ context.Context, root string, args json.RawMessage) (Planned, error) {
			var in struct{ What string }
			if err := json.Unmarshal(args, &in); err != nil || in.What == "" {
				return Planned{}, errors.New("what is required")
			}
			return Planned{Summary: "Do " + in.What + " with token ghp_" + strings.Repeat("a", 36), Run: func(context.Context) (string, error) {
				ran.Add(1)
				return "did " + in.What + " in " + root, nil
			}}, nil
		}
	}
	return t
}

const callAct = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"act","arguments":{"what":"x"}}}`

func TestActingToolsAreAnnounced(t *testing.T) {
	var ran atomic.Int32
	s := newTestServer(t, Options{Tools: func() []Tool { return []Tool{echoTool(), actTool(&ran, false)} }})
	url, token, _, _ := s.Endpoint("/p")
	r := call(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var list struct {
		Tools []struct {
			Name        string `json:"name"`
			Annotations struct {
				ReadOnly    bool `json:"readOnlyHint"`
				Destructive bool `json:"destructiveHint"`
				Idempotent  bool `json:"idempotentHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &list); err != nil || len(list.Tools) != 2 {
		t.Fatalf("list %s %v", r.Result, err)
	}
	read, act := list.Tools[0].Annotations, list.Tools[1].Annotations
	if !read.ReadOnly || read.Destructive || !read.Idempotent {
		t.Errorf("read tool announced as %+v", read)
	}
	if act.ReadOnly || !act.Destructive || act.Idempotent {
		t.Errorf("acting tool announced as %+v", act)
	}
	if !s.Acts("act") || s.Acts("echo") || s.Acts("nope") {
		t.Error("Acts is wrong")
	}
}

// Without a way to ask, or without a yes, an acting tool never runs.
func TestActingToolNeedsApproval(t *testing.T) {
	var ran atomic.Int32
	tools := func() []Tool { return []Tool{actTool(&ran, false)} }

	s := newTestServer(t, Options{Tools: tools})
	url, token, _, _ := s.Endpoint("/p")
	if text, isErr := toolText(t, call(t, url, token, callAct)); !isErr || !strings.Contains(text, "no way to ask") {
		t.Fatalf("no approver: %q %v", text, isErr)
	}

	var asked []Approval
	var mu sync.Mutex
	answer := ErrNotApproved
	s = newTestServer(t, Options{Tools: tools, Approve: func(_ context.Context, a Approval) error {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, a)
		return answer
	}})
	url, token, _, _ = s.Endpoint("/p")
	if text, isErr := toolText(t, call(t, url, token, callAct)); !isErr || text != ErrNotApproved.Error() {
		t.Fatalf("denied: %q %v", text, isErr)
	}
	if ran.Load() != 0 {
		t.Fatal("the tool ran without approval")
	}
	if len(asked) != 1 || asked[0].Root != "/p" || asked[0].Tool != "act" || asked[0].Title != "Act" || string(asked[0].Args) != `{"what":"x"}` {
		t.Fatalf("asked %+v", asked)
	}

	answer = nil
	if text, isErr := toolText(t, call(t, url, token, callAct)); isErr || text != "did it in /p" {
		t.Fatalf("approved: %q %v", text, isErr)
	}
	// Every call asks again.
	toolText(t, call(t, url, token, callAct))
	if ran.Load() != 2 || len(asked) != 3 {
		t.Fatalf("ran %d times, asked %d times", ran.Load(), len(asked))
	}
}

// A plan refuses bad arguments before the user is asked, and the user sees
// its (redacted) summary; what was planned is what runs.
func TestActingToolPlan(t *testing.T) {
	var ran atomic.Int32
	var asked []Approval
	s := newTestServer(t, Options{
		Tools: func() []Tool { return []Tool{actTool(&ran, true)} },
		Approve: func(_ context.Context, a Approval) error {
			asked = append(asked, a)
			return nil
		},
	})
	url, token, _, _ := s.Endpoint("/p")
	bad := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"act","arguments":{}}}`
	if text, isErr := toolText(t, call(t, url, token, bad)); !isErr || text != "what is required" {
		t.Fatalf("bad arguments: %q %v", text, isErr)
	}
	if len(asked) != 0 {
		t.Fatal("asked about a call that can't run")
	}
	if text, isErr := toolText(t, call(t, url, token, callAct)); isErr || text != "did x in /p" {
		t.Fatalf("planned call: %q %v", text, isErr)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0].Summary, "Do x with token ") || strings.Contains(asked[0].Summary, "ghp_aaaa") {
		t.Fatalf("asked %+v", asked)
	}
	if ran.Load() != 1 {
		t.Fatalf("ran %d times", ran.Load())
	}
}

// A read tool is not asked about.
func TestReadToolsAreNotAskedAbout(t *testing.T) {
	asked := 0
	s := newTestServer(t, Options{Approve: func(context.Context, Approval) error {
		asked++
		return ErrNotApproved
	}})
	url, token, _, _ := s.Endpoint("/p")
	r := call(t, url, token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
	if text, isErr := toolText(t, r); isErr || text != "/p:hi" || asked != 0 {
		t.Fatalf("read call: %q %v, asked %d", text, isErr, asked)
	}
}
