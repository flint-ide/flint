package orchestrate

import "testing"

func TestAllListsNodesInRegistrationOrderAsCopies(t *testing.T) {
	r := newWith(t, Node{ID: "b"}, Node{ID: "a", Parent: "b"}, Node{ID: "c"})
	all := r.All()
	if len(all) != 3 || all[0].ID != "b" || all[1].ID != "a" || all[2].ID != "c" || all[1].Depth != 1 {
		t.Fatalf("All = %+v", all)
	}
	all[0].ID = "changed"
	if n, ok := r.Get("b"); !ok || n.ID != "b" {
		t.Error("All returned the registry's own nodes")
	}
	if got := New().All(); len(got) != 0 {
		t.Errorf("empty registry: %+v", got)
	}
}
