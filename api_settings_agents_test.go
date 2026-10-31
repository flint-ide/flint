package main

import (
	"errors"
	"strings"
	"testing"

	"flint/internal/acp/acptest"
	"flint/internal/settings"
)

func TestProbeCustomAgentNeedsConfirmation(t *testing.T) {
	cmd := useFakeAgent(t, acptest.ModeEcho)
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	a.settings.CustomAgents = []settings.CustomAgent{{ID: "fake", Command: cmd, Args: []string{"--acp"}}}
	var asked []string
	answer := false
	a.ask = func(_, message, _ string) bool {
		asked = append(asked, message)
		return answer
	}
	api := &SettingsAPI{app: a}

	if _, err := api.ProbeCustomAgent("fake"); !errors.Is(err, errNotConfirmed) {
		t.Fatalf("declined Check = %v, want errNotConfirmed", err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], commandLine(cmd, []string{"--acp"})) {
		t.Fatalf("dialog messages = %q, want the command line", asked)
	}
	answer = true
	if info, err := api.ProbeCustomAgent("fake"); err != nil || info.Name != "fake-agent" {
		t.Fatalf("confirmed Check = %+v, %v", info, err)
	}
	// Confirmed once per session; a changed command line asks again.
	if _, err := api.ProbeCustomAgent("fake"); err != nil || len(asked) != 2 {
		t.Fatalf("second Check = %v after %d questions, want no new question", err, len(asked))
	}
	a.settings.CustomAgents[0].Args = []string{"--acp", "--other"}
	if _, err := api.ProbeCustomAgent("fake"); err != nil || len(asked) != 3 {
		t.Fatalf("Check with new arguments = %v after %d questions, want a new question", err, len(asked))
	}
}

func TestCommandLine(t *testing.T) {
	// A Cyrillic "a" looks like a Latin one; it is shown escaped.
	cyrillicA := string(rune(0x430))
	got := commandLine("/bin/sh", []string{"-c", "curl x | sh", "it's", "a\nb", "", "--model=gemini-2.5", cyrillicA})
	want := `/bin/sh -c 'curl x | sh' 'it'\''s' "a\nb" '' --model=gemini-2.5 "` + "\x5cu0430" + `"`
	if got != want {
		t.Errorf("commandLine = %s\nwant          %s", got, want)
	}
}
