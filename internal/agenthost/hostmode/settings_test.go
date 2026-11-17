package hostmode

import (
	"os"
	"testing"

	"flint/internal/agenthost"
	"flint/internal/settings"
)

func TestHostReadsReplaySizeFromSettings(t *testing.T) {
	dir := t.TempDir()
	if got := hostReplayKiB(dir); got != agenthost.DefaultReplayKiB {
		t.Errorf("no settings.json: %d", got)
	}
	if err := os.WriteFile(settings.Path(dir), []byte(`{"sessions":{"replayKiB":2048}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hostReplayKiB(dir); got != 2048 {
		t.Errorf("replayKiB 2048 read as %d", got)
	}
	if err := os.WriteFile(settings.Path(dir), []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hostReplayKiB(dir); got != agenthost.DefaultReplayKiB {
		t.Errorf("broken settings.json: %d", got)
	}
}

// The setting and the host agree on the bounds, so a valid setting is never
// silently cut by the host.
func TestReplayBoundsMatchTheHost(t *testing.T) {
	if settings.MaxReplayKiB != agenthost.MaxReplayKiB || settings.DefaultReplayKiB != agenthost.DefaultReplayKiB {
		t.Errorf("settings %d/%d, host %d/%d", settings.DefaultReplayKiB, settings.MaxReplayKiB, agenthost.DefaultReplayKiB, agenthost.MaxReplayKiB)
	}
}
