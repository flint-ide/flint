package vars

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		s     string
		tests bool
		want  string // "" = no error
	}{
		{"${file}", false, ""},
		{"--root=${workspaceFolder}/x ${fileDirname} ${fileBasename}", false, ""},
		{"plain $HOME and ${", false, ""},
		{"${tests}", true, ""},
		{"${tests}", false, "only for test runner commands"},
		{"--name=${tests}", true, "argument of its own"},
		{"${env:HOME}", false, "unknown variable ${env:HOME}"},
		{"${File}", false, "unknown variable"},
	} {
		err := Check(tc.s, tc.tests)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("Check(%q, %v) = %v, want %q", tc.s, tc.tests, err, tc.want)
		}
	}
}

func TestExpandArgs(t *testing.T) {
	v := ForFile("/p", "/p/spec/a b_spec.rb")
	args := []string{"exec", "rspec", "${file}", "${tests}", "--dir=${fileDirname}", "${fileBasename}", "$(rm -rf /)"}
	got := ExpandArgs(args, v, []string{"adds", "it; rm"})
	want := []string{"exec", "rspec", "/p/spec/a b_spec.rb", "adds", "it; rm", "--dir=/p/spec", "a b_spec.rb", "$(rm -rf /)"}
	if !slices.Equal(got, want) {
		t.Errorf("ExpandArgs = %q\nwant %q", got, want)
	}
	if got := ExpandArgs([]string{"x", "${tests}"}, v, nil); !slices.Equal(got, []string{"x"}) {
		t.Errorf("no tests: %q", got)
	}
}

func TestExpandJSON(t *testing.T) {
	raw := json.RawMessage(`{"program":"${file}","cwd":"${workspaceFolder}","args":["${fileBasename}",1],"port":12345678901234567,"${file}":true,"nested":{"x":"${fileDirname}"}}`)
	out, err := ExpandJSON(raw, ForFile("/p", "/p/lib/a.rb"), 8)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"${file}":true,"args":["a.rb",1],"cwd":"/p","nested":{"x":"/p/lib"},"port":12345678901234567,"program":"/p/lib/a.rb"}`
	if string(out) != want {
		t.Errorf("ExpandJSON = %s\nwant %s", out, want)
	}
	if _, err := ExpandJSON(json.RawMessage(`{"a":{"b":{"c":"x"}}}`), nil, 2); err == nil {
		t.Error("too deep accepted")
	}
}
