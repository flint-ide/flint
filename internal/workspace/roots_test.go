package workspace

import (
	"slices"
	"testing"
)

func TestRootOf(t *testing.T) {
	roots := []string{"/w/api", "/w/web"}
	cases := map[string]string{
		"/w/api":            "/w/api",
		"/w/api/main.go":    "/w/api",
		"/w/web/src/app.ts": "/w/web",
		"/w/apix/main.go":   "",
		"/elsewhere":        "",
	}
	for path, want := range cases {
		got, ok := RootOf(roots, path)
		if got != want || ok != (want != "") {
			t.Errorf("RootOf(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
}

func TestCheckNewRoot(t *testing.T) {
	roots := []string{"/w/api", "/w/web"}
	for _, bad := range []string{"/w/api", "/w/api/", "/w/api/internal", "/w", "/", "relative"} {
		if CheckNewRoot(roots, bad) == nil {
			t.Errorf("CheckNewRoot(%q) accepted", bad)
		}
	}
	for _, good := range []string{"/w/apix", "/other/api"} {
		if err := CheckNewRoot(roots, good); err != nil {
			t.Errorf("CheckNewRoot(%q): %v", good, err)
		}
	}
}

func TestLabels(t *testing.T) {
	cases := []struct {
		roots, want []string
	}{
		{[]string{"/w/api"}, []string{"api"}},
		{[]string{"/w/api", "/w/web"}, []string{"api", "web"}},
		{[]string{"/a/app", "/b/app", "/c/web"}, []string{"app (a)", "app (b)", "web"}},
		{[]string{"/x/a/app", "/y/a/app"}, []string{"app (a)", "app 2"}},
	}
	for _, c := range cases {
		if got := Labels(c.roots); !slices.Equal(got, c.want) {
			t.Errorf("Labels(%v) = %v, want %v", c.roots, got, c.want)
		}
	}
}
