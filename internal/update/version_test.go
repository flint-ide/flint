package update

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b   string
		want   int
		wantOK bool
	}{
		{"v1.2.3", "v1.2.3", 0, true},
		{"v1.2.3", "1.2.3", 0, true},
		{"v1.2.4", "v1.2.3", 1, true},
		{"v1.3.0", "v1.2.9", 1, true},
		{"v2.0.0", "v1.99.99", 1, true},
		{"v1.10.0", "v1.9.0", 1, true}, // numeric, not lexical
		{"v1.2.3", "v1.2.4", -1, true},
		{"v1.2.0-rc.1", "v1.2.0", -1, true},
		{"v1.2.0", "v1.2.0-rc.1", 1, true},
		{"v1.2.0-rc.2", "v1.2.0-rc.1", 1, true},
		{"v1.2.0-rc.10", "v1.2.0-rc.9", 1, true},
		{"v1.2.0-alpha", "v1.2.0-beta", -1, true},
		{"v1.2.0-1", "v1.2.0-alpha", -1, true}, // numbers sort below words
		{"v1.2.0-alpha.1", "v1.2.0-alpha", 1, true},
		{"v1.2.0-rc.1", "v1.1.9", 1, true},
		{"v1.2.3+build.5", "v1.2.3", 0, true},
		{"dev", "v1.0.0", 0, false},
		{"v1.0.0", "dev-0123456789ab", 0, false},
		{"dev-0123456789ab+dirty", "v1.0.0", 0, false},
		{"v1.2", "v1.2.0", 0, false},
		{"v1.2.3.4", "v1.2.3", 0, false},
		{"v01.2.3", "v1.2.3", 0, false},
		{"v1.2.3-", "v1.2.3", 0, false},
		{"v1.2.3-rc..1", "v1.2.3", 0, false},
		{"v-1.2.3", "v1.2.3", 0, false},
		{"", "v1.2.3", 0, false},
		{"v99999999999999999999.0.0", "v1.0.0", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got, ok := Compare(tt.a, tt.b)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", tt.a, tt.b, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsRelease(t *testing.T) {
	tests := map[string]bool{
		"v1.0.0":            true,
		"v0.4.2-rc.1":       true,
		"dev":               false,
		"dev-0123456789ab":  false,
		"dev-abc+dirty":     false,
		"v1.0":              false,
		"latest":            false,
		"v1.0.0-rc.1 extra": false,
		"v1.0.0\nv2.0.0":    false,
		"refs/tags/v1.0.0":  false,
		"v1.0.0-🙂":          false,
		"v1.0.0-rc-1.x-y":   true,
		"v1.0.0-rc.1.2.3.4": true,
		"v1.0.0+build.7":    true,
		"v1.0.0+":           false,
		"v1.0.0+<b>":        false,
	}
	for v, want := range tests {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}
