package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
)

// The second kind of policy: a workflow's script step (a run configuration
// run on code an agent wrote, docs/design/workflows.md, "Sandbox"). An
// extension gets only what it declares; a script runs the user's own
// toolchain (compilers, package managers and their global caches), so it
// may read what the user can read except the places ProtectedPaths lists,
// write only its worktree, the temporary folders and the folders the user
// added, and use the network only when the user allows it.

// scriptReadDepth bounds how deep BuildScript looks for a protected place on
// Linux (the deepest one, ~/.local/share/fish/fish_history, is a few levels
// under the home folder, itself a few levels under /).
const scriptReadDepth = 16

// ScriptRequest is what Flint knows when it is about to run a script step
// confined.
type ScriptRequest struct {
	// GOOS is "linux" or "darwin".
	GOOS string
	// Home is the user's home folder; ConfigDir Flint's own; RunDir the
	// background session host's run directory ("" when there is none).
	Home      string
	ConfigDir string
	RunDir    string
	// Worktree is the agent task's worktree: read, written and run. It must
	// exist and may not be or hold a protected place.
	Worktree string
	// TempDirs are the system's temporary folders and Writable the folders
	// the user added (workflows.sandboxWritable): written when they exist
	// and are not, hold or sit inside a protected place.
	TempDirs []string
	Writable []string
	// Network: Internet and local sockets may be opened.
	Network bool
	// Resolve resolves symbolic links (filepath.EvalSymlinks); an error
	// means the path doesn't exist. ReadDir lists the names in a folder,
	// sorted; it is used on Linux only, where Landlock can only allow, to
	// grant everything around the protected places.
	Resolve func(path string) (string, error)
	ReadDir func(dir string) ([]string, error)
}

// scriptDevices may be written by a script besides its folders: the
// terminal it runs in, for programs that open it to ask or to draw.
var scriptDevices = []string{"/dev/tty"}

// BuildScript turns a script step's request into a policy. Everything may be
// run (a build runs what it builds); nothing may be written outside the
// worktree, the temporary folders, the user's folders and the terminal; the
// protected places (keys, credentials, history, Flint's own folder, the
// background host's run directory) cannot be read. On macOS the profile
// allows reading everything else and closes the protected places; on Linux
// the policy lists everything readable around them. A request whose
// worktree cannot be granted is an error; a temporary or user folder that
// cannot be is left out with a note.
func BuildScript(r ScriptRequest) (Policy, error) {
	denied := r.protected()
	p := Policy{
		GOOS: r.GOOS, ExecAny: true, Network: r.Network, LocalSockets: r.Network,
		Protected: denied,
	}
	check := func(real string) string { return refuseScript(real, r.Home, denied) }
	if r.Worktree == "" || !path.IsAbs(r.Worktree) {
		return Policy{}, errors.New("sandbox: the worktree is not an absolute path")
	}
	worktree, err := r.Resolve(path.Clean(r.Worktree))
	if err != nil {
		return Policy{}, fmt.Errorf("sandbox: the worktree %s doesn't exist", r.Worktree)
	}
	if why := check(worktree); why != "" {
		return Policy{}, fmt.Errorf("sandbox: the worktree %s cannot be granted: %s", r.Worktree, why)
	}
	p.ExtensionDir = worktree
	p.Write = []string{worktree}
	p.DenyWrite = worktreeDenyWrite(worktree)
	grant := func(what, dir string) {
		if !path.IsAbs(dir) {
			p.Notes = append(p.Notes, fmt.Sprintf("%s %s is not granted: it is not an absolute path", what, dir))
			return
		}
		real, err := r.Resolve(path.Clean(dir))
		switch {
		case err != nil:
			p.Notes = append(p.Notes, fmt.Sprintf("%s %s is not granted: it doesn't exist", what, dir))
		case check(real) != "":
			p.Notes = append(p.Notes, fmt.Sprintf("%s %s is not granted: %s", what, dir, check(real)))
		case !containsPath(p.Write, real):
			p.Write = append(p.Write, real)
		}
	}
	for _, dir := range r.TempDirs {
		grant("the temporary folder", dir)
	}
	for _, dir := range r.Writable {
		grant("the folder", dir)
	}
	p.Write = append(p.Write, scriptDevices...)
	if r.GOOS == "linux" {
		p.Read = readableAround("/", denied, r, scriptReadDepth)
		p.Read = slices.DeleteFunc(p.Read, func(s string) bool { return containsPath(p.Write, s) })
	}
	return p, nil
}

// protected are the places a script may not read: ProtectedPaths (with the
// system's keychains on macOS) and the background host's run directory,
// each as given and where its links lead.
func (r ScriptRequest) protected() []string {
	list := ProtectedPaths(r.GOOS, r.Home, r.ConfigDir)
	if r.GOOS == "darwin" {
		list = append(list, "/Library/Keychains")
	}
	if r.RunDir != "" {
		list = append(list, path.Clean(r.RunDir))
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
		if real, err := r.Resolve(p); err == nil && !slices.Contains(out, real) {
			out = append(out, real)
		}
	}
	return out
}

// refuseScript says why a script may not write real; "" when it may. Every
// protected place is denied, so a folder that is one, sits inside one or
// holds one is refused (Landlock has no "except", and the macOS profile
// would open what it holds again).
func refuseScript(real, home string, denied []string) string {
	if real == "/" || real == home {
		return "it is too wide; name the folders the script needs"
	}
	for _, d := range denied {
		switch {
		case within(d, real):
			return fmt.Sprintf("it is inside %s, which a script may not read", tilde(d, home))
		case within(real, d):
			return fmt.Sprintf("it contains %s; name the folders the script needs", tilde(d, home))
		}
	}
	return ""
}

// readableAround lists what may be read so that everything under dir is
// readable except the denied places: each entry of dir that holds none of
// them, and the entries of those that do, to depth. A link is judged by
// where it leads and never followed into a folder that holds a denied place.
// The system folders LinuxRules always grants are left out.
func readableAround(dir string, denied []string, r ScriptRequest, depth int) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		names, err := r.ReadDir(dir)
		if err != nil {
			return
		}
		for _, name := range names {
			entry := path.Join(dir, name)
			real, err := r.Resolve(entry)
			switch {
			case err != nil:
			case slices.ContainsFunc(denied, func(d string) bool { return within(d, real) }):
				// a protected place, or inside one
			case slices.ContainsFunc(denied, func(d string) bool { return within(real, d) }):
				if real == entry && depth > 0 {
					walk(entry, depth-1)
				}
			case linuxSystem(real), containsPath(out, real):
			default:
				out = append(out, real)
			}
		}
	}
	walk(dir, depth)
	return out
}

// DirNames lists the names in dir, sorted: ScriptRequest.ReadDir on a real
// file system.
func DirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, err
}
