package sandbox

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// Request is what Flint knows when it is about to start an extension's
// program: the folders that are the extension's, what its manifest declares
// (run.sandbox, as written) and what its permissions add.
type Request struct {
	// GOOS is "linux" or "darwin".
	GOOS string
	// Home is the user's home folder; ConfigDir Flint's own.
	Home      string
	ConfigDir string
	// ExtensionDir is the installed folder (read and run), DataDir the
	// extension's data folder and TempDir its private temporary folder (both
	// written). All three are absolute, with symbolic links resolved.
	ExtensionDir string
	DataDir      string
	TempDir      string
	// Read and Write are run.sandbox's paths; Network, LocalSockets and Exec
	// its flags (Network also when the manifest has a network: permission).
	Read         []string
	Write        []string
	Network      bool
	LocalSockets bool
	Exec         bool
	// Roots are the trusted project folders, for an extension with the
	// workspace.read permission; nil otherwise.
	Roots []string
	// Interpreters are the programs the system starts to run the extension's
	// own program (its ELF loader, a script's interpreter): Interpreters.
	Interpreters []string
	// Env looks up a variable of the environment the program gets (PATH, the
	// XDG folders, run.env's variables).
	Env func(name string) string
	// Resolve resolves symbolic links (filepath.EvalSymlinks); an error means
	// the path doesn't exist.
	Resolve func(path string) (string, error)
}

// Policy is what one process may do, in concrete paths.
type Policy struct {
	GOOS string
	// ExtensionDir may be read and its files run.
	ExtensionDir string
	// Read may be read (files, or folders with everything in them).
	Read []string
	// Write may be read and changed.
	Write []string
	// ExecFiles may be run besides the extension's own files. With ExecAny,
	// everything the process can read may be run, and the folders of PATH
	// are readable.
	ExecFiles []string
	ExecAny   bool
	// Network: Internet sockets (IPv4, IPv6) may be opened. LocalSockets:
	// Unix sockets may. macOS can only allow or deny both together.
	Network      bool
	LocalSockets bool
	// Protected are the places the macOS profile closes before it opens the
	// paths above (Linux denies everything not listed anyway).
	Protected []string
	// Notes say what was asked for but not granted, and why.
	Notes []string
}

// Build turns a request into a policy. Three rules protect the user from a
// path that is wider than it looks:
//
//   - nothing inside Flint's own folder is granted, except the extension's
//     own folders;
//   - a path that contains a protected location (the home folder, ~/.config
//     with a browser profile in it) is not granted: Landlock has no "except",
//     and the same rule on macOS keeps the two systems alike;
//   - a protected location itself (~/.kube) is granted only when the
//     manifest names it as written; a project folder, a PATH folder or a
//     symbolic link that leads into one is not.
func Build(r Request) Policy {
	p := Policy{
		GOOS: r.GOOS, ExtensionDir: r.ExtensionDir, Network: r.Network, LocalSockets: r.LocalSockets, ExecAny: r.Exec,
		Write:     []string{r.DataDir, r.TempDir},
		Protected: darwinClosed(r.Home),
	}
	if !r.Exec {
		p.ExecFiles = slices.Clone(r.Interpreters)
	}
	protected := ProtectedPaths(r.GOOS, r.Home, r.ConfigDir)
	own := []string{r.ExtensionDir, r.DataDir, r.TempDir}
	grant := func(list *[]string, shown, target string, explicit bool) {
		real, err := r.Resolve(target)
		if err != nil {
			p.Notes = append(p.Notes, fmt.Sprintf("%s is not granted: it doesn't exist", shown))
			return
		}
		if real != target {
			explicit = false // a link: judged by where it leads
		}
		switch reason := refuse(real, r.Home, r.ConfigDir, own, protected, explicit); {
		case reason != "":
			p.Notes = append(p.Notes, fmt.Sprintf("%s is not granted: %s", shown, reason))
		case !containsPath(*list, real):
			*list = append(*list, real)
		}
	}
	for _, root := range r.Roots {
		grant(&p.Read, "the project folder "+root, path.Clean(root), false)
	}
	for _, declared := range r.Read {
		for _, target := range expand(declared, r, &p.Notes) {
			grant(&p.Read, declared, target, true)
		}
	}
	for _, declared := range r.Write {
		for _, target := range expand(declared, r, &p.Notes) {
			grant(&p.Write, declared, target, true)
		}
	}
	if r.Exec {
		for _, dir := range strings.Split(r.Env("PATH"), ":") {
			if !strings.HasPrefix(dir, "/") {
				continue // empty or relative: the working directory is the extension's folder
			}
			dir = path.Clean(dir)
			real, err := r.Resolve(dir)
			if err != nil || linuxSystem(real) && r.GOOS == "linux" {
				continue
			}
			if refuse(real, r.Home, r.ConfigDir, own, protected, false) == "" && !containsPath(p.Read, real) {
				p.Read = append(p.Read, real)
			}
		}
	}
	// A path both read and written is written.
	p.Read = slices.DeleteFunc(p.Read, func(s string) bool { return containsPath(p.Write, s) })
	return p
}

// refuse says why real may not be granted; "" when it may.
func refuse(real, home, configDir string, own, protected []string, explicit bool) string {
	if real == "/" || real == home {
		return "it is too wide; name the folders the program needs"
	}
	for _, o := range own {
		if within(o, real) {
			return "" // already the extension's
		}
	}
	if within(configDir, real) {
		return "it is inside Flint's own folder"
	}
	for _, prot := range protected {
		switch {
		case real != prot && within(real, prot):
			return fmt.Sprintf("it contains %s; name the folders the program needs", tilde(prot, home))
		case within(prot, real) && !explicit:
			return fmt.Sprintf("it is inside %s, which only the manifest can ask for by name", tilde(prot, home))
		}
	}
	return ""
}

// expand turns a declared path into absolute ones: ~, the XDG folders with
// their defaults, and ${env:NAME} as the list of paths the variable holds.
func expand(declared string, r Request, notes *[]string) []string {
	if name, ok := strings.CutPrefix(declared, "${env:"); ok {
		name = strings.TrimSuffix(name, "}")
		var out []string
		for _, v := range strings.Split(r.Env(name), ":") {
			switch {
			case v == "":
			case strings.HasPrefix(v, "/") && !strings.ContainsRune(v, 0):
				out = append(out, path.Clean(v))
			default:
				*notes = append(*notes, fmt.Sprintf("%s is not granted: $%s holds a relative path", declared, name))
			}
		}
		return out
	}
	if rest, ok := strings.CutPrefix(declared, "~/"); ok {
		return []string{path.Join(r.Home, rest)}
	}
	if rest, ok := strings.CutPrefix(declared, "${"); ok {
		name, rest, _ := strings.Cut(rest, "}")
		base := r.Env(name)
		if !strings.HasPrefix(base, "/") {
			fallback, ok := xdgDefaults[name]
			if !ok {
				*notes = append(*notes, fmt.Sprintf("%s is not granted: $%s is not set", declared, name))
				return nil
			}
			base = path.Join(r.Home, fallback)
		}
		return []string{path.Join(base, rest)}
	}
	return []string{path.Clean(declared)}
}

// xdgDefaults are the XDG base folders when their variable is unset
// (XDG_RUNTIME_DIR has no default).
var xdgDefaults = map[string]string{
	"XDG_CACHE_HOME":  ".cache",
	"XDG_CONFIG_HOME": ".config",
	"XDG_DATA_HOME":   ".local/share",
	"XDG_STATE_HOME":  ".local/state",
}

// ProtectedPaths are the places that hold credentials and private history:
// never granted as part of something wider, and granted themselves only when
// a manifest names them.
func ProtectedPaths(goos, home, configDir string) []string {
	out := []string{configDir}
	for _, rel := range []string{
		// keys and cloud credentials
		".ssh", ".gnupg", ".aws", ".kube", ".docker", ".azure", ".config/gcloud", ".config/gh", ".config/git/credentials",
		".netrc", ".git-credentials", ".npmrc", ".pypirc", ".password-store", ".config/op", ".vault-token",
		// shell and tool history
		".bash_history", ".zsh_history", ".zhistory", ".history", ".local/share/fish/fish_history",
		".python_history", ".node_repl_history", ".psql_history", ".mysql_history", ".sqlite_history", ".lesshst",
	} {
		out = append(out, path.Join(home, rel))
	}
	var more []string
	switch goos {
	case "darwin":
		more = []string{
			"Library/Keychains", "Library/Cookies", "Library/Safari", "Library/Mail", "Library/Messages",
			"Library/Application Support/Google/Chrome", "Library/Application Support/Chromium", "Library/Application Support/Firefox",
			"Library/Application Support/BraveSoftware", "Library/Application Support/Microsoft Edge", "Library/Application Support/Arc",
		}
	default:
		more = []string{
			".local/share/keyrings", ".local/share/kwalletd", ".pki",
			".mozilla", ".config/google-chrome", ".config/chromium", ".config/BraveSoftware", ".config/microsoft-edge",
			"snap/firefox", "snap/chromium", ".var/app",
		}
	}
	for _, rel := range more {
		out = append(out, path.Join(home, rel))
	}
	return out
}

// darwinClosed is what the macOS profile makes unreadable before opening the
// policy's paths: every user's files, other disks and the shared temporary
// folders. (The profile allows the rest of the system by default.)
func darwinClosed(home string) []string {
	out := []string{"/Users", "/Volumes", "/Network", "/private/var/folders", "/private/tmp", "/private/var/tmp", "/Library/Keychains", "/private/var/root"}
	if !within("/Users", home) {
		out = append(out, home)
	}
	return out
}

// within: p is root or inside it (clean absolute paths).
func within(root, p string) bool {
	if root == "" {
		return false
	}
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}

// containsPath: one of list is p or contains it.
func containsPath(list []string, p string) bool {
	return slices.ContainsFunc(list, func(dir string) bool { return within(dir, p) })
}

func tilde(p, home string) string {
	if p != home && within(home, p) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
