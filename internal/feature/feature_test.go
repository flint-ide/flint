package feature

import (
	"context"
	"slices"
	"strings"
	"testing"

	"flint/internal/mcpserver"
)

func TestRegisterChecksModules(t *testing.T) {
	for name, tc := range map[string]struct {
		mods []Module
		want string
	}{
		"ok":           {[]Module{{ID: "a"}, {ID: "b-2", State: []string{"b"}}}, ""},
		"bad id":       {[]Module{{ID: "Docker"}}, "invalid id"},
		"empty id":     {[]Module{{ID: ""}}, "invalid id"},
		"listed twice": {[]Module{{ID: "a"}, {ID: "a"}}, "listed twice"},
		"shared state": {[]Module{{ID: "a", State: []string{"k"}}, {ID: "b", State: []string{"k"}}}, `state key "k"`},
	} {
		t.Run(name, func(t *testing.T) {
			err := Register(tc.mods)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Register() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Register() = %v, want %q", err, tc.want)
			}
		})
	}
	if got := StateKeys([]Module{{ID: "a", State: []string{"x"}}, {ID: "b"}, {ID: "c", State: []string{"y"}}}); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("StateKeys() = %v", got)
	}
}

func TestSetRunsModulesInListOrder(t *testing.T) {
	var log []string
	mod := func(id string) Module {
		return Module{ID: id, New: func(Host) Instance {
			log = append(log, "new "+id)
			return Instance{
				AgentTools: func() []mcpserver.Tool { return []mcpserver.Tool{{Name: id + "_tool"}} },
				Start:      func(context.Context) { log = append(log, "start "+id) },
				Close:      func() { log = append(log, "close "+id) },
			}
		}}
	}
	s := New([]Module{mod("a"), {ID: "declarations-only"}, mod("b")}, nil)
	s.Start(context.Background())
	for _, c := range s.Closers() {
		c()
	}
	want := []string{"new a", "new b", "start a", "start b", "close a", "close b"}
	if !slices.Equal(log, want) {
		t.Errorf("log = %v, want %v", log, want)
	}
	var names []string
	for _, tool := range s.AgentTools() {
		names = append(names, tool.Name)
	}
	if !slices.Equal(names, []string{"a_tool", "b_tool"}) || len(s.Services()) != 0 {
		t.Errorf("agent tools %v, services %d", names, len(s.Services()))
	}

	var none *Set // an App built without features
	none.Start(context.Background())
	if none.Services() != nil || none.AgentTools() != nil || none.Closers() != nil {
		t.Error("a nil Set contributes something")
	}
}

func TestSetTasksChanged(t *testing.T) {
	calls := 0
	s := New([]Module{
		{ID: "a", New: func(Host) Instance { return Instance{TasksChanged: func() { calls++ }} }},
		{ID: "b", New: func(Host) Instance { return Instance{} }},
	}, nil)
	s.TasksChanged()
	if calls != 1 {
		t.Errorf("TasksChanged ran %d times", calls)
	}
	var none *Set
	none.TasksChanged()
}

func TestSetTaskContext(t *testing.T) {
	var asked []string
	s := New([]Module{
		{ID: "a", New: func(Host) Instance {
			return Instance{TaskContext: func(_ context.Context, st TaskStart) string {
				asked = append(asked, "a:"+st.Origin)
				if strings.HasPrefix(st.Origin, "a:") {
					return "context of " + st.Origin
				}
				return ""
			}}
		}},
		{ID: "b", New: func(Host) Instance { return Instance{} }},
	}, nil)
	tests := []struct {
		origin, want string
		asked        int
	}{
		{"a:item", "context of a:item", 1},
		{"other:x", "", 1},
		{"", "", 0}, // a task without an origin: no feature is asked
	}
	for _, tt := range tests {
		asked = nil
		if got := s.TaskContext(t.Context(), TaskStart{Origin: tt.origin}); got != tt.want || len(asked) != tt.asked {
			t.Errorf("TaskContext(%q) = %q, asked %v", tt.origin, got, asked)
		}
	}
	var none *Set
	if none.TaskContext(t.Context(), TaskStart{Origin: "a:x"}) != "" {
		t.Error("a nil set gave context")
	}
}
