package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"flint/internal/extensions/manifest"
	"flint/internal/extensions/sandbox"
)

// How an extension's process is confined (docs/design/extensions.md,
// "Sandbox"): the manifest's run.sandbox decides whether, package sandbox
// how. This file turns a manifest into a policy when the process starts,
// and into the words Settings ▸ Extensions and the install dialog show.

// Sandbox states, as SandboxInfo.State.
const (
	SandboxNone        = "none"        // no program: nothing to confine
	SandboxOn          = "sandboxed"   // the program runs confined
	SandboxDeclaredOff = "declared"    // the manifest says "mode": "none"
	SandboxUndeclared  = "undeclared"  // the manifest has no run.sandbox
	SandboxUnavailable = "unavailable" // asked for, but this system can't
)

// SandboxInfo says how an extension's program is confined, for the install
// dialog and Settings ▸ Extensions.
type SandboxInfo struct {
	// State is one of the Sandbox constants.
	State string `json:"state"`
	// Summary is one line: "Sandboxed (…)", "Not sandboxed (declared by the
	// extension)", "Not sandboxed: not available on this system (…)".
	Summary string `json:"summary"`
	// Details are what it may read, write and connect to, what this system
	// can't enforce, and what was asked for but not granted.
	Details []string `json:"details"`
	// Refused: the user requires a sandbox and this extension has none, so
	// Flint doesn't start it.
	Refused bool `json:"refused"`
}

// support is what this system can enforce (a fake in tests).
func (h *Host) support() sandbox.Support {
	if h.o.Sandbox != nil {
		return h.o.Sandbox()
	}
	return sandbox.Probe()
}

func (h *Host) requireSandbox() bool { return h.o.RequireSandbox != nil && h.o.RequireSandbox() }

// describeSandbox is the sandbox of m's program on a system with support s;
// notes are grants the policy refused (known once the folders are).
func describeSandbox(m *manifest.Manifest, s sandbox.Support, notes []string) SandboxInfo {
	in := SandboxInfo{Details: []string{}}
	state := m.SandboxState()
	switch {
	case state == manifest.SandboxNoProgram:
		in.State, in.Summary = SandboxNone, "No program to confine"
	case state == manifest.SandboxUndeclared:
		in.State, in.Summary = SandboxUndeclared, "Not sandboxed: the extension doesn't declare a sandbox (run.sandbox)"
		in.Details = append(in.Details, "It was written before Flint had a sandbox, or not updated for it. The program runs with your user's permissions.")
	case state == manifest.SandboxDeclaredNone:
		in.State, in.Summary = SandboxDeclaredOff, "Not sandboxed (declared by the extension)"
		in.Details = append(in.Details, "The extension says it can't work confined. The program runs with your user's permissions.")
	case !s.Available:
		in.State, in.Summary = SandboxUnavailable, fmt.Sprintf("Not sandboxed: not available on this system (%s)", s.Reason)
		in.Details = append(in.Details, "The extension asks to be confined, but here its program runs with your user's permissions.")
	default:
		in.State, in.Summary = SandboxOn, fmt.Sprintf("Sandboxed (%s)", s.Mechanism)
		in.Details = append(sandboxLines(m), s.Gaps...)
		in.Details = append(in.Details, notes...)
	}
	return in
}

// sandboxLines say what a confined program may do, from its manifest.
func sandboxLines(m *manifest.Manifest) []string {
	sb := m.Run.Sandbox
	reads := []string{"its own folder", "system files"}
	if m.HasPermission(manifest.PermWorkspaceRead) {
		reads = append(reads, "trusted project folders")
	}
	writes := []string{"its data folder", "a private temporary folder"}
	lines := []string{
		"Reads: " + strings.Join(append(reads, sb.Read...), ", "),
		"Writes: " + strings.Join(append(writes, sb.Write...), ", "),
	}
	switch hosts := m.NetworkHosts(); {
	case len(hosts) > 0:
		lines = append(lines, "Network: allowed, to any host (declared: "+strings.Join(hosts, ", ")+"; the sandbox can't limit it to them)")
	case sb.Network:
		lines = append(lines, "Network: allowed, to any host")
	default:
		lines = append(lines, "Network: blocked")
	}
	if sb.LocalSockets {
		lines = append(lines, "Local sockets: allowed (services on this machine, such as Docker or the desktop's message bus)")
	}
	if sb.Exec {
		lines = append(lines, "Other programs: may start any program it can read; they run in the same sandbox")
	} else {
		lines = append(lines, "Other programs: only its own")
	}
	return append(lines, fmt.Sprintf("Limits: %d open files, no core dumps", sandbox.MaxOpenFiles))
}

// sandboxNetwork: the confined program may open sockets.
func sandboxNetwork(m *manifest.Manifest) bool {
	return m.Run.Sandbox.Network || len(m.NetworkHosts()) > 0
}

// sandboxInfo is the extension's sandbox as Settings shows it.
func (e *ext) sandboxInfo() SandboxInfo {
	s := e.h.support()
	var notes []string
	if e.m.SandboxState() == manifest.SandboxDeclared && s.Available {
		if req, err := e.sandboxRequest("", nil); err == nil {
			notes = sandbox.Build(req).Notes
		}
	}
	in := describeSandbox(e.m, s, notes)
	if in.State != SandboxOn && in.State != SandboxNone && e.h.requireSandbox() {
		in.Refused = true
		in.Details = append(in.Details, "Flint doesn't start it: Settings ▸ Extensions only runs sandboxed extensions.")
	}
	return in
}

// sandboxRefusal is why the extension may not start at all: the user only
// runs sandboxed extensions and this one would run unconfined.
func (e *ext) sandboxRefusal() error {
	if !e.m.HasProgram() || !e.h.requireSandbox() {
		return nil
	}
	if in := describeSandbox(e.m, e.h.support(), nil); in.State != SandboxOn {
		return fmt.Errorf("%s is not started: Settings ▸ Extensions only runs sandboxed extensions, and it is %s", e.name(), lowerFirst(in.Summary))
	}
	return nil
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// sandboxRequest collects what the policy is built from. tmp is the
// process's private temporary folder ("" when only describing) and env the
// environment it gets (nil: Flint's own, which run.env's values come from).
func (e *ext) sandboxRequest(tmp string, env []string) (sandbox.Request, error) {
	resolve := filepath.EvalSymlinks
	dir, err := resolve(e.dir)
	if err != nil {
		return sandbox.Request{}, err
	}
	config := filepath.Dir(e.h.o.Dir)
	if real, err := resolve(config); err == nil {
		config = real
	}
	data := filepath.Join(config, "extension-data", e.id)
	home, err := os.UserHomeDir()
	if err != nil {
		return sandbox.Request{}, err
	}
	if real, err := resolve(home); err == nil {
		home = real
	}
	lookup := os.Getenv
	if env != nil {
		vars := map[string]string{}
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok {
				vars[k] = v
			}
		}
		lookup = func(name string) string { return vars[name] }
	}
	sb := e.m.Run.Sandbox
	req := sandbox.Request{
		GOOS: runtime.GOOS, Home: home, ConfigDir: config,
		ExtensionDir: dir, DataDir: data, TempDir: tmp,
		Read: sb.Read, Write: sb.Write, Network: sandboxNetwork(e.m), LocalSockets: sb.LocalSockets, Exec: sb.Exec,
		Env: lookup, Resolve: resolve,
	}
	if e.m.HasPermission(manifest.PermWorkspaceRead) {
		for _, r := range e.roots() {
			req.Roots = append(req.Roots, r.Path)
		}
	}
	return req, nil
}

// confined is what start keeps of a sandboxed process.
type confined struct {
	// roots are the project folders its sandbox lets it read: fixed for the
	// life of the process.
	roots []string
	// tmp is its private temporary folder, removed when it ends.
	tmp string
}

func (c *confined) cleanup() {
	if c != nil && c.tmp != "" {
		_ = os.RemoveAll(c.tmp)
	}
}

// confine rewrites cmd so the program starts inside its sandbox, when the
// manifest declares one and this system has one; otherwise cmd is left as it
// is and nil is returned, with the reason in the extension's log.
func (e *ext) confine(cmd *exec.Cmd) (*confined, error) {
	s := e.h.support()
	in := describeSandbox(e.m, s, nil)
	e.mu.Lock()
	e.sandboxed = in.State == SandboxOn
	e.mu.Unlock()
	if in.State != SandboxOn {
		if e.h.requireSandbox() {
			return nil, e.sandboxRefusal()
		}
		e.log.add("flint", lowerFirst(in.Summary))
		return nil, nil
	}
	tmp, err := os.MkdirTemp("", "flint-ext-")
	if err != nil {
		return nil, err
	}
	c := &confined{tmp: tmp}
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+tmp, "FLINT_EXTENSION_SANDBOX=1")
	req, err := e.sandboxRequest(tmp, cmd.Env)
	if err != nil {
		c.cleanup()
		return nil, err
	}
	if !req.Exec {
		req.Interpreters = sandbox.Interpreters(cmd.Path, req.Env("PATH"))
	}
	policy := sandbox.Build(req)
	launcher := e.h.o.Launcher
	if launcher == "" {
		if launcher, err = sandbox.Launcher(); err != nil {
			c.cleanup()
			return nil, err
		}
	}
	args, env, err := sandbox.Command(launcher, policy, s, cmd.Args)
	if err != nil {
		c.cleanup()
		return nil, err
	}
	cmd.Path, cmd.Args = args[0], args
	cmd.Env = append(cmd.Env, env...)
	c.roots = slices.Clone(req.Roots)
	e.log.add("flint", lowerFirst(in.Summary))
	for _, line := range append(slices.Clone(s.Gaps), policy.Notes...) {
		e.log.add("flint", "sandbox: "+line)
	}
	return c, nil
}

// sandboxParams is initialize's "sandbox": whether the process is confined.
func (e *ext) sandboxParams() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]any{"active": e.sandboxed}
	if e.sandboxed {
		out["network"] = sandboxNetwork(e.m)
		out["localSockets"] = e.m.Run.Sandbox.LocalSockets
		out["exec"] = e.m.Run.Sandbox.Exec
	}
	return out
}

// rootsOutgrew reports whether p's sandbox no longer matches the trusted
// project folders: a sandbox can't be widened while the process runs.
func (e *ext) rootsOutgrew(p *proc) bool {
	if p.confined == nil || !e.m.HasPermission(manifest.PermWorkspaceRead) {
		return false
	}
	var now []string
	for _, r := range e.roots() {
		now = append(now, r.Path)
	}
	return !slices.Equal(now, p.confined.roots)
}

// restartForRoots stops a confined process whose project folders changed and
// starts it again if it should be running (onStartup, a panel on screen);
// otherwise its next use does.
func (e *ext) restartForRoots() {
	e.mu.Lock()
	wanted := e.m.RunsAtStartup() || len(e.visible) > 0 || len(e.docs) > 0
	e.mu.Unlock()
	e.stop("the project folders changed, and its sandbox is rebuilt for them")
	e.h.changed()
	if !wanted {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), e.h.o.InitTimeout+time.Second)
	defer cancel()
	_, _ = e.session(ctx)
}

// SandboxSettingChanged stops the running extensions that the "only run
// sandboxed extensions" setting no longer allows.
func (h *Host) SandboxSettingChanged() {
	for _, e := range h.all() {
		if e.m == nil || e.running() == nil {
			continue
		}
		if err := e.sandboxRefusal(); err != nil {
			e.stop("only sandboxed extensions may run")
		}
	}
	h.changed()
}

// sandboxMessage ends the install dialog: how the program is confined, and
// what that means for trusting it.
func sandboxMessage(b *strings.Builder, in SandboxInfo) {
	fmt.Fprintf(b, "\nSandbox: %s\n", in.Summary)
	if in.State == SandboxOn {
		for _, line := range in.Details {
			fmt.Fprintf(b, "• %s\n", line)
		}
		b.WriteString("\nThe program runs inside this sandbox, with your user's permissions for what the sandbox allows. Install only extensions you trust.")
		return
	}
	b.WriteString("\nThe program runs with your user's permissions: Flint shows what it declares but cannot confine what it does. Install only extensions you trust.")
}
