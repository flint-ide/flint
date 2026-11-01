package todo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"flint/internal/settings"
)

func TestExcludeSetting(t *testing.T) {
	settings.Register(Exclude)
	if got := Exclude.Get(settings.Default()); got == nil || len(got) != 0 {
		t.Fatalf("default = %#v, want []", got)
	}
	var s settings.Settings
	if err := json.Unmarshal([]byte(`{"todoExclude": [" docs ", "docs", "", "!x", "a,b", "gen/"]}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := Exclude.Get(settings.Normalize(s)); !slices.Equal(got, []string{"docs", "gen/"}) {
		t.Errorf("normalized = %q", got)
	}
	for _, bad := range [][]string{{"!src"}, {"a,b"}, {" "}} {
		if err := settings.Validate(Exclude.With(settings.Default(), bad)); err == nil || !strings.Contains(err.Error(), "exclude pattern") {
			t.Errorf("Validate(%q) = %v", bad, err)
		}
	}
	if err := settings.Validate(Exclude.With(settings.Default(), []string{"docs/legacy", "*.min.js", "/gen/"})); err != nil {
		t.Errorf("valid patterns refused: %v", err)
	}
	data, err := json.Marshal(settings.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"todoExclude":[]`) {
		t.Errorf("encoded %s", data)
	}
}
