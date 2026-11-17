package agenthost

import (
	"strings"
	"testing"
)

func TestVersionLineRoundTrips(t *testing.T) {
	for _, v := range []string{"v1.2.3", "dev-abc1234+dirty", "0.0.1-rc.1"} {
		got, proto, ok := ParseVersionLine(VersionLine(v))
		if !ok || got != v || proto != Protocol {
			t.Errorf("%q: got %q %d %v", v, got, proto, ok)
		}
	}
	if got, _, ok := ParseVersionLine(VersionLine("")); !ok || got != "unknown" {
		t.Errorf("empty version: %q %v", got, ok)
	}
}

func TestParseVersionLineRefusesOtherText(t *testing.T) {
	for _, line := range []string{
		"", "flint", "flint v1", "flint v1 host-protocol", "flint v1 host-protocol x", "flint v1 host-protocol 1 extra",
		"Flint v1 host-protocol 1", "flint v1 host-protocol 1\nflint v2 host-protocol 2", "flint  v1 host-protocol 1",
		"flint v1 host-protocol 1234567", "flint " + strings.Repeat("a", 129) + " host-protocol 1", "GNU bash, version 5.1",
	} {
		if _, _, ok := ParseVersionLine(line); ok {
			t.Errorf("accepted %q", line)
		}
	}
}
