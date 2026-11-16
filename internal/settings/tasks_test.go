package settings

import (
	"strings"
	"testing"
)

func TestAgentTasksDefaults(t *testing.T) {
	d := Default().AgentTasks
	if d.WorktreeDir != "" || d.BranchPrefix != "agent/" || d.MaxParallel != 3 {
		t.Errorf("defaults = %+v", d)
	}
	if err := Validate(Default()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAgentTasks(t *testing.T) {
	good := []AgentTasks{
		{},
		{BranchPrefix: "agent/"},
		{BranchPrefix: "me/tasks-"},
		{WorktreeDir: "~/worktrees", BranchPrefix: "x/"},
		{WorktreeDir: "/Volumes/fast/wt"},
		{MaxParallel: 0},
		{MaxParallel: 32},
	}
	for _, a := range good {
		if err := CheckAgentTasks(a); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []AgentTasks{
		{WorktreeDir: "relative/dir"},
		{WorktreeDir: "~other/dir"},
		{WorktreeDir: "/tmp/a\nb"},
		{WorktreeDir: "/" + strings.Repeat("a", 1100)},
		{BranchPrefix: "-x/"},
		{BranchPrefix: "/x"},
		{BranchPrefix: "a..b/"},
		{BranchPrefix: "a b/"},
		{BranchPrefix: "a//b"},
		{BranchPrefix: "x.lock/"},
		{BranchPrefix: "a~b"},
		{BranchPrefix: strings.Repeat("a", 41)},
		{MaxParallel: -1},
		{MaxParallel: 33},
	}
	for _, a := range bad {
		if CheckAgentTasks(a) == nil {
			t.Errorf("%+v accepted", a)
		}
	}
}

func TestAgentTasksNormalize(t *testing.T) {
	s, err := decode([]byte(`{"agentTasks":{"worktreeDir":"relative","branchPrefix":"ok/"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.AgentTasks.WorktreeDir != "" || s.AgentTasks.BranchPrefix != "ok/" {
		t.Errorf("normalized = %+v", s.AgentTasks)
	}
	s, _ = decode([]byte(`{"agentTasks":{"branchPrefix":"bad prefix"}}`))
	if s.AgentTasks.BranchPrefix != "agent/" {
		t.Errorf("bad prefix kept: %+v", s.AgentTasks)
	}
	// A file from before the queue keeps its values and gets the default limit;
	// an explicit 0 (no limit) stays, a value out of range is defaulted.
	s, _ = decode([]byte(`{"agentTasks":{"branchPrefix":"ok/"}}`))
	if s.AgentTasks.MaxParallel != 3 || s.AgentTasks.BranchPrefix != "ok/" {
		t.Errorf("old file = %+v", s.AgentTasks)
	}
	s, _ = decode([]byte(`{"agentTasks":{"maxParallel":0}}`))
	if s.AgentTasks.MaxParallel != 0 {
		t.Errorf("no limit = %+v", s.AgentTasks)
	}
	s, _ = decode([]byte(`{"agentTasks":{"maxParallel":500}}`))
	if s.AgentTasks.MaxParallel != 3 {
		t.Errorf("out of range = %+v", s.AgentTasks)
	}
	// An older file without the key gets the defaults.
	s, _ = decode([]byte(`{"theme":"dark"}`))
	if s.AgentTasks != Default().AgentTasks {
		t.Errorf("missing key = %+v", s.AgentTasks)
	}
}
