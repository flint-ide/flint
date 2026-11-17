package settings

import (
	"strings"
	"testing"
)

func TestOrchestrationDefaults(t *testing.T) {
	d := Default().Orchestration
	want := Orchestration{Enabled: false, MaxChildren: 4, MaxDepth: 2, MaxWaitSeconds: 600, NotifyParent: true, OutputLines: 200, MaxAutoTurns: 40}
	if d != want {
		t.Errorf("defaults = %+v, want %+v", d, want)
	}
	if err := Validate(Default()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckOrchestration(t *testing.T) {
	for _, o := range []Orchestration{
		defaultOrchestration(),
		{MaxChildren: 1, MaxDepth: 1, MaxWaitSeconds: 1, OutputLines: 1, MaxAutoTurns: 1},
		{MaxChildren: MaxOrchestrationChildren, MaxDepth: MaxOrchestrationDepth, MaxWaitSeconds: MaxOrchestrationWait, OutputLines: MaxOrchestrationOutputLines, MaxAutoTurns: MaxOrchestrationAutoTurns},
	} {
		if err := CheckOrchestration(o); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
	base := defaultOrchestration()
	bad := map[string]func(*Orchestration){
		"children zero": func(o *Orchestration) { o.MaxChildren = 0 },
		"children many": func(o *Orchestration) { o.MaxChildren = MaxOrchestrationChildren + 1 },
		"depth zero":    func(o *Orchestration) { o.MaxDepth = 0 },
		"depth deep":    func(o *Orchestration) { o.MaxDepth = MaxOrchestrationDepth + 1 },
		"wait zero":     func(o *Orchestration) { o.MaxWaitSeconds = 0 },
		"wait long":     func(o *Orchestration) { o.MaxWaitSeconds = MaxOrchestrationWait + 1 },
		"lines zero":    func(o *Orchestration) { o.OutputLines = 0 },
		"lines many":    func(o *Orchestration) { o.OutputLines = MaxOrchestrationOutputLines + 1 },
		"turns zero":    func(o *Orchestration) { o.MaxAutoTurns = 0 },
		"turns many":    func(o *Orchestration) { o.MaxAutoTurns = MaxOrchestrationAutoTurns + 1 },
	}
	for name, mutate := range bad {
		o := base
		mutate(&o)
		if err := CheckOrchestration(o); err == nil || !strings.Contains(err.Error(), "orchestration.") {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	s := Default()
	s.Orchestration.MaxDepth = 99
	if Validate(s) == nil {
		t.Error("Validate accepted maxDepth 99")
	}
}

func TestOrchestrationNormalize(t *testing.T) {
	s, err := decode([]byte(`{"orchestration":{"enabled":true,"maxChildren":99,"maxDepth":-1,"maxWaitSeconds":99999,"outputLines":0,"maxAutoTurns":9999}}`))
	if err != nil {
		t.Fatal(err)
	}
	o := s.Orchestration
	if !o.Enabled || o.MaxChildren != MaxOrchestrationChildren || o.MaxDepth != 1 || o.MaxWaitSeconds != MaxOrchestrationWait || o.OutputLines != 200 || o.MaxAutoTurns != MaxOrchestrationAutoTurns {
		t.Errorf("normalized = %+v", o)
	}
	empty, err := decode([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Orchestration != defaultOrchestration() {
		t.Errorf("empty file = %+v", empty.Orchestration)
	}
}
