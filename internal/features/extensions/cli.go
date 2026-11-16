package extensions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallFlag is the command-line flag that installs an extension from a
// folder: `flint --install-extension <dir>`. It is the command-line form of
// "Install Extension from Folder…" and goes through the same native
// confirmation; it can't install anything without it.
const InstallFlag = "--install-extension"

// EventInstallRequest carries InstallRequest to the frontend when a second
// `flint --install-extension <dir>` was handed to the running Flint.
const EventInstallRequest = "extensions:install-request"

// InstallRequest is a folder to install from the command line, or why the
// flag can't be used.
type InstallRequest struct {
	// Dir is the absolute folder ("" when Error is set or nothing was asked).
	Dir   string `json:"dir"`
	Error string `json:"error"`
}

// InstallArg finds `--install-extension <dir>` (or `--install-extension=<dir>`)
// in args. found says the flag is there; dir is then the folder, absolute
// (a relative one is taken from cwd, or from the process's working folder
// when cwd is ""), or err says why the value can't be used. Only the first
// occurrence counts. The folder's content is checked by the install itself.
func InstallArg(args []string, cwd string) (dir string, found bool, err error) {
	value, ok := "", false
	for i, arg := range args {
		if arg == InstallFlag {
			if i+1 >= len(args) {
				return "", true, fmt.Errorf("%s needs a folder: flint %s <folder>", InstallFlag, InstallFlag)
			}
			value, ok = args[i+1], true
			break
		}
		if v, has := strings.CutPrefix(arg, InstallFlag+"="); has {
			value, ok = v, true
			break
		}
	}
	if !ok {
		return "", false, nil
	}
	if value == "" || strings.ContainsRune(value, 0) {
		return "", true, fmt.Errorf("%s needs a folder: flint %s <folder>", InstallFlag, InstallFlag)
	}
	if cwd != "" && !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", true, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", true, fmt.Errorf("%s: %s is not a folder", InstallFlag, abs)
	}
	return abs, true, nil
}

// WithoutInstallArg is args without the flag and its value, so the folder
// to install is not taken for a project folder to open.
func WithoutInstallArg(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == InstallFlag {
			i++
			continue
		}
		if strings.HasPrefix(args[i], InstallFlag+"=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// RequestOf turns InstallArg's result into what the frontend gets; ok is
// false when the flag is absent.
func RequestOf(args []string, cwd string) (InstallRequest, bool) {
	dir, found, err := InstallArg(args, cwd)
	if !found {
		return InstallRequest{}, false
	}
	if err != nil {
		return InstallRequest{Error: err.Error()}, true
	}
	return InstallRequest{Dir: dir}, true
}
