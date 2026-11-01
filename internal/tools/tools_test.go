package tools

import "testing"

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
