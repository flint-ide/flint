package shellenv

import "testing"

func TestParseMarker(t *testing.T) {
	tests := map[string]string{
		"motd noise\n" + marker + "/opt/homebrew/bin:/usr/bin\n": "/opt/homebrew/bin:/usr/bin",
		marker + "/a:/b":                     "/a:/b",
		"no marker here":                     "",
		marker + "/x\n" + marker + "/y\nend": "/y",
	}
	for in, want := range tests {
		if got := ParseMarker(in); got != want {
			t.Errorf("ParseMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergePaths(t *testing.T) {
	got := mergePaths("/opt/homebrew/bin:/usr/bin", "/usr/bin:/bin:")
	if want := "/opt/homebrew/bin:/usr/bin:/bin"; got != want {
		t.Errorf("mergePaths = %q, want %q", got, want)
	}
}

func TestWaitResolves(t *testing.T) {
	Wait() // must return (bounded by the timeout) even in CI shells
}
