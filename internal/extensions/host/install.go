package host

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"flint/internal/extensions/manifest"
)

// Limits on a folder being installed.
const (
	maxInstallFiles = 10000
	maxInstallBytes = 512 << 20
)

// Install copies the extension in folder src into the extensions folder,
// after the user confirmed its id, name, program and permissions in a native
// dialog. An installed extension with the same id is replaced (stopped
// first); its settings in settings.json stay.
func (h *Host) Install(src string) (Info, error) {
	h.ensureDiscovered()
	src, err := filepath.Abs(src)
	if err != nil {
		return Info{}, err
	}
	if src, err = filepath.EvalSymlinks(src); err != nil {
		return Info{}, err
	}
	if dir, err := filepath.EvalSymlinks(h.o.Dir); err == nil && within(dir, src) {
		return Info{}, errors.New("this folder is already in Flint's extensions folder")
	}
	m, err := manifest.Load(src)
	if err != nil {
		return Info{}, err
	}
	h.mu.Lock()
	old := h.exts[m.ID]
	h.mu.Unlock()
	title := fmt.Sprintf("Install the extension %q?", m.Name)
	if old != nil && old.m != nil {
		title = fmt.Sprintf("Replace %s %s with %s?", old.m.Name, old.m.Version, m.Version)
	}
	if !h.o.App.Confirm(title, installMessage(m), "Install") {
		return Info{}, ErrCancelled
	}
	if err := os.MkdirAll(h.o.Dir, 0o700); err != nil {
		return Info{}, err
	}
	tmp := filepath.Join(h.o.Dir, ".install-"+m.ID+"-"+randomSuffix())
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return Info{}, fmt.Errorf("copy %s: %w", src, err)
	}
	copied, err := manifest.Load(tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return Info{}, err
	}
	if !reflect.DeepEqual(copied, m) {
		_ = os.RemoveAll(tmp)
		return Info{}, errors.New("the extension's manifest changed while it was being installed; nothing was installed")
	}
	if old != nil {
		old.stop("being replaced")
	}
	dst := filepath.Join(h.o.Dir, m.ID)
	trash := filepath.Join(h.o.Dir, ".old-"+m.ID+"-"+randomSuffix())
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, trash); err != nil {
			_ = os.RemoveAll(tmp)
			return Info{}, err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(trash, dst)
		_ = os.RemoveAll(tmp)
		return Info{}, err
	}
	_ = os.RemoveAll(trash)
	h.Discover()
	e, err := h.get(m.ID)
	if err != nil {
		return Info{}, err
	}
	return e.info(), nil
}

// languagesMessage lists the languages and the language servers they run;
// it reports whether any language has a server.
func languagesMessage(b *strings.Builder, langs []manifest.Language) bool {
	if len(langs) == 0 {
		return false
	}
	servers := false
	b.WriteString("\nLanguages:\n")
	for _, l := range langs {
		files := append(slices.Clone(l.Extensions), l.Filenames...)
		fmt.Fprintf(b, "• %s (%s)\n", l.Label, strings.Join(files, ", "))
		if l.Server == nil {
			continue
		}
		servers = true
		cmds := make([]string, len(l.Server.Candidates))
		for i, c := range l.Server.Candidates {
			cmds[i] = strings.Join(c, " ")
		}
		fmt.Fprintf(b, "  language server, the first one installed: %s (starts in trusted folders only)\n", strings.Join(cmds, " | "))
	}
	return servers
}

// toolsMessage lists the debuggers and test runners and the programs they
// start; it reports whether there is any.
func toolsMessage(b *strings.Builder, m *manifest.Manifest) bool {
	c := m.Contributes
	if len(c.Debuggers) > 0 {
		b.WriteString("\nDebuggers:\n")
	}
	for _, d := range c.Debuggers {
		cmds := make([]string, len(d.Adapter.Candidates))
		for i, cand := range d.Adapter.Candidates {
			cmds[i] = strings.Join(cand, " ")
		}
		fmt.Fprintf(b, "• %s, for %s files\n", d.Label, strings.Join(d.Languages, ", "))
		fmt.Fprintf(b, "  runs the first one installed: %s (in trusted folders only)\n", strings.Join(cmds, " | "))
	}
	if len(c.TestRunners) > 0 {
		b.WriteString("\nTest runners:\n")
	}
	for _, r := range c.TestRunners {
		fmt.Fprintf(b, "• %s, for %s files\n", r.Label, strings.Join(r.Languages, ", "))
		fmt.Fprintf(b, "  runs: %s (in trusted folders only)\n", r.CommandLine())
	}
	return len(c.Debuggers)+len(c.TestRunners) > 0
}

// installMessage lists what the user is about to allow.
func installMessage(m *manifest.Manifest) string {
	var b strings.Builder
	by := ""
	if m.Publisher != "" {
		by = " by " + m.Publisher
	}
	fmt.Fprintf(&b, "%s %s%s (%s)\n", m.Name, m.Version, by, m.ID)
	if m.Description != "" {
		fmt.Fprintf(&b, "%s\n", m.Description)
	}
	if n := len(m.Contributes.Themes); n > 0 {
		fmt.Fprintf(&b, "\nColor themes: %d\n", n)
	}
	servers := languagesMessage(&b, m.Contributes.Languages)
	tools := toolsMessage(&b, m)
	if !m.HasProgram() {
		if servers || tools {
			b.WriteString("\nNo program of its own: only the programs listed above run, and only in folders you trust.")
		} else {
			b.WriteString("\nNo program: this extension only adds data (themes, languages, settings) and runs no code.")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "\nProgram: %s\n", m.CommandLine(runtime.GOOS))
	if len(m.Run.Env) > 0 {
		fmt.Fprintf(&b, "Reads from your environment: %s\n", strings.Join(m.Run.Env, ", "))
	}
	if m.HasWebviews() {
		b.WriteString("Shows its own UI in Flint (webviews, in a sandboxed frame without network)\n")
	}
	if m.RunsAtStartup() {
		b.WriteString("Starts: when Flint starts (onStartup)\n")
	} else {
		b.WriteString("Starts: when you first use it\n")
	}
	b.WriteString("\nPermissions:\n")
	if len(m.Permissions) == 0 {
		b.WriteString("• none\n")
	}
	for _, p := range m.Permissions {
		fmt.Fprintf(&b, "• %s\n", PermissionText(p))
	}
	if n := len(m.Contributes.AgentTools); n > 0 {
		names := make([]string, n)
		for i, t := range m.Contributes.AgentTools {
			names[i] = t.Name
		}
		fmt.Fprintf(&b, "• Agent tools: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("\nThe program runs with your user's permissions: Flint shows what it declares but cannot confine what it does. Install only extensions you trust.")
	return b.String()
}

// PermissionText explains a permission in a sentence.
func PermissionText(p string) string {
	switch p {
	case manifest.PermWorkspaceRead:
		return "Read and open files in trusted project folders (workspace.read)"
	case manifest.PermSecrets:
		return "Keep its own secrets in the system keychain (secrets)"
	case manifest.PermAgentAsk:
		return "Write drafts in the agent chat for you to send (agent.ask)"
	case manifest.PermTerminal:
		return "Open terminal tabs that run commands it chooses (terminal)"
	}
	if hosts, ok := strings.CutPrefix(p, "network:"); ok {
		return "Network access to " + strings.ReplaceAll(hosts, ",", ", ") + " (declared; not enforced by Flint)"
	}
	return p
}

// Uninstall removes an installed extension's folder after the user
// confirmed. Its settings stay in settings.json and its secrets in the
// keychain, so installing it again restores them.
func (h *Host) Uninstall(id string) error {
	e, err := h.get(id)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Flint stops it and deletes %s.\n\nIts settings stay in settings.json, so installing it again restores them.", e.dir)
	if !h.o.App.Confirm(fmt.Sprintf("Uninstall %s?", e.name()), msg, "Uninstall") {
		return ErrCancelled
	}
	e.stop("uninstalled")
	trash := filepath.Join(h.o.Dir, ".old-"+id+"-"+randomSuffix())
	if err := os.Rename(e.dir, trash); err != nil {
		return err
	}
	err = os.RemoveAll(trash)
	h.Discover()
	return err
}

// maxInstallDepth bounds how deep folders in an extension may nest.
const maxInstallDepth = 32

// copyTree copies the regular files and folders of src into dst (new),
// keeping executable bits. .git is left out; a symbolic link is copied as
// the file it points to, which must be inside src. Everything is read
// through an os.Root (no path may lead outside src, however the folder
// changes during the copy), each regular file opened is checked to be the
// one the walk listed, and the byte limit holds while copying, so a file
// swapped for a FIFO, a link elsewhere or a growing file can't get in.
func copyTree(src, dst string) error {
	r, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer r.Close()
	entries, size := 0, int64(0)
	return fs.WalkDir(r.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if rel != "." {
			if entries++; entries > maxInstallFiles {
				return fmt.Errorf("more than %d files and folders", maxInstallFiles)
			}
			if strings.Count(rel, "/") >= maxInstallDepth {
				return fmt.Errorf("folders nested deeper than %d", maxInstallDepth)
			}
		}
		if d.IsDir() {
			if d.Name() == ".git" && rel != "." {
				return fs.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		n, err := copyFile(r, rel, target, d, maxInstallBytes-size)
		size += n
		return err
	})
}

// copyFile copies rel (inside r) to the new file to, at most budget bytes.
func copyFile(r *os.Root, rel, to string, d fs.DirEntry, budget int64) (int64, error) {
	// A symbolic link is followed only while it stays inside r; O_NONBLOCK
	// keeps a FIFO from blocking the open.
	in, err := r.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if d.Type()&fs.ModeSymlink != 0 {
			return 0, fmt.Errorf("%s links outside the folder or to nothing", rel)
		}
		return 0, err
	}
	defer in.Close()
	got, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !got.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", rel)
	}
	if d.Type().IsRegular() {
		want, err := d.Info()
		if err != nil {
			return 0, err
		}
		if !os.SameFile(got, want) {
			return 0, fmt.Errorf("%s changed while it was being copied", rel)
		}
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, got.Mode().Perm()&0o755|0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(in, budget+1))
	if err == nil && n > budget {
		err = fmt.Errorf("more than %d MiB", maxInstallBytes>>20)
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func randomSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// within: path is root or inside it (both cleaned and absolute).
func within(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
