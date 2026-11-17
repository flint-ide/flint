package sandbox

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// The background host's run directory holds its socket and token. A sandboxed
// extension runs as the same user, so the token's secrecy is what keeps it
// out: nothing may grant the directory, whatever the manifest says.
func TestRunDirIsNeverGranted(t *testing.T) {
	const run = "/run/user/1000/flint-ab12cd34"
	tests := []struct {
		name  string
		read  []string
		write []string
		want  []string // paths that must be refused, named in the notes
	}{
		{"the directory itself, named by the manifest", []string{run}, []string{run}, []string{run}},
		{"a file inside it, named by the manifest", []string{run + "/host.token"}, nil, []string{"host.token"}},
		{"a folder that holds it", []string{"/run/user/1000", "/run/user"}, []string{"/run/user/1000"}, []string{"/run/user/1000", "/run/user"}},
		{"the runtime folder variable", []string{"${XDG_RUNTIME_DIR}"}, nil, []string{"${XDG_RUNTIME_DIR}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Build(request(func(r *Request) {
				r.RunDir = run
				r.Read, r.Write = tt.read, tt.write
				env := r.Env
				r.Env = func(name string) string {
					if name == "XDG_RUNTIME_DIR" {
						return "/run/user/1000"
					}
					return env(name)
				}
			}))
			for _, list := range [][]string{p.Read, p.Write} {
				for _, granted := range list {
					if within(granted, run) || within(run, granted) {
						t.Errorf("%s was granted (read %v, write %v)", granted, p.Read, p.Write)
					}
				}
			}
			notes := strings.Join(p.Notes, "\n")
			for _, want := range tt.want {
				if !strings.Contains(notes, want+" is not granted") {
					t.Errorf("no note about %s:\n%s", want, notes)
				}
			}
		})
	}
}

func TestRunDirDoesNotBlockItsNeighbours(t *testing.T) {
	p := Build(request(func(r *Request) {
		r.RunDir = "/run/user/1000/flint-ab12cd34"
		r.Read = []string{"/run/user/1000/bus", "/run/user/1000/flint-other"}
	}))
	if !slices.Equal(p.Read, []string{"/run/user/1000/bus", "/run/user/1000/flint-other"}) || len(p.Notes) != 0 {
		t.Errorf("Read = %v, notes %v", p.Read, p.Notes)
	}
}

// The folder is protected where a link leads, too: /tmp is /private/tmp on
// macOS, and the host may fall back to /tmp.
func TestRunDirIsProtectedThroughLinks(t *testing.T) {
	links := map[string]string{"/tmp": "/private/tmp", "/tmp/flint-501-ab12cd34": "/private/tmp/flint-501-ab12cd34"}
	p := Build(request(func(r *Request) {
		r.GOOS, r.Home, r.ConfigDir = "darwin", "/Users/me", "/Users/me/Library/Application Support/flint"
		r.ExtensionDir = "/Users/me/Library/Application Support/flint/extensions/acme.ext"
		r.DataDir = "/Users/me/Library/Application Support/flint/extension-data/acme.ext"
		r.TempDir = "/private/var/folders/ab/T/flint-ext-1"
		r.RunDir = "/tmp/flint-501-ab12cd34"
		r.Resolve = func(path string) (string, error) {
			if to, ok := links[path]; ok {
				return to, nil
			}
			if path == "/missing" {
				return "", fs.ErrNotExist
			}
			return path, nil
		}
		r.Read = []string{"/tmp", "/private/tmp/flint-501-ab12cd34/host.token"}
	}))
	if len(p.Read) != 0 {
		t.Errorf("Read = %v", p.Read)
	}
	profile, err := p.DarwinProfile()
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/tmp/flint-501-ab12cd34", "/private/tmp/flint-501-ab12cd34"} {
		if !strings.Contains(profile, `(subpath "`+dir+`")`) {
			t.Errorf("the macOS profile does not close %s:\n%s", dir, profile)
		}
	}
}

func TestRunDirIsOptional(t *testing.T) {
	p := Build(request(nil))
	for _, c := range p.Protected {
		if c == "" {
			t.Errorf("an empty protected path in %v", p.Protected)
		}
	}
}
