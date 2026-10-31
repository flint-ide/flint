package lsp

import (
	"os"
	"path/filepath"
	"strings"
)

// Opening a file must not run code the repository ships. typescript-language-server
// prefers the TypeScript in the workspace's node_modules over its own, so a cloned
// repo could put its own "tsserver" there and have it started just by opening a .ts
// file. Point it at the TypeScript installed next to the server instead.
// (vtsls, tried first, uses its bundled TypeScript unless told otherwise.)

// initializationOptions returns the initialize options for the server binary.
func initializationOptions(bin string) map[string]any {
	if filepath.Base(bin) != "typescript-language-server" {
		return nil
	}
	if ts := tsserverNextTo(bin); ts != "" {
		return map[string]any{"tsserver": map[string]any{"path": ts}}
	}
	return nil
}

// tsserverNextTo finds tsserver.js of the TypeScript installed with the
// language server: in its own node_modules or alongside it (npm -g, Homebrew).
func tsserverNextTo(bin string) string {
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return ""
	}
	// .../node_modules/typescript-language-server/lib/cli.mjs → the package dir.
	pkg := ""
	for dir := filepath.Dir(real); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "typescript-language-server" && filepath.Base(filepath.Dir(dir)) == "node_modules" {
			pkg = dir
			break
		}
	}
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
