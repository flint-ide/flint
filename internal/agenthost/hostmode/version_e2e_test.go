package hostmode_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"flint/internal/agenthost"
)

// "flint --version" prints what the machine Check parses, and nothing else.
func TestVersionFlagPrintsVersionAndProtocol(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "--version")
	cmd.Env = append(os.Environ(), "FLINT_TEST_AGENT_HOST=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	line := strings.TrimSpace(string(out))
	version, protocol, ok := agenthost.ParseVersionLine(line)
	if !ok || version != "e2e" || protocol != agenthost.Protocol {
		t.Fatalf("output %q parsed as %q %d %v", out, version, protocol, ok)
	}
	if string(out) != agenthost.VersionLine("e2e")+"\n" {
		t.Errorf("output %q is more than the version line", out)
	}
}
