package agenttask

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStoreAddFindRemove(t *testing.T) {
	var s Store
	s2, err := s.Add("/r/.git", Task{ID: "a1", Worktree: "/wt/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Error("Add changed the original store")
	}
	s3, _ := s2.Add("/r/.git", Task{ID: "b2", Worktree: "/wt/b"})
	if repo, task, ok := s3.Find("b2"); !ok || repo != "/r/.git" || task.Worktree != "/wt/b" {
		t.Errorf("Find = %q %+v %v", repo, task, ok)
	}
	if id := s3.ByWorktree("/wt/a"); id != "a1" {
		t.Errorf("ByWorktree = %q", id)
	}
	if _, err := s3.Add("/r/.git", Task{ID: "a1"}); err == nil {
		t.Error("duplicate id accepted")
	}
	s4 := s3.Update("a1", func(t Task) Task { t.State = Finished; return t })
	if _, task, _ := s4.Find("a1"); task.State != Finished {
		t.Error("Update lost")
	}
	if _, task, _ := s3.Find("a1"); task.State != "" {
		t.Error("Update changed the original store")
	}
	s5 := s4.Remove("a1").Remove("b2")
	if len(s5) != 0 {
		t.Errorf("empty repository kept: %v", s5)
	}
	if len(s4.List("/r/.git")) != 2 || len(s4.List("/other")) != 0 {
		t.Error("List wrong")
	}
}

func TestStoreBounds(t *testing.T) {
	var s Store
	var err error
	for i := range MaxPerRepo {
		if s, err = s.Add("/r", Task{ID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Add("/r", Task{ID: "x"}); err == nil {
		t.Error("more than MaxPerRepo tasks accepted")
	}
}

func TestStoreJSONRoundTrip(t *testing.T) {
	s, _ := Store{}.Add("/r/.git", Task{ID: "a1", Title: "Fix", Branch: "agent/fix", Base: "main", Worktree: "/wt", Agent: "claude", CreatedAt: 5})
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Store
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if _, task, ok := back.Find("a1"); !ok || task.Branch != "agent/fix" || task.CreatedAt != 5 {
		t.Errorf("round trip = %+v", back)
	}
}

// A task's origin is kept in state.json; a file written before origins
// loads with none, and a task without one writes no key.
func TestStoreOriginRoundTrip(t *testing.T) {
	tests := []struct {
		name, json, want string
	}{
		{"with an origin", `{"/r/.git":[{"id":"a1","prompt":"Fix","origin":"projects:{\"p\":\"/plan\"}"}]}`, `projects:{"p":"/plan"}`},
		{"an old file", `{"/r/.git":[{"id":"a1","prompt":"Fix","joint":"j","lead":true}]}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Store
			if err := json.Unmarshal([]byte(tt.json), &s); err != nil {
				t.Fatal(err)
			}
			_, task, ok := s.Find("a1")
			if !ok || task.Origin != tt.want || task.Prompt != "Fix" {
				t.Fatalf("task = %+v", task)
			}
			out, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var back Store
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatal(err)
			}
			if _, again, _ := back.Find("a1"); again.Origin != tt.want {
				t.Errorf("after a round trip: %+v", again)
			}
			if tt.want == "" && strings.Contains(string(out), "origin") {
				t.Errorf("a task without an origin writes one: %s", out)
			}
		})
	}
}

func TestValidID(t *testing.T) {
	id := NewID()
	if !ValidID(id) || ValidID("") || ValidID("../x") || ValidID("ABCDEF0123456789") {
		t.Errorf("ValidID wrong for %q", id)
	}
}
