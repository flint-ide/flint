package settings

import (
	"slices"
	"testing"
)

func TestWatchExclude(t *testing.T) {
	if d := Default().WatchExclude; !slices.Contains(d, "dist") {
		t.Errorf("default watch excludes = %v", d)
	}
	s, err := decode([]byte(`{}`))
	if err != nil || !slices.Equal(s.WatchExclude, defaultWatchExclude()) {
		t.Errorf("missing key: %v, %v", s.WatchExclude, err)
	}
	s, _ = decode([]byte(`{"watchExclude": []}`))
	if len(s.WatchExclude) != 0 {
		t.Errorf("an empty list must stay empty (watch everything): %v", s.WatchExclude)
	}

	for _, bad := range []string{"a/b", "!dist", "[", " "} {
		s := Default()
		s.WatchExclude = []string{bad}
		if err := Validate(s); err == nil {
			t.Errorf("Validate accepted %q", bad)
		}
	}
	got := cleanWatchExcludes([]string{" out ", "out", "a/b", "*.egg-info"})
	if !slices.Equal(got, []string{"out", "*.egg-info"}) {
		t.Errorf("cleanWatchExcludes = %v", got)
	}
	if !WatchExcluded([]string{"*.egg-info"}, "foo.egg-info") || WatchExcluded([]string{"dist"}, "src") {
		t.Error("WatchExcluded")
	}
}
