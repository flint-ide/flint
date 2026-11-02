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
