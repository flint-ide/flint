package sandbox

import "strings"

// Landlock access rights (linux/landlock.h). Kept here, not taken from
// golang.org/x/sys/unix, so the rules are computed and tested on every system.
const (
	fsExecute    uint64 = 1 << 0
	fsWriteFile  uint64 = 1 << 1
	fsReadFile   uint64 = 1 << 2
	fsReadDir    uint64 = 1 << 3
	fsRemoveDir  uint64 = 1 << 4
	fsRemoveFile uint64 = 1 << 5
	fsMakeChar   uint64 = 1 << 6
	fsMakeDir    uint64 = 1 << 7
	fsMakeReg    uint64 = 1 << 8
	fsMakeSock   uint64 = 1 << 9
	fsMakeFifo   uint64 = 1 << 10
	fsMakeBlock  uint64 = 1 << 11
	fsMakeSym    uint64 = 1 << 12
	fsRefer      uint64 = 1 << 13 // ABI 2
	fsTruncate   uint64 = 1 << 14 // ABI 3
	fsIoctlDev   uint64 = 1 << 15 // ABI 5

	netBindTCP    uint64 = 1 << 0 // ABI 4
	netConnectTCP uint64 = 1 << 1

	scopeAbstractUnix uint64 = 1 << 0 // ABI 6
	scopeSignal       uint64 = 1 << 1

	// fsFileRights are the rights a rule on a file (not a folder) may carry.
	fsFileRights = fsExecute | fsWriteFile | fsReadFile | fsTruncate | fsIoctlDev
)

// LinuxRules is a Landlock ruleset and the seccomp switch, as the launcher
// applies them.
type LinuxRules struct {
	// ABI is the kernel's Landlock version the rules were made for.
	ABI int `json:"abi"`
	// HandledFS, HandledNet and Scoped are what the ruleset denies unless a
	// rule allows it.
	HandledFS  uint64 `json:"handledFs"`
	HandledNet uint64 `json:"handledNet"`
	Scoped     uint64 `json:"scoped"`
	// Paths are the allowed accesses, each for a file or for a folder and
	// everything below it. A path that doesn't exist is skipped.
	Paths []PathRule `json:"paths"`
	// SocketFilter installs the seccomp filter that refuses socket(2) for
	// every address family except SocketFamilies.
	SocketFilter   bool     `json:"socketFilter"`
	SocketFamilies []uint32 `json:"socketFamilies,omitempty"`
}

// Address families the socket filter can leave open (linux/socket.h).
const (
	afUnix    uint32 = 1
	afInet    uint32 = 2
	afInet6   uint32 = 10
	afNetlink uint32 = 16 // how libc and Go list interfaces and routes before connecting
)

// PathRule allows Access on Path and below.
type PathRule struct {
	Path   string `json:"path"`
	Access uint64 `json:"access"`
}

// linuxSystemPaths are read by every sandboxed program: programs, shared
// libraries, certificates, time zones and name resolution (/etc, /usr/share),
// the kernel's views of the process and the machine, and the usual homes of
// extra software. None holds the user's files. Paths that don't exist on a
// system are skipped.
var linuxSystemPaths = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/opt",
	"/proc", "/sys",
	"/run/systemd/resolve", "/var/lib/ca-certificates",
	"/nix", "/run/current-system", "/gnu", "/snap", "/var/lib/snapd", "/home/linuxbrew/.linuxbrew",
}

// linuxDevices may be read and written: the data sinks and sources.
var linuxDevices = []string{"/dev/null", "/dev/zero", "/dev/full", "/dev/random", "/dev/urandom"}

// linuxSystem: p is one of the system paths or inside one.
func linuxSystem(p string) bool { return containsPath(linuxSystemPaths, p) }

// LinuxRules computes the Landlock rules for p on a kernel with Landlock
// version abi (at least 1), and whether the socket filter is installed
// (seccomp: the kernel has seccomp filters).
//
// Everything Landlock knows at that version is denied by default; the rules
// then allow reading the system and what the policy lists, writing only the
// policy's Write paths, and running files only in the extension's folder
// (plus its interpreters), or wherever it can read with ExecAny.
func (p Policy) LinuxRules(abi int, seccomp bool) LinuxRules {
	read := fsReadFile | fsReadDir
	write := read | fsWriteFile | fsRemoveDir | fsRemoveFile | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo | fsMakeSym
	handled := write | fsExecute | fsMakeChar | fsMakeBlock
	device := fsReadFile | fsWriteFile
	if abi >= 2 {
		handled |= fsRefer
		write |= fsRefer
	}
	if abi >= 3 {
		handled |= fsTruncate
		write |= fsTruncate
	}
	if abi >= 5 {
		handled |= fsIoctlDev
		device |= fsIoctlDev
	}
	r := LinuxRules{ABI: abi, HandledFS: handled}
	if !p.Network && abi >= 4 {
		r.HandledNet = netBindTCP | netConnectTCP
	}
	if !p.Network || !p.LocalSockets {
		r.SocketFilter = seccomp
		if p.Network {
			r.SocketFamilies = []uint32{afInet, afInet6, afNetlink}
		}
		if p.LocalSockets {
			r.SocketFamilies = []uint32{afUnix}
		}
	}
	if abi >= 6 {
		r.Scoped = scopeSignal
		if !p.LocalSockets {
			r.Scoped |= scopeAbstractUnix
		}
	}
	var anyExec uint64
	if p.ExecAny {
		anyExec = fsExecute
	}
	add := func(path string, access uint64) {
		if path != "" && strings.HasPrefix(path, "/") {
			r.Paths = append(r.Paths, PathRule{Path: path, Access: access})
		}
	}
	for _, dir := range linuxSystemPaths {
		add(dir, read|anyExec)
	}
	for _, dev := range linuxDevices {
		add(dev, device)
	}
	add(p.ExtensionDir, read|fsExecute)
	for _, f := range p.ExecFiles {
		add(f, fsReadFile|fsExecute)
	}
	for _, path := range p.Read {
		add(path, read|anyExec)
	}
	for _, path := range p.Write {
		add(path, write|anyExec)
	}
	return r
}

// A classic BPF instruction, as seccomp takes it (struct sock_filter).
type bpfInsn struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

// BPF opcodes and seccomp return values used by socketFilter.
const (
	bpfLoadAbs  = 0x20 // BPF_LD | BPF_W | BPF_ABS; K: 0 the number, 4 the architecture, 16 the first argument
	bpfJumpEq   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
	bpfJumpGe   = 0x35 // BPF_JMP | BPF_JGE | BPF_K
	bpfReturn   = 0x06 // BPF_RET | BPF_K
	seccompOK   = 0x7fff0000
	seccompErr  = 0x00050000 // | errno
	errnoAccess = 13
	errnoNoSys  = 38
	// x32Bit marks x32 system calls on x86-64.
	x32Bit = 0x40000000
)

// seccompArch describes the system calls of one architecture the filter
// names; other architectures get no filter (Landlock's TCP rules remain).
type seccompArch struct {
	audit            uint32 // AUDIT_ARCH_*
	socket, ioUring  uint32
	refuseX32Numbers bool
}

var seccompArches = map[string]seccompArch{
	"amd64":   {audit: 0xc000003e, socket: 41, ioUring: 425, refuseX32Numbers: true},
	"arm64":   {audit: 0xc00000b7, socket: 198, ioUring: 425},
	"riscv64": {audit: 0xc00000f3, socket: 198, ioUring: 425},
	"loong64": {audit: 0xc0000102, socket: 198, ioUring: 425},
}

// socketFilter is the seccomp program that keeps a process from getting a
// new socket, except of the address families in allowed: socket(2) fails
// with EACCES, and io_uring, which can create sockets without that call, is
// reported as missing. System calls of another ABI of the same machine
// (32-bit, x32) are refused, since their numbers differ. Everything else is
// allowed: files are Landlock's job. socketpair(2) stays, as do sockets the
// process was given.
func socketFilter(a seccompArch, allowed []uint32) []bpfInsn {
	deny := func(errno uint32) bpfInsn { return bpfInsn{Code: bpfReturn, K: seccompErr | errno} }
	prog := []bpfInsn{
		{Code: bpfLoadAbs, K: 4},                    // the call's architecture
		{Code: bpfJumpEq, K: a.audit, Jt: 1, Jf: 0}, // ours: skip the refusal
		deny(errnoNoSys),
		{Code: bpfLoadAbs, K: 0}, // the call's number
	}
	if a.refuseX32Numbers {
		prog = append(prog, bpfInsn{Code: bpfJumpGe, K: x32Bit, Jt: 0, Jf: 1}, deny(errnoNoSys))
	}
	ok := bpfInsn{Code: bpfReturn, K: seccompOK}
	// socket(family, …): the family is the first argument (the low half of
	// args[0]; every architecture above is little-endian).
	check := []bpfInsn{{Code: bpfLoadAbs, K: 16}}
	for _, family := range allowed {
		check = append(check, bpfInsn{Code: bpfJumpEq, K: family, Jt: 0, Jf: 1}, ok)
	}
	check = append(check, deny(errnoAccess))
	prog = append(prog,
		bpfInsn{Code: bpfJumpEq, K: a.ioUring, Jt: 0, Jf: 1}, deny(errnoNoSys),
		bpfInsn{Code: bpfJumpEq, K: a.socket, Jt: 1, Jf: 0}, ok, // not socket: allowed
	)
	return append(prog, check...)
}
