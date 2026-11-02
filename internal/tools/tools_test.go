package tools

import (
	"slices"
	"testing"
)

func TestInstallForPicksThePlatformCommand(t *testing.T) {
	if got := installFor("darwin", "brew install x", "sudo apt install x"); got != "brew install x" {
		t.Errorf("darwin: %q", got)
	}
	if got := installFor("linux", "brew install x", "sudo apt install x"); got != "sudo apt install x" {
		t.Errorf("linux: %q", got)
	}
}

func TestEveryToolHasAnInstallCommand(t *testing.T) {
	for _, tool := range All() {
		if tool.Install == "" {
			t.Errorf("%s has no install command", tool.ID)
		}
	}
}

func TestRegisteredToolsAreGroupedAndOrdered(t *testing.T) {
	registeredMu.Lock()
	saved := registered
	registeredMu.Unlock()
	t.Cleanup(func() {
		registeredMu.Lock()
		registered = saved
		registeredMu.Unlock()
	})
	Register(Tool{ID: "zz-infra", Name: "Z", Group: GroupInfra, Order: 10, Install: "brew install z"}.Find("z", "--version"))
	Register(Tool{ID: "zz-core", Name: "Y", Group: GroupCore, Install: "brew install y"}.Find("y"))
	var ids []string
	for _, tool := range All() {
		ids = append(ids, tool.ID)
	}
	at := func(id string) int { return slices.Index(ids, id) }
	if !(at("rg") < at("zz-core") && at("zz-core") < at("claude")) {
		t.Errorf("core tool not at the end of its group: %v", ids)
	}
	if !(at("zz-infra") < at("kubectl") && at("kubectl") < at("aws")) {
		t.Errorf("Order not applied within the group: %v", ids)
	}
	if got := All()[at("zz-infra")].Binaries(); !slices.Equal(got, []string{"z"}) {
		t.Errorf("Binaries() = %v", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("a duplicate id was registered")
		}
	}()
	Register(Tool{ID: "git"})
}
