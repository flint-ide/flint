package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "sessions"))
	if got, err := s.Load("/p/a"); err != nil || got != "" {
		t.Fatalf("empty store: %q, %v", got, err)
	}
	if err := s.Save("/p/a", `{"tabs":[1,2]}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("/p/b", `{"tabs":[]}`); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Load("/p/a"); got != `{"tabs":[1,2]}` {
		t.Errorf("a = %q", got)
	}
	if got, _ := s.Load("/p/b"); got != `{"tabs":[]}` {
		t.Errorf("b = %q", got)
	}
	info, err := os.Stat(s.path("/p/a"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, %v", info.Mode(), err)
	}
	if err := s.Forget("/p/a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Load("/p/a"); got != "" {
		t.Errorf("after Forget = %q", got)
	}
	if err := s.Forget("/p/a"); err != nil {
		t.Errorf("second Forget: %v", err)
	}
}

func TestSaveRejectsBadInput(t *testing.T) {
	s := New(t.TempDir())
	for name, tc := range map[string][2]string{
		"relative root": {"p", `{}`},
		"empty root":    {"", `{}`},
		"invalid json":  {"/p", `{`},
		"too large":     {"/p", `"` + strings.Repeat("x", MaxSize) + `"`},
	} {
		if err := s.Save(tc[0], tc[1]); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadIgnoresOtherRoot(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Save("/p/a", `{}`); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.path("/p/a"))
	if err := os.WriteFile(s.path("/p/a"), []byte(strings.Replace(string(data), `"/p/a"`, `"/p/other"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load("/p/a"); err != nil || got != "" {
		t.Errorf("foreign file: %q, %v", got, err)
	}
}
