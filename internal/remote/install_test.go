package remote

import (
	"fmt"
	"strings"
	"testing"

	"flint/internal/agenthost"
)

func TestInstallInstructionsAreGeneratedFromTheConstants(t *testing.T) {
	in := Install(testMachine())
	if len(in.Artifacts) != 4 {
		t.Fatalf("artifacts %v", in.Artifacts)
	}
	for _, want := range []string{"flint-host-linux-amd64.tar.gz", "flint-host-linux-arm64.tar.gz", "flint-host-darwin-amd64.tar.gz", "flint-host-darwin-arm64.tar.gz"} {
		if !strings.Contains(strings.Join(in.Artifacts, " "), want) {
			t.Errorf("no artifact %s", want)
		}
	}
	if in.ReleasesURL != ReleasesURL || !strings.Contains(in.Notice, "No release publishes") {
		t.Errorf("notice %q url %q", in.Notice, in.ReleasesURL)
	}
	if in.Command != InstalledCommand {
		t.Errorf("command %q", in.Command)
	}
}

// Only the releases page is named: no URL of a file that may not exist, and
// nothing that fetches or installs on its own.
func TestInstallInstructionsInventNoURLAndRunNothing(t *testing.T) {
	in := Install(testMachine())
	var all strings.Builder
	for _, s := range in.Steps {
		all.WriteString(s.Title + "\n" + s.Command + "\n" + s.Note + "\n")
		for _, banned := range []string{"curl", "wget", "| sh ", "| bash", "sudo"} {
			if strings.Contains(s.Command, banned) {
				t.Errorf("step %q uses %q", s.Title, banned)
			}
		}
	}
	text := all.String()
	if n := strings.Count(text, "https://"); n != 1 || !strings.Contains(text, ReleasesURL) {
		t.Errorf("the steps name %d URLs, want only the releases page:\n%s", n, text)
	}
	if strings.Contains(text, "/releases/download/") {
		t.Error("an invented download URL")
	}
	if !strings.Contains(text, Sha256Placeholder) || !strings.Contains(text, "sha256sum") {
		t.Error("no SHA-256 line")
	}
	if !strings.Contains(text, agenthostLine()) {
		t.Errorf("the version step does not name the protocol:\n%s", text)
	}
}

func agenthostLine() string { return fmt.Sprintf("host-protocol %d", agenthost.Protocol) }
