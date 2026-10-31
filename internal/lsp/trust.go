package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Opening a file must not run code the repository ships. typescript-language-server
// prefers the TypeScript in the workspace's node_modules over its own, so a cloned
// repo could put its own "tsserver" there and have it started just by opening a .ts
// file. Point it at the TypeScript installed next to the server instead.
// (vtsls, tried first, uses its bundled TypeScript unless told otherwise.)

// ErrRestricted: the folder isn't trusted and the server would run its code.
var ErrRestricted = errors.New("Restricted Mode: trust this folder to start its language server")

// needsTrust are servers that execute the project's own code when they start:
// rust-analyzer runs build scripts, proc macros and .cargo/config tools; the
// Python servers run the interpreter of a virtualenv the project points at.
var needsTrust = map[string]string{
	"rust":   "rust-analyzer runs the project's build scripts and proc macros",
	"python": "the Python language server runs the project's interpreter",
}

// initializationOptions returns the initialize options for the server binary.
// typescript-language-server that can't be pinned to its own TypeScript is
// refused (it would load the project's node_modules/typescript instead).
func initializationOptions(bin string) (map[string]any, error) {
	// A server set in Settings may be a link or wrapper under another name.
	if filepath.Base(bin) != "typescript-language-server" && tslsPackage(bin) == "" {
		return nil, nil
	}
	if ts := tsserverNextTo(bin); ts != "" {
		return map[string]any{"tsserver": map[string]any{"path": ts}}, nil
	}
	return nil, errors.New("typescript-language-server: no TypeScript installed next to it, and the project's own is never used — run: npm i -g typescript-language-server typescript (or install vtsls)")
}

// tslsPackage returns the typescript-language-server package directory bin
// links into (.../node_modules/typescript-language-server/lib/cli.mjs), or "".
func tslsPackage(bin string) string {
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return ""
	}
	for dir := filepath.Dir(real); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "typescript-language-server" && filepath.Base(filepath.Dir(dir)) == "node_modules" {
			return dir
		}
	}
	return ""
}

// tsserverNextTo finds tsserver.js of the TypeScript installed with the
// language server: in its own node_modules or alongside it (npm -g, Homebrew).
func tsserverNextTo(bin string) string {
	pkg := tslsPackage(bin)
	if pkg == "" {
		return ""
	}
	for _, p := range []string{
		filepath.Join(pkg, "node_modules", "typescript", "lib", "tsserver.js"),
		filepath.Join(filepath.Dir(pkg), "typescript", "lib", "tsserver.js"),
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && strings.HasSuffix(p, "tsserver.js") {
			return p
		}
	}
	return ""
}
