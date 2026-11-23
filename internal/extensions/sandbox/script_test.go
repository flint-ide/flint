package sandbox

import (
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
)

// machine is an imaginary file system: the folders and what is in them.
// Every listed path exists and is its own target unless links says
// otherwise.
type machine struct {
	dirs  map[string][]string
	links map[string]string
}

func (m machine) resolve(p string) (string, error) {
	if to, ok := m.links[p]; ok {
		return to, nil
	}
	if p == "/" {
		return p, nil
	}
	if names, ok := m.dirs[path.Dir(p)]; ok && slices.Contains(names, path.Base(p)) {
		return p, nil
	}
	if _, ok := m.dirs[p]; ok {
		return p, nil
	}
	return "", fs.ErrNotExist
}

func (m machine) readDir(dir string) ([]string, error) {
	names, ok := m.dirs[dir]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return names, nil
}

var linuxMachine = machine{
	dirs: map[string][]string{
		"/":                           {"bin", "etc", "home", "opt", "run", "srv", "tmp", "usr", "var"},
		"/home":                       {"me", "other"},
		"/home/me":                    {".aws", ".cache", ".config", ".gitconfig", ".local", ".ssh", "code", "go", "keys-link", "proj-link"},
		"/home/me/.config":            {"flint", "gcloud", "nvim"},
		"/home/me/.config/flint":      {"settings.json"},
		"/home/me/.local":             {"bin", "share"},
		"/home/me/.local/share":       {"fish", "keyrings", "nvim"},
		"/home/me/.local/share/fish":  {"fish_history", "functions"},
		"/home/me/code":               {"app", "app.worktrees"},
		"/home/me/code/app.worktrees": {"fix"},
		"/run":                        {"user"},
		"/run/user":                   {"1000"},
		"/run/user/1000":              {"bus", "flint-1234"},
		"/tmp":                        {"x"},
	},
	links: map[string]string{
		"/bin":               "/usr/bin",
		"/home/me/keys-link": "/home/me/.ssh",
		"/home/me/proj-link": "/srv",
	},
}

func scriptRequest(m machine, change func(r *ScriptRequest)) ScriptRequest {
	r := ScriptRequest{
		GOOS: "linux", Home: "/home/me", ConfigDir: "/home/me/.config/flint", RunDir: "/run/user/1000/flint-1234",
		Worktree: "/home/me/code/app.worktrees/fix", TempDirs: []string{"/tmp"},
		Resolve: m.resolve, ReadDir: m.readDir,
	}
	if change != nil {
		change(&r)
	}
	return r
}

func TestBuildScriptLinux(t *testing.T) {
	p, err := BuildScript(scriptRequest(linuxMachine, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !p.ExecAny || p.Network || p.LocalSockets || p.ExtensionDir != "/home/me/code/app.worktrees/fix" || len(p.Notes) != 0 {
		t.Errorf("policy %+v", p)
	}
	if want := []string{"/home/me/code/app.worktrees/fix", "/tmp", "/dev/tty"}; !slices.Equal(p.Write, want) {
		t.Errorf("Write = %v, want %v", p.Write, want)
	}
	// Everything around the protected places, nothing in them; system
	// folders are LinuxRules' own; links are judged by their targets.
	want := []string{
		"/home/me/.cache", "/home/me/.config/nvim", "/home/me/.gitconfig", "/home/me/.local/bin",
		"/home/me/.local/share/fish/functions", "/home/me/.local/share/nvim", "/home/me/code", "/home/me/go",
		"/srv", "/home/other", "/run/user/1000/bus", "/var",
	}
	if !slices.Equal(p.Read, want) {
		t.Errorf("Read =\n%v\nwant\n%v", p.Read, want)
	}
	for _, secret := range []string{"/home/me/.ssh", "/home/me/.aws", "/home/me/.config/flint", "/home/me/.config/gcloud",
		"/home/me/.local/share/keyrings", "/home/me/.local/share/fish/fish_history", "/run/user/1000/flint-1234", "/home/me", "/"} {
		if containsPath(p.Read, secret) || containsPath(p.Write, secret) {
			t.Errorf("%s is granted", secret)
		}
	}
	rules := p.LinuxRules(6, true)
	if !rules.SocketFilter || len(rules.SocketFamilies) != 0 || rules.HandledNet == 0 {
		t.Errorf("network not closed: %+v", rules)
	}
	for _, rule := range rules.Paths {
		if rule.Path == "/home/me/code" && rule.Access&fsWriteFile != 0 {
			t.Errorf("another folder is writable: %+v", rule)
		}
		if rule.Path == "/home/me/code" && rule.Access&fsExecute == 0 {
			t.Errorf("a readable folder can't be run from: %+v", rule)
		}
	}
}

func TestBuildScriptGrants(t *testing.T) {
	tests := []struct {
		name      string
		change    func(r *ScriptRequest)
		write     []string
		network   bool
		noteParts []string
		err       string
	}{
		{name: "network on opens Internet and local sockets",
			change: func(r *ScriptRequest) { r.Network = true },
			write:  []string{"/home/me/code/app.worktrees/fix", "/tmp", "/dev/tty"}, network: true},
		{name: "extra writable folders, a link judged by its target",
			change: func(r *ScriptRequest) { r.Writable = []string{"/home/me/.cache", "/home/me/proj-link"} },
			write:  []string{"/home/me/code/app.worktrees/fix", "/tmp", "/home/me/.cache", "/srv", "/dev/tty"}},
		{name: "a writable folder that is, holds or sits in a protected place, or is missing, is left out",
			change: func(r *ScriptRequest) {
				r.Writable = []string{"/home/me", "/home/me/.config", "/home/me/.ssh", "/home/me/keys-link", "/home/me/nothing", "/", "relative", "/run/user/1000"}
			},
			write: []string{"/home/me/code/app.worktrees/fix", "/tmp", "/dev/tty"},
			noteParts: []string{
				"/home/me is not granted: it is too wide", "/home/me/.config is not granted: it contains ~/.config/flint",
				"/home/me/.ssh is not granted: it is inside ~/.ssh", "/home/me/keys-link is not granted: it is inside ~/.ssh",
				"/home/me/nothing is not granted: it doesn't exist", "/ is not granted: it is too wide",
				"relative is not granted: it is not an absolute path", "/run/user/1000 is not granted: it contains /run/user/1000/flint-1234",
			}},
		{name: "a temporary folder that holds the host's run directory is left out",
			change: func(r *ScriptRequest) { r.RunDir = "/tmp/x"; r.TempDirs = []string{"/tmp"} },
			write:  []string{"/home/me/code/app.worktrees/fix", "/dev/tty"}, noteParts: []string{"the temporary folder /tmp is not granted: it contains /tmp/x"}},
		{name: "a worktree in a protected place is an error",
			change: func(r *ScriptRequest) { r.Worktree = "/home/me/.config/flint" }, err: "cannot be granted"},
		{name: "a missing worktree is an error",
			change: func(r *ScriptRequest) { r.Worktree = "/home/me/code/gone" }, err: "doesn't exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := BuildScript(scriptRequest(linuxMachine, tt.change))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.Write, tt.write) {
				t.Errorf("Write = %v, want %v", p.Write, tt.write)
			}
			if p.Network != tt.network || p.LocalSockets != tt.network {
				t.Errorf("Network = %v, LocalSockets = %v", p.Network, p.LocalSockets)
			}
			notes := strings.Join(p.Notes, "\n")
			for _, part := range tt.noteParts {
				if !strings.Contains(notes, part) {
					t.Errorf("notes lack %q:\n%s", part, notes)
				}
			}
			if len(tt.noteParts) == 0 && notes != "" {
				t.Errorf("unexpected notes:\n%s", notes)
			}
		})
	}
}

func TestScriptProfileDarwin(t *testing.T) {
	m := machine{dirs: map[string][]string{
		"/Users/me":                    {".ssh", "Library", "code"},
		"/Users/me/code":               {"app.worktrees"},
		"/Users/me/Library":            {"Caches"},
		"/Users/me/code/app.worktrees": {"fix"},
		"/private/tmp":                 {},
		"/private/var/folders/xy/T":    {},
	}, links: map[string]string{"/tmp": "/private/tmp"}}
	config := "/Users/me/Library/Application Support/flint"
	profile := func(network bool) string {
		t.Helper()
		p, err := BuildScript(ScriptRequest{
			GOOS: "darwin", Home: "/Users/me", ConfigDir: config, RunDir: config + "/run",
			Worktree: "/Users/me/code/app.worktrees/fix", TempDirs: []string{"/tmp", "/private/var/folders/xy/T"},
			Writable: []string{"/Users/me/Library/Caches"}, Network: network,
			Resolve: m.resolve, ReadDir: func(string) ([]string, error) { t.Fatal("macOS lists no folder"); return nil, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Read) != 0 {
			t.Errorf("Read = %v: the profile reads everything but the protected places", p.Read)
		}
		s, err := p.DarwinProfile()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	off := profile(false)
	for _, part := range []string{
		"(allow default)\n(deny file-write*)\n(allow file-write*\n  (subpath \"/Users/me/code/app.worktrees/fix\")\n  (subpath \"/private/tmp\")\n  (subpath \"/private/var/folders/xy/T\")\n  (subpath \"/Users/me/Library/Caches\")\n  (subpath \"/dev/tty\"))\n",
		"(deny file-read-data\n  (subpath \"/Users/me/Library/Application Support/flint\")\n  (subpath \"/Users/me/.ssh\")",
		"(subpath \"/Users/me/Library/Keychains\")", "(subpath \"/Library/Keychains\")",
		"(subpath \"/Users/me/Library/Application Support/flint/run\")",
		"(deny network*)",
		"(deny signal)",
	} {
		if !strings.Contains(off, part) {
			t.Errorf("profile lacks %q:\n%s", part, off)
		}
	}
	for _, part := range []string{"(deny process-exec*)", "(subpath \"/Users\")", "(subpath \"/private/var/folders\")"} {
		if strings.Contains(off, part) {
			t.Errorf("profile has %q: a script runs programs and reads the user's files:\n%s", part, off)
		}
	}
	// The protected places are closed before the written folders open again.
	if strings.Index(off, "(deny file-read-data") > strings.Index(off, "(allow file-read-data") {
		t.Errorf("reads are opened before the protected places are closed:\n%s", off)
	}
	if on := profile(true); strings.Contains(on, "(deny network*)") {
		t.Errorf("network on is denied:\n%s", on)
	}
}
