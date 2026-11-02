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
	"strings"

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
	if _, err := manifest.Load(tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return Info{}, err
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
	fmt.Fprintf(&b, "\nProgram: %s\n", strings.Join(append([]string{m.Run.Command}, m.Run.Args...), " "))
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

// copyTree copies the regular files and folders of src into dst (new),
// keeping executable bits. .git is left out; a symbolic link is copied as
// the file it points to, which must be inside src.
func copyTree(src, dst string) error {
	files, size := 0, int64(0)
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == ".git" && path != src {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		from := path
		if d.Type()&fs.ModeSymlink != 0 {
			if from, err = filepath.EvalSymlinks(path); err != nil {
				return err
			}
			if !within(src, from) {
				return fmt.Errorf("%s links outside the folder", rel)
			}
		}
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		if files++; files > maxInstallFiles {
			return fmt.Errorf("more than %d files", maxInstallFiles)
		}
		if size += info.Size(); size > maxInstallBytes {
			return fmt.Errorf("more than %d MiB", maxInstallBytes>>20)
		}
		return copyFile(from, target, info.Mode().Perm()&0o755)
	})
}

func copyFile(from, to string, mode fs.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
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
