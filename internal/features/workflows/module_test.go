package workflows

import (
	"testing"

	"flint/internal/feature"
	"flint/internal/feature/featuretest"
)

func TestModule(t *testing.T) {
	if err := feature.Register([]feature.Module{Module}); err != nil {
		t.Fatal(err)
	}
	h := featuretest.New(t.Context())
	set := feature.New([]feature.Module{Module}, h)
	if !set.Has(ID) || len(set.Services()) != 1 {
		t.Fatalf("active %v, %d services", set.Active(), len(set.Services()))
	}
	inst := Module.New(h)
	if inst.TaskContext == nil || inst.TasksChanged == nil || inst.RootsChanged == nil || inst.Start == nil || inst.Close == nil {
		t.Errorf("hooks missing: %+v", inst)
	}
	inst.Start(t.Context())
	inst.TasksChanged()
	inst.RootsChanged()
	if got := set.TaskContext(t.Context(), feature.TaskStart{Origin: "workflow:0123456789abcdef"}); got != "" {
		t.Errorf("an unknown run answered %q", got)
	}
	inst.Close()
}
