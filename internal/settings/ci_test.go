package settings

import (
	"strings"
	"testing"
)

func TestCIDefaults(t *testing.T) {
	d := Default().CI
	if d != (CI{PollSeconds: 30, LogTailLines: 60, LogErrorLines: 40, Notify: true}) {
		t.Errorf("defaults = %+v", d)
	}
}

func TestCheckCI(t *testing.T) {
	good := []CI{
		defaultCI(),
		{PollSeconds: MinCIPollSeconds, LogTailLines: MinCILogTailLines, LogErrorLines: 0},
		{PollSeconds: MaxCIPollSeconds, LogTailLines: MaxCILogTailLines, LogErrorLines: MaxCILogErrorLines},
	}
	for _, c := range good {
		if err := CheckCI(c); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := map[string]CI{
		"poll":   {PollSeconds: MinCIPollSeconds - 1, LogTailLines: 60},
		"slow":   {PollSeconds: MaxCIPollSeconds + 1, LogTailLines: 60},
		"tail":   {PollSeconds: 30, LogTailLines: MinCILogTailLines - 1},
		"long":   {PollSeconds: 30, LogTailLines: MaxCILogTailLines + 1},
		"errors": {PollSeconds: 30, LogTailLines: 60, LogErrorLines: MaxCILogErrorLines + 1},
		"neg":    {PollSeconds: 30, LogTailLines: 60, LogErrorLines: -1},
	}
	for name, c := range bad {
		if err := CheckCI(c); err == nil || !strings.Contains(err.Error(), "CI") {
			t.Errorf("%s: %+v accepted (%v)", name, c, err)
		}
	}
	s := Default()
	s.CI.PollSeconds = 1
	if Validate(s) == nil {
		t.Error("Validate accepted a 1 s CI poll")
	}
}

func TestCINormalize(t *testing.T) {
	s, err := decode([]byte(`{"ci":{"pollSeconds":1,"logTailLines":99999,"logErrorLines":-3,"notify":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := CI{PollSeconds: MinCIPollSeconds, LogTailLines: MaxCILogTailLines, LogErrorLines: 0, Notify: false}
	if s.CI != want {
		t.Errorf("normalized = %+v, want %+v", s.CI, want)
	}
	s, _ = decode([]byte(`{"ci":{"logErrorLines":5}}`))
	if s.CI.PollSeconds != 30 || s.CI.LogTailLines != 60 || s.CI.LogErrorLines != 5 || !s.CI.Notify {
		t.Errorf("missing keys = %+v, want defaults", s.CI)
	}
}
