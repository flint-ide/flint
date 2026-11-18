package claude

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/agent"
	"flint/internal/agentsession"
)

// addDirs returns the values of every --add-dir in args, and where the
// first one is.
func addDirs(args []string) (dirs []string, first int) {
	first = -1
	for i, a := range args {
		if a == "--add-dir" && i+1 < len(args) {
			if first < 0 {
				first = i
			}
			dirs = append(dirs, args[i+1])
		}
	}
	return dirs, first
}

// Flint's additional folders are one --add-dir pair each, among Flint's own
// flags (after the user's), on start and on resume, and always followed by
// another flag.
func TestBuildArgsFolders(t *testing.T) {
	folders := []string{"/wt/web", "/wt/docs dir"}
	user := Options{Settings: func() agent.Config { return agent.Config{Args: []string{"--add-dir", "/mine"}} }}.current()
	for _, c := range []struct {
		name    string
		resume  bool
		opts    Options
		folders []string
		want    []string
	}{
		{"start", false, Options{}, folders, folders},
		{"resume", true, Options{}, folders, folders},
		{"none", false, Options{}, nil, nil},
		{"next to the user's own", false, user, folders, append([]string{"/mine"}, folders...)},
	} {
		args, err := buildArgs("sid", c.resume, c.opts, false, nil, c.folders)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		dirs, _ := addDirs(args)
		if !slices.Equal(dirs, c.want) {
			t.Errorf("%s: --add-dir %v, want %v (args %q)", c.name, dirs, c.want, args)
		}
		p := slices.Index(args, "-p")
		for i, a := range args {
			if a != "--add-dir" || !slices.Contains(c.folders, args[i+1]) {
				continue
			}
			if i < p {
				t.Errorf("%s: Flint's --add-dir before its first flag: %q", c.name, args)
			}
			if i+2 >= len(args) || !strings.HasPrefix(args[i+2], "-") {
				t.Errorf("%s: Flint's --add-dir %s is not followed by a flag: %q", c.name, args[i+1], args)
			}
		}
	}
}

// The driver takes the folders from the context it starts with, and passes
// them to the CLI as separate arguments.
func TestStartWithFolders(t *testing.T) {
	opts, logPath := fakeOptions(t, "text")
	d := New(opts)
	t.Cleanup(func() { _ = d.Close() })
	other, third := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := d.Start(agentsession.WithFolders(ctx, []string{other, third}), newWorkspace(t), (&recorder{}).sink)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{d: d, info: info, log: logPath}
	if err := d.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	var args []string
	_ = json.Unmarshal(h.fakeLog(t)[0]["args"], &args)
	if dirs, _ := addDirs(args); !slices.Equal(dirs, []string{other, third}) {
		t.Fatalf("args %q", args)
	}
}

// Folders that are not absolute, not there, twice, or not trusted (nor a
// cwd that is not) refuse the start before any process runs.
func TestStartRefusesBadFolders(t *testing.T) {
	cwd, ok := newWorkspace(t), t.TempDir()
	untrusted := t.TempDir()
	for _, c := range []struct {
		name      string
		folders   []string
		untrusted string
	}{
		{"relative", []string{"web"}, ""},
		{"not clean", []string{ok + "/../x"}, ""},
		{"missing", []string{ok + "/missing"}, ""},
		{"twice", []string{ok, ok}, ""},
		{"the cwd", []string{cwd}, ""},
		{"untrusted folder", []string{ok, untrusted}, untrusted},
		{"untrusted cwd", []string{ok}, cwd},
	} {
		d := New(Options{Binary: "/nonexistent/claude", Untrusted: func(p string) bool { return p == c.untrusted }})
		_, err := d.Start(agentsession.WithFolders(context.Background(), c.folders), cwd, (&recorder{}).sink)
		if err == nil || errors.Is(err, ErrClosed) || strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}
