package sandbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// request is a Request over an imaginary machine: every path exists and is
// its own target unless links says otherwise.
func request(change func(r *Request)) Request {
	env := map[string]string{"PATH": "/usr/bin:/home/me/.local/bin:/home/me/.ssh:relative::/opt/tools/bin"}
	r := Request{
		GOOS: "linux", Home: "/home/me", ConfigDir: "/home/me/.config/flint",
		ExtensionDir: "/home/me/.config/flint/extensions/acme.ext",
		DataDir:      "/home/me/.config/flint/extension-data/acme.ext",
		TempDir:      "/tmp/flint-ext-1",
		Env:          func(name string) string { return env[name] },
		Resolve:      func(p string) (string, error) { return p, nil },
	}
	if change != nil {
		change(&r)
	}
	return r
}

func TestBuildStrictDefault(t *testing.T) {
	p := Build(request(nil))
	if len(p.Read) != 0 || p.Network || p.ExecAny || len(p.Notes) != 0 {
		t.Errorf("the strict profile grants more than the extension's folders: %+v", p)
	}
	if !slices.Equal(p.Write, []string{"/home/me/.config/flint/extension-data/acme.ext", "/tmp/flint-ext-1"}) {
		t.Errorf("Write = %v", p.Write)
	}
}

func TestBuildGrants(t *testing.T) {
	missing := map[string]bool{"/home/me/nothing": true}
	links := map[string]string{"/home/me/innocent": "/home/me/.ssh", "/home/me/dot-kube": "/home/me/dotfiles/kube"}
	env := map[string]string{
		"KUBECONFIG":      "/home/me/.kube/config:/work/kube.yaml:relative.yaml",
		"XDG_CONFIG_HOME": "/home/me/cfg",
		"PATH":            "/usr/bin:/home/me/.local/bin:/home/me/.ssh:relative::/opt/tools/bin:/home/me/.config/flint/extensions/other.ext",
	}
	tests := []struct {
		name      string
		change    func(r *Request)
		read      []string
		write     []string
		noteParts []string
	}{
		{name: "a declared credentials folder is granted by name",
			change: func(r *Request) { r.Read = []string{"~/.kube", "~/.aws/config", "/var/run/docker.sock"} },
			read:   []string{"/home/me/.kube", "/home/me/.aws/config", "/var/run/docker.sock"}},
		{name: "the home folder, the root and folders with protected places in them are refused",
			change:    func(r *Request) { r.Read = []string{"/home/me", "/", "/home", "~/.config", "~/.local/share"} },
			noteParts: []string{"/home/me is not granted: it is too wide", "/ is not granted: it is too wide", "/home is not granted: it contains", "~/.config is not granted: it contains", "~/.local/share is not granted: it contains ~/.local/share/"}},
		{name: "Flint's own files and other extensions are never granted",
			change: func(r *Request) {
				r.Read = []string{"~/.config/flint/settings.json", "~/.config/flint/extensions/other.ext"}
				r.Write = []string{"~/.config/flint/extension-data/other.ext", "~/.config/flint/extension-data/acme.ext/cache"}
			},
			noteParts: []string{"settings.json is not granted: it is inside Flint's own folder", "extensions/other.ext is not granted", "extension-data/other.ext is not granted"}},
		{name: "project folders are read only with workspace.read, and not when they hold or are protected places",
			change: func(r *Request) {
				r.Roots = []string{"/home/me/code/app", "/home/me", "/home/me/.ssh", "/home/me/.kube/work"}
			},
			read:      []string{"/home/me/code/app"},
			noteParts: []string{"the project folder /home/me is not granted", "the project folder /home/me/.ssh is not granted: it is inside ~/.ssh", "the project folder /home/me/.kube/work is not granted"}},
		{name: "variables: XDG folders with defaults, a run.env list, an unset runtime folder",
			change: func(r *Request) {
				r.Read = []string{"${XDG_CONFIG_HOME}/tool", "${XDG_DATA_HOME}/tool", "${env:KUBECONFIG}", "${XDG_RUNTIME_DIR}/bus", "${env:UNSET}"}
			},
			read:      []string{"/home/me/cfg/tool", "/home/me/.local/share/tool", "/home/me/.kube/config", "/work/kube.yaml"},
			noteParts: []string{"${env:KUBECONFIG} is not granted: $KUBECONFIG holds a relative path", "${XDG_RUNTIME_DIR}/bus is not granted: $XDG_RUNTIME_DIR is not set"}},
		{name: "a missing path is not granted, a link is judged by its target",
			change:    func(r *Request) { r.Read = []string{"~/nothing", "~/innocent", "~/dot-kube"} },
			read:      []string{"/home/me/dotfiles/kube"},
			noteParts: []string{"~/nothing is not granted: it doesn't exist", "~/innocent is not granted: it is inside ~/.ssh"}},
		{name: "written paths are not listed as read too",
			change: func(r *Request) { r.Read = []string{"~/out/sub", "~/in"}; r.Write = []string{"~/out"} },
			read:   []string{"/home/me/in"}, write: []string{"/home/me/out"}},
		{name: "exec adds PATH's folders, except system, relative and protected ones",
			change: func(r *Request) { r.Exec = true },
			read:   []string{"/home/me/.local/bin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := request(func(r *Request) {
				r.Env = func(name string) string { return env[name] }
				r.Resolve = func(p string) (string, error) {
					if missing[p] {
						return "", fs.ErrNotExist
					}
					if to, ok := links[p]; ok {
						return to, nil
					}
					return p, nil
				}
				tt.change(r)
			})
			p := Build(r)
			if tt.name == "exec adds PATH's folders, except system, relative and protected ones" {
				// /opt is a system path on Linux; on macOS the profile reads it anyway.
				if !p.ExecAny || len(p.ExecFiles) != 0 {
					t.Errorf("ExecAny = %v, ExecFiles = %v", p.ExecAny, p.ExecFiles)
				}
			}
			if !slices.Equal(p.Read, tt.read) {
				t.Errorf("Read = %v, want %v", p.Read, tt.read)
			}
			if got := p.Write[2:]; !slices.Equal(got, tt.write) {
				t.Errorf("Write (declared) = %v, want %v", got, tt.write)
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

func TestBuildExecFilesOnlyWithoutExec(t *testing.T) {
	interp := []string{"/usr/bin/python3", "/lib64/ld-linux-x86-64.so.2"}
	if p := Build(request(func(r *Request) { r.Interpreters = interp })); !slices.Equal(p.ExecFiles, interp) || p.ExecAny {
		t.Errorf("ExecFiles = %v", p.ExecFiles)
	}
}

func TestProtectedPaths(t *testing.T) {
	linux := ProtectedPaths("linux", "/home/me", "/home/me/.config/flint")
	darwin := ProtectedPaths("darwin", "/Users/me", "/Users/me/Library/Application Support/flint")
	for _, want := range []string{"/home/me/.ssh", "/home/me/.gnupg", "/home/me/.aws", "/home/me/.kube", "/home/me/.docker", "/home/me/.config/gcloud",
		"/home/me/.mozilla", "/home/me/.config/google-chrome", "/home/me/.local/share/keyrings", "/home/me/.bash_history", "/home/me/.zsh_history", "/home/me/.config/flint"} {
		if !slices.Contains(linux, want) {
			t.Errorf("linux: %s is not protected", want)
		}
	}
	for _, want := range []string{"/Users/me/.ssh", "/Users/me/Library/Keychains", "/Users/me/Library/Application Support/Google/Chrome", "/Users/me/Library/Safari", "/Users/me/Library/Application Support/flint"} {
		if !slices.Contains(darwin, want) {
			t.Errorf("darwin: %s is not protected", want)
		}
	}
}

func rule(r LinuxRules, path string) (uint64, bool) {
	for _, p := range r.Paths {
		if p.Path == path {
			return p.Access, true
		}
	}
	return 0, false
}

func TestLinuxRules(t *testing.T) {
	p := Build(request(func(r *Request) {
		r.Read = []string{"~/in"}
		r.Write = []string{"~/out"}
		r.Interpreters = []string{"/lib64/ld-linux-x86-64.so.2"}
	}))
	r := p.LinuxRules(7, true)
	read := fsReadFile | fsReadDir
	for path, want := range map[string]uint64{
		"/usr": read, "/etc": read, "/proc": read,
		"/dev/null":                         fsReadFile | fsWriteFile | fsIoctlDev,
		p.ExtensionDir:                      read | fsExecute,
		"/lib64/ld-linux-x86-64.so.2":       fsReadFile | fsExecute,
		"/home/me/in":                       read,
		"/home/me/out":                      read | fsWriteFile | fsRemoveDir | fsRemoveFile | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo | fsMakeSym | fsRefer | fsTruncate,
		"/tmp/flint-ext-1":                  read | fsWriteFile | fsRemoveDir | fsRemoveFile | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo | fsMakeSym | fsRefer | fsTruncate,
		"/home/me/.config/flint":            0,
		"/home/me":                          0,
		"/home/me/.config/flint/extensions": 0,
	} {
		got, ok := rule(r, path)
		if ok != (want != 0) || got != want {
			t.Errorf("%s: access %#x (rule: %v), want %#x", path, got, ok, want)
		}
	}
	for _, pr := range r.Paths {
		if pr.Access&^r.HandledFS != 0 {
			t.Errorf("%s allows rights the ruleset doesn't handle: %#x", pr.Path, pr.Access&^r.HandledFS)
		}
		if pr.Access&(fsMakeChar|fsMakeBlock) != 0 {
			t.Errorf("%s may create device files", pr.Path)
		}
	}
	if !r.SocketFilter || len(r.SocketFamilies) != 0 || r.HandledNet != netBindTCP|netConnectTCP || r.Scoped != scopeSignal|scopeAbstractUnix {
		t.Errorf("no network: filter %v %v, net %#x, scoped %#x", r.SocketFilter, r.SocketFamilies, r.HandledNet, r.Scoped)
	}

	// The Internet without local sockets, local sockets without the
	// Internet, then both and other programs.
	p.Network = true
	r = p.LinuxRules(7, true)
	if !r.SocketFilter || !slices.Equal(r.SocketFamilies, []uint32{afInet, afInet6, afNetlink}) || r.HandledNet != 0 || r.Scoped != scopeSignal|scopeAbstractUnix {
		t.Errorf("network: filter %v %v, net %#x, scoped %#x", r.SocketFilter, r.SocketFamilies, r.HandledNet, r.Scoped)
	}
	p.Network, p.LocalSockets = false, true
	r = p.LinuxRules(7, true)
	if !r.SocketFilter || !slices.Equal(r.SocketFamilies, []uint32{afUnix}) || r.HandledNet != netBindTCP|netConnectTCP || r.Scoped != scopeSignal {
		t.Errorf("local sockets: filter %v %v, net %#x, scoped %#x", r.SocketFilter, r.SocketFamilies, r.HandledNet, r.Scoped)
	}
	p.Network, p.ExecAny = true, true
	r = p.LinuxRules(7, true)
	if r.SocketFilter || r.HandledNet != 0 || r.Scoped != scopeSignal {
		t.Errorf("network and local sockets: filter %v, net %#x, scoped %#x", r.SocketFilter, r.HandledNet, r.Scoped)
	}
	for _, path := range []string{"/usr", "/home/me/in", "/home/me/out"} {
		if got, _ := rule(r, path); got&fsExecute == 0 {
			t.Errorf("exec: %s can't be run", path)
		}
	}
	if got, _ := rule(r, "/dev/null"); got&fsExecute != 0 {
		t.Error("exec: a device can be run")
	}
}

// Older kernels: each Landlock version handles only what it knows, and the
// network falls back from seccomp to TCP rules to nothing.
func TestLinuxRulesByABI(t *testing.T) {
	p := Build(request(nil))
	base := fsExecute | fsWriteFile | fsReadFile | fsReadDir | fsRemoveDir | fsRemoveFile | fsMakeChar | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo | fsMakeBlock | fsMakeSym
	tests := []struct {
		abi         int
		seccomp     bool
		handled     uint64
		net, scoped uint64
		sockets     bool
	}{
		{1, false, base, 0, 0, false},
		{1, true, base, 0, 0, true},
		{2, true, base | fsRefer, 0, 0, true},
		{3, false, base | fsRefer | fsTruncate, 0, 0, false},
		{4, false, base | fsRefer | fsTruncate, netBindTCP | netConnectTCP, 0, false},
		{5, true, base | fsRefer | fsTruncate | fsIoctlDev, netBindTCP | netConnectTCP, 0, true},
		{6, true, base | fsRefer | fsTruncate | fsIoctlDev, netBindTCP | netConnectTCP, scopeSignal | scopeAbstractUnix, true},
		{7, true, base | fsRefer | fsTruncate | fsIoctlDev, netBindTCP | netConnectTCP, scopeSignal | scopeAbstractUnix, true},
	}
	for _, tt := range tests {
		r := p.LinuxRules(tt.abi, tt.seccomp)
		if r.ABI != tt.abi || r.HandledFS != tt.handled || r.HandledNet != tt.net || r.Scoped != tt.scoped || r.SocketFilter != tt.sockets {
			t.Errorf("ABI %d, seccomp %v: %+v", tt.abi, tt.seccomp, r)
		}
		for _, pr := range r.Paths {
			if pr.Access&^r.HandledFS != 0 {
				t.Errorf("ABI %d: %s allows %#x, which this version doesn't know", tt.abi, pr.Path, pr.Access&^r.HandledFS)
			}
		}
	}
}

// runBPF interprets the few instructions socketFilter uses over a system
// call's architecture, number and first argument.
func runBPF(t *testing.T, prog []bpfInsn, arch, nr, arg uint32) uint32 {
	t.Helper()
	var a uint32
	for pc := 0; pc < len(prog); pc++ {
		in := prog[pc]
		switch in.Code {
		case bpfLoadAbs:
			switch in.K {
			case 0:
				a = nr
			case 4:
				a = arch
			case 16:
				a = arg
			default:
				t.Fatalf("load of offset %d", in.K)
			}
		case bpfJumpEq:
			if a == in.K {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case bpfJumpGe:
			if a >= in.K {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case bpfReturn:
			return in.K
		default:
			t.Fatalf("instruction %#x", in.Code)
		}
	}
	t.Fatal("the filter ran off its end")
	return 0
}

func TestSocketFilter(t *testing.T) {
	const i386 = 0x40000003
	const refused, missing = seccompErr | errnoAccess, seccompErr | errnoNoSys
	for name, arch := range seccompArches {
		type call struct {
			what          string
			arch, nr, arg uint32
			want          uint32
		}
		always := []call{
			{"io_uring_setup", arch.audit, arch.ioUring, 0, missing},
			{"read", arch.audit, 0, 0, seccompOK},
			{"write", arch.audit, 1, afUnix, seccompOK},
			{"socketpair", arch.audit, arch.socket + 1, afUnix, seccompOK},
			{"execve", arch.audit, 59, 0, seccompOK},
			{"a call of another ABI", i386, 3, 0, missing},
			{"that ABI's number for socket", i386, arch.socket, afUnix, missing},
			{"a packet socket", arch.audit, arch.socket, 17, refused},
			{"a Bluetooth socket", arch.audit, arch.socket, 31, refused},
		}
		if arch.refuseX32Numbers {
			always = append(always, call{"x32 socket", arch.audit, x32Bit + 41, afUnix, missing})
		}
		for _, tt := range []struct {
			name                       string
			allowed                    []uint32
			unix, inet, inet6, netlink uint32
		}{
			{"no sockets", nil, refused, refused, refused, refused},
			{"the Internet", []uint32{afInet, afInet6, afNetlink}, refused, seccompOK, seccompOK, seccompOK},
			{"local sockets", []uint32{afUnix}, seccompOK, refused, refused, refused},
		} {
			prog := socketFilter(arch, tt.allowed)
			calls := append(slices.Clone(always),
				call{"a Unix socket", arch.audit, arch.socket, afUnix, tt.unix},
				call{"an IPv4 socket", arch.audit, arch.socket, afInet, tt.inet},
				call{"an IPv6 socket", arch.audit, arch.socket, afInet6, tt.inet6},
				call{"a netlink socket", arch.audit, arch.socket, afNetlink, tt.netlink},
			)
			for _, c := range calls {
				if got := runBPF(t, prog, c.arch, c.nr, c.arg); got != c.want {
					t.Errorf("%s, %s: %s = %#x, want %#x", name, tt.name, c.what, got, c.want)
				}
			}
			if len(prog) > 32 {
				t.Errorf("%s: %d instructions", name, len(prog))
			}
		}
	}
}

func TestDarwinProfile(t *testing.T) {
	p := Build(request(func(r *Request) {
		r.GOOS, r.Home, r.ConfigDir = "darwin", "/Users/me", "/Users/me/Library/Application Support/flint"
		r.ExtensionDir = "/Users/me/Library/Application Support/flint/extensions/acme.ext"
		r.DataDir = "/Users/me/Library/Application Support/flint/extension-data/acme.ext"
		r.TempDir = "/private/var/folders/ab/T/flint-ext-1"
		r.Read = []string{"~/.kube", `~/odd "name"\x`}
		r.Roots = []string{"/Users/me/code/app"}
		r.Interpreters = []string{"/usr/bin/env"}
	}))
	got, err := p.DarwinProfile()
	if err != nil {
		t.Fatal(err)
	}
	want := `(version 1)
(allow default)
(deny file-write*)
(allow file-write*
  (subpath "/Users/me/Library/Application Support/flint/extension-data/acme.ext")
  (subpath "/private/var/folders/ab/T/flint-ext-1"))
(allow file-write-data
  (literal "/dev/null")
  (literal "/dev/zero")
  (literal "/dev/dtracehelper"))
(deny file-read-data
  (subpath "/Users")
  (subpath "/Volumes")
  (subpath "/Network")
  (subpath "/private/var/folders")
  (subpath "/private/tmp")
  (subpath "/private/var/tmp")
  (subpath "/Library/Keychains")
  (subpath "/private/var/root"))
(allow file-read-data
  (subpath "/Users/me/Library/Application Support/flint/extensions/acme.ext")
  (subpath "/Users/me/code/app")
  (subpath "/Users/me/.kube")
  (subpath "/Users/me/odd \"name\"\\x")
  (subpath "/Users/me/Library/Application Support/flint/extension-data/acme.ext")
  (subpath "/private/var/folders/ab/T/flint-ext-1"))
(deny network*)
(deny process-exec*)
(allow process-exec*
  (subpath "/Users/me/Library/Application Support/flint/extensions/acme.ext"))
(allow process-exec*
  (literal "/usr/bin/env"))
(deny signal)
(allow signal (target self))
(allow signal (target same-sandbox))
`
	if got != want {
		t.Errorf("profile:\n%s\nwant:\n%s", got, want)
	}

	// Local sockets can't be allowed without the Internet here: either one
	// opens both.
	p.LocalSockets = true
	if got, _ := p.DarwinProfile(); strings.Contains(got, "network") {
		t.Errorf("local sockets are still blocked:\n%s", got)
	}
	p.LocalSockets = false

	// With the network and other programs, those two sections are gone.
	p.Network, p.ExecAny = true, true
	got, err = p.DarwinProfile()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "network") || strings.Contains(got, "process-exec") {
		t.Errorf("network and exec are still restricted:\n%s", got)
	}
	if !strings.Contains(got, "(deny file-write*)") || !strings.Contains(got, `(subpath "/Users")`) {
		t.Errorf("files are no longer restricted:\n%s", got)
	}

	// A home outside /Users is closed as well; a path that could break out
	// of its string is refused.
	if closed := darwinClosed("/var/home/me"); !slices.Contains(closed, "/var/home/me") {
		t.Errorf("closed = %v", closed)
	}
	p.Read = []string{"/Users/me/line\nbreak"}
	if _, err := p.DarwinProfile(); err == nil {
		t.Error("a path with a newline is written into the profile")
	}
	p.Read = []string{"relative"}
	if _, err := p.DarwinProfile(); err == nil {
		t.Error("a relative path is written into the profile")
	}
}

func TestInterpreters(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string, mode os.FileMode) string {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		real, err := filepath.EvalSymlinks(file)
		if err != nil {
			t.Fatal(err)
		}
		return real
	}
	tool := write("bin/tool", "#!"+filepath.Join(dir, "bin", "base")+" -x\n", 0o755)
	base := write("bin/base", "not a program", 0o755)
	env := write("bin/env", "also not a program", 0o755)
	write("bin/notexec", "x", 0o644)
	direct := write("direct", "#!"+tool+"\n", 0o755)
	viaEnv := write("via-env", "#! "+env+" -S FOO=1 tool --flag\n", 0o755)
	missing := write("missing", "#!"+env+" nothing-like-this\n", 0o755)
	relative := write("relative", "#!bin/base\n", 0o755)
	plain := write("plain", "just text", 0o755)

	if got := Interpreters(direct, bin); !slices.Equal(got, []string{tool, base}) {
		t.Errorf("a script's interpreter chain = %v", got)
	}
	if got := Interpreters(viaEnv, "relative:"+bin); !slices.Equal(got, []string{env, tool, base}) {
		t.Errorf("through env = %v", got)
	}
	if got := Interpreters(missing, bin); !slices.Equal(got, []string{env}) {
		t.Errorf("a command PATH doesn't have = %v", got)
	}
	for _, file := range []string{relative, plain, filepath.Join(dir, "nothing")} {
		if got := Interpreters(file, bin); len(got) != 0 {
			t.Errorf("%s: %v", file, got)
		}
	}
	if got := lookPath("notexec", bin); got != "" {
		t.Errorf("a file that isn't executable is found: %s", got)
	}
}

func TestCommandNeedsSupport(t *testing.T) {
	_, _, err := Command("/flint", Policy{}, Support{Reason: "no kernel support"}, []string{"/bin/true"})
	if err == nil || !strings.Contains(err.Error(), "no kernel support") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := Command("/flint", Policy{}, Support{Available: true}, nil); err == nil {
		t.Error("nothing to run is accepted")
	}
}
