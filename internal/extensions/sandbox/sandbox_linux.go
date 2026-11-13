package sandbox

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// probed caches Probe: the kernel doesn't change while Flint runs.
var probed = sync.OnceValue(probe)

// Probe reports what this kernel can enforce. Landlock is required (files
// are the point of the sandbox); seccomp and newer Landlock versions add to
// it, and what is missing is listed in Gaps.
func Probe() Support { return probed() }

func probe() Support {
	abi, err := landlockABI()
	if err != nil {
		reason := "this kernel has no Landlock (Linux 5.13 or newer is needed)"
		if errors.Is(err, unix.EOPNOTSUPP) {
			reason = "Landlock is turned off in this kernel (it is not in the lsm= boot parameter)"
		}
		return Support{Reason: reason}
	}
	_, known := seccompArches[runtime.GOARCH]
	s := Support{Available: true, landlockABI: abi, seccomp: known && seccompAvailable()}
	return describeLinux(s)
}

// describeLinux fills in the mechanism's name and the gaps of an older kernel.
func describeLinux(s Support) Support {
	s.Mechanism = fmt.Sprintf("Landlock (ABI %d)", s.landlockABI)
	if s.seccomp {
		s.Mechanism += " and seccomp"
	}
	s.Gaps = nil
	switch {
	case s.seccomp:
	case s.landlockABI >= 4:
		s.Gaps = append(s.Gaps, "Only TCP is blocked when an extension has no network access; UDP and local sockets are not (no seccomp filter on this system).")
	default:
		s.Gaps = append(s.Gaps, "Network access and local sockets are not blocked (this needs seccomp, or Linux 6.7 for Landlock's TCP rules).")
	}
	if s.landlockABI < 3 {
		s.Gaps = append(s.Gaps, "Files an extension may only read can still be emptied by it (truncate is controlled from Linux 6.2).")
	}
	if s.landlockABI < 6 {
		s.Gaps = append(s.Gaps, "An extension can send signals to your other processes (controlled from Linux 6.12).")
	}
	return s
}

// landlockABI asks the kernel for its Landlock version.
func landlockABI() (int, error) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, errno
	}
	if v < 1 {
		return 0, unix.ENOSYS
	}
	return int(v), nil
}

// seccompAvailable: the kernel has seccomp filters (asking whether it knows
// the "return an error" action changes nothing).
func seccompAvailable() bool {
	action := uint32(unix.SECCOMP_RET_ERRNO)
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_ACTION_AVAIL, 0, uintptr(unsafe.Pointer(&action)))
	return errno == 0
}

// wrap: on Linux the launcher applies the rules itself and runs argv.
func wrap(p Policy, s Support, argv []string) (launchSpec, error) {
	rules := p.LinuxRules(s.landlockABI, s.seccomp)
	return launchSpec{Argv: argv, Linux: &rules}, nil
}

// launch confines this process (this thread: the caller locked it) and
// replaces it with the program. It returns only on failure.
func launch(spec launchSpec, env []string) error {
	if spec.Linux == nil {
		return errors.New("no rules to apply")
	}
	if err := setLimits(spec.OpenFiles); err != nil {
		return fmt.Errorf("resource limits: %w", err)
	}
	if err := applyRules(*spec.Linux); err != nil {
		return err
	}
	return fmt.Errorf("%s: %w", spec.Argv[0], syscall.Exec(spec.Argv[0], spec.Argv, env))
}

// applyRules restricts the calling thread, for good: no_new_privs (required
// by Landlock and seccomp without privileges, and it keeps setuid programs
// from regaining any), the Landlock ruleset, then the socket filter.
func applyRules(r LinuxRules) error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	// The attribute grew with the ABI; an older kernel refuses a larger one
	// with non-zero fields, so pass only what it knows.
	attr := struct{ fs, net, scoped uint64 }{r.HandledFS, r.HandledNet, r.Scoped}
	size := unsafe.Sizeof(attr)
	switch {
	case r.ABI < 4:
		size = 8
	case r.ABI < 6:
		size = 16
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", errno)
	}
	ruleset := int(fd)
	defer unix.Close(ruleset)
	for _, rule := range r.Paths {
		if err := allowPath(ruleset, rule, r.HandledFS); err != nil {
			return fmt.Errorf("landlock rule for %s: %w", rule.Path, err)
		}
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(ruleset), 0, 0); errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", errno)
	}
	if r.SocketFilter {
		if err := installSocketFilter(r.SocketFamilies); err != nil {
			return fmt.Errorf("seccomp: %w", err)
		}
	}
	return nil
}

// allowPath adds one rule; a path that doesn't exist is skipped.
func allowPath(ruleset int, rule PathRule, handled uint64) error {
	fd, err := unix.Open(rule.Path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.EACCES) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	access := rule.Access & handled
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		access &= fsFileRights // the kernel refuses folder rights on a file
	}
	if access == 0 {
		return nil
	}
	// struct landlock_path_beneath_attr is packed: 8 bytes, then the fd.
	attr := struct {
		allowed uint64
		parent  int32
	}{access, int32(fd)}
	_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// installSocketFilter loads socketFilter for this architecture.
func installSocketFilter(allowed []uint32) error {
	arch, ok := seccompArches[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("no filter for %s", runtime.GOARCH)
	}
	insns := socketFilter(arch, allowed)
	filter := make([]unix.SockFilter, len(insns))
	for i, in := range insns {
		filter[i] = unix.SockFilter{Code: in.Code, Jt: in.Jt, Jf: in.Jf, K: in.K}
	}
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&prog)))
	runtime.KeepAlive(filter)
	if errno != 0 {
		return errno
	}
	return nil
}

// launcherPath is the program to start as the launcher: this process's own
// image, which stays reachable even when the file was replaced by an update.
func launcherPath() (string, error) {
	if _, err := os.Stat("/proc/self/exe"); err == nil {
		return "/proc/self/exe", nil
	}
	return os.Executable()
}
