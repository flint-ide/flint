//go:build linux

package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MoveToTrash moves path to the user's trash as the freedesktop.org Trash
// specification describes, so file managers can restore it: the file goes to
// $XDG_DATA_HOME/Trash/files and a .trashinfo next to it records where it came
// from. A file on another file system than the home trash can't be renamed
// there; `gio trash` (GLib, present wherever GTK is) handles those through the
// per-volume .Trash-$UID directories.
func MoveToTrash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	err = trashHome(abs, homeTrash(), time.Now())
	if errors.Is(err, syscall.EXDEV) {
		return gioTrash(abs)
	}
	return err
}

// homeTrash is the user's trash directory ($XDG_DATA_HOME/Trash).
func homeTrash() string {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "Trash")
}

// trashHome moves abs into trash. The .trashinfo is created first with
// O_EXCL, which also reserves a name no other trashing process can take.
func trashHome(abs, trash string, now time.Time) error {
	files, info := filepath.Join(trash, "files"), filepath.Join(trash, "info")
	for _, dir := range []string{files, info} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		(&url.URL{Path: abs}).EscapedPath(), now.Format("2006-01-02T15:04:05"))
	base := filepath.Base(abs)
	for i := 1; i < 10000; i++ {
		name := base
		if i > 1 {
			ext := filepath.Ext(base)
			name = strings.TrimSuffix(base, ext) + "." + strconv.Itoa(i) + ext
		}
		infoPath := filepath.Join(info, name+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := f.WriteString(content)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Rename(abs, filepath.Join(files, name))
		}
		if werr != nil {
			_ = os.Remove(infoPath)
		}
		return werr
	}
	return fmt.Errorf("no free name in %s for %s", files, base)
}

// gioTrash trashes abs with GLib's `gio trash`.
func gioTrash(abs string) error {
	gio, err := exec.LookPath("gio")
	if err != nil {
		return fmt.Errorf("%s is on another file system than the trash, and `gio` (GLib) isn't installed to trash it there", abs)
	}
	if out, err := exec.Command(gio, "trash", "--", abs).CombinedOutput(); err != nil {
		return fmt.Errorf("gio trash: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
