package ripgrep

import (
	"path/filepath"
	"testing"
)

func TestLocateEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-rg")
	t.Setenv("FLINT_RG", want)
	if got := locate(); got != want {
		t.Errorf("locate() = %q, want %q", got, want)
	}
}

func TestLocateWithoutPath(t *testing.T) {
	t.Setenv("FLINT_RG", "")
	t.Setenv("PATH", t.TempDir()) // empty PATH: only well-known install locations are probed
	got := locate()
	if got == "" {
		return // rg not installed in any candidate location
	}
	for _, c := range candidates {
		if got == c {
			return
		}
	}
	t.Errorf("locate() = %q, want one of %v", got, candidates)
}

func TestPathIsCached(t *testing.T) {
	first := Path()
	t.Setenv("FLINT_RG", "/changed/after/first/call")
	if got := Path(); got != first {
		t.Errorf("Path() = %q after env change, want cached %q", got, first)
	}
}
