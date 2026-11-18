package projects

// Migration of the pre-rename format of this feature, which was called
// Backlog on main for a short time (never released): a project folder marked
// by flint-backlog.json instead of flint-project.json. Such a folder is still
// a project "in the old format": it is listed and its items are shown, but it
// is read-only in Flint (no new item, status change, Delegate, start, agent
// tool or project context), so nothing new is written in the old format,
// until the user renames the file with the one action it offers
// (RenameOldConfig). Flint never renames it by itself: the folder may be a
// shared git repository. When both files exist, flint-project.json is used and
// the old one is listed as a leftover problem. This file can be removed once
// nobody runs a main from before the rename.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// OldConfigFile marked a project before the feature was renamed.
const OldConfigFile = "flint-backlog.json"

// errOldFormat refuses every write to a project in the old format.
var errOldFormat = fmt.Errorf("this project is in the old format (%s) and read-only in Flint: rename the file first (Rename %s to %s in the Projects panel)", OldConfigFile, OldConfigFile, ConfigFile)

// configFile is the file that marks folder as a project: ConfigFile, or
// OldConfigFile when only that one exists.
func configFile(folder string) string {
	if _, err := os.Lstat(filepath.Join(folder, ConfigFile)); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Lstat(filepath.Join(folder, OldConfigFile)); err == nil {
			return OldConfigFile
		}
	}
	return ConfigFile
}

// hasConfigFile reports whether folder's top holds a regular ConfigFile or,
// in the old format, OldConfigFile.
func hasConfigFile(folder string) bool {
	for _, name := range []string{ConfigFile, OldConfigFile} {
		if info, err := os.Stat(filepath.Join(folder, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// writable refuses a project in the old format.
func writable(c Config) error {
	if c.OldFormat {
		return errOldFormat
	}
	return nil
}

// leftoverProblem reports an OldConfigFile next to the ConfigFile in use.
func leftoverProblem(folder string, c Config) (Problem, bool) {
	if c.OldFormat {
		return Problem{}, false
	}
	if _, err := os.Lstat(filepath.Join(folder, OldConfigFile)); err != nil {
		return Problem{}, false
	}
	return Problem{File: OldConfigFile, Reason: fmt.Sprintf("left over from the old format: Flint uses %s and ignores this file; delete it when nobody needs it", ConfigFile)}, true
}

// RenameOldConfig renames a project's OldConfigFile to ConfigFile after the
// user confirmed it (Rename flint-backlog.json to flint-project.json). The
// content is not touched, and it is refused when ConfigFile exists: the
// rename never replaces a file.
func (a *API) RenameOldConfig(folder string) (ProjectInfo, error) {
	real, workspace, err := a.known(folder)
	if err != nil {
		return ProjectInfo{}, err
	}
	oldPath, newPath := filepath.Join(real, OldConfigFile), filepath.Join(real, ConfigFile)
	check := func() error {
		if _, err := os.Lstat(newPath); err == nil {
			return fmt.Errorf("the project has a %s already: Flint uses it, and %s is left over (delete it yourself if nobody needs it)", ConfigFile, OldConfigFile)
		}
		info, err := os.Lstat(oldPath)
		if err != nil {
			return fmt.Errorf("the project has no %s", OldConfigFile)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file: rename it yourself", OldConfigFile)
		}
		return nil
	}
	if err := check(); err != nil {
		return ProjectInfo{}, err
	}
	c, err := LoadConfig(real)
	if err != nil {
		return ProjectInfo{}, err
	}
	msg := fmt.Sprintf("Rename %s to %s in %s?\n\nThe project %q is in the format of the Backlog preview. Its content stays as it is. If the folder is in a git repository, commit the rename so your team gets it too.", OldConfigFile, ConfigFile, real, c.Name)
	if !a.host.Confirm("Rename the project file?", msg, "Rename") {
		return ProjectInfo{}, errors.New("the rename was cancelled")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := check(); err != nil { // the folder may have changed while the question was open
		return ProjectInfo{}, err
	}
	if err := renameNoReplace(oldPath, newPath); err != nil {
		return ProjectInfo{}, err
	}
	a.emit(real)
	a.emit("")
	a.kickStarter()
	c, err = LoadConfig(real)
	if err != nil {
		return ProjectInfo{}, err
	}
	return ProjectInfo{Path: real, Name: c.Name, Workspace: workspace || slices.Contains(a.workspaceProjects(), real), Trusted: a.host.Trusted(real)}, nil
}

// renameNoReplace renames oldPath to newPath, refusing when newPath exists:
// a hard link to the new name (which fails on an existing file) and then the
// old name's removal. Where hard links are not supported, a plain rename
// after a last check.
func renameNoReplace(oldPath, newPath string) error {
	err := os.Link(oldPath, newPath)
	switch {
	case err == nil:
		return os.Remove(oldPath)
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s exists already: nothing was renamed", filepath.Base(newPath))
	}
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("%s exists already: nothing was renamed", filepath.Base(newPath))
	}
	return os.Rename(oldPath, newPath)
}
