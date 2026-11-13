package extensions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstallArg(t *testing.T) {
	parent := t.TempDir()
	ext := filepath.Join(parent, "ext")
	if err := os.Mkdir(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		args    []string
		dir     string
		found   bool
		wantErr string
	}{
		{"absent", []string{"project"}, "", false, ""},
		{"no arguments", nil, "", false, ""},
		{"relative to the caller's folder", []string{"--install-extension", "ext"}, ext, true, ""},
		{"absolute", []string{"-psn_0_1", "--install-extension", ext, "project"}, ext, true, ""},
		{"with =", []string{"--install-extension=" + ext}, ext, true, ""},
		{"first one counts", []string{"--install-extension", "ext", "--install-extension", "file"}, ext, true, ""},
		{"no value", []string{"--install-extension"}, "", true, "needs a folder"},
		{"empty value", []string{"--install-extension="}, "", true, "needs a folder"},
		{"a file", []string{"--install-extension", "file"}, "", true, "is not a folder"},
		{"missing", []string{"--install-extension", "nope"}, "", true, "is not a folder"},
		{"another flag as value", []string{"--install-extension", "--help"}, "", true, "is not a folder"},
		{"similar flag", []string{"--install-extensions", "ext"}, "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, found, err := InstallArg(c.args, parent)
			if dir != c.dir || found != c.found {
				t.Errorf("InstallArg = %q, %v, %v", dir, found, err)
			}
			if (c.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Errorf("err = %v, want %q", err, c.wantErr)
			}
			req, ok := RequestOf(c.args, parent)
			if ok != c.found || req.Dir != c.dir || (req.Error == "") != (c.wantErr == "") {
				t.Errorf("RequestOf = %+v, %v", req, ok)
			}
		})
	}
}

func TestWithoutInstallArg(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"a", "--install-extension", "ext", "b"}, []string{"a", "b"}},
		{[]string{"--install-extension=ext", "b"}, []string{"b"}},
		{[]string{"a", "--install-extension"}, []string{"a"}},
		{[]string{"-x", "a"}, []string{"-x", "a"}},
	}
	for _, c := range cases {
		if got := WithoutInstallArg(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("WithoutInstallArg(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPendingInstallIsHandedOutOnce(t *testing.T) {
	a := &API{pending: &InstallRequest{Dir: "/x"}}
	if got := a.PendingInstall(); got.Dir != "/x" {
		t.Errorf("first = %+v", got)
	}
	if got := a.PendingInstall(); got != (InstallRequest{}) {
		t.Errorf("second = %+v", got)
	}
}
