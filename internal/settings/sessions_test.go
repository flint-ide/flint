package settings

import (
	"strings"
	"testing"
)

func TestSessionsDefaults(t *testing.T) {
	if got, want := Default().Sessions, (Sessions{Background: false, ReplayKiB: 1024}); got != want {
		t.Errorf("defaults = %+v, want %+v", got, want)
	}
}

func TestCheckSessions(t *testing.T) {
	for _, kib := range []int{MinReplayKiB, DefaultReplayKiB, MaxReplayKiB} {
		if err := CheckSessions(Sessions{ReplayKiB: kib}); err != nil {
			t.Errorf("%d: %v", kib, err)
		}
	}
	for _, kib := range []int{0, -1, MinReplayKiB - 1, MaxReplayKiB + 1} {
		if err := CheckSessions(Sessions{ReplayKiB: kib}); err == nil || !strings.Contains(err.Error(), "sessions.replayKiB") {
			t.Errorf("%d accepted (%v)", kib, err)
		}
	}
	s := Default()
	s.Sessions.ReplayKiB = 99999
	if Validate(s) == nil {
		t.Error("Validate accepted replayKiB 99999")
	}
}

func TestSessionsNormalize(t *testing.T) {
	for in, want := range map[string]Sessions{
		`{}`:                               {ReplayKiB: DefaultReplayKiB},
		`{"sessions":{"background":true}}`: {Background: true, ReplayKiB: DefaultReplayKiB},
		`{"sessions":{"background":true,"replayKiB":1}}`:     {Background: true, ReplayKiB: MinReplayKiB},
		`{"sessions":{"replayKiB":100000}}`:                  {ReplayKiB: MaxReplayKiB},
		`{"sessions":{"replayKiB":2048}}`:                    {ReplayKiB: 2048},
		`{"sessions":{"background":"yes","replayKiB":2048}}`: {ReplayKiB: 2048},
	} {
		s, err := decode([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if s.Sessions != want {
			t.Errorf("%s: %+v, want %+v", in, s.Sessions, want)
		}
	}
}
