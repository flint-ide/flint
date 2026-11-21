package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// typecheck runs TypeScript over the page's sources (every .ts/.tsx file in the
// entry's directory and below) against this checkout's @flint-ide/ui and
// frontend/node_modules, the same modules the bundle uses. The extension needs
// no tsconfig.json or node_modules of its own: the config is written to a
// temporary directory with absolute paths.
func typecheck(flintRoot, entry string) error {
	root, err := filepath.Abs(flintRoot)
	if err != nil {
		return err
	}
	entry, err = filepath.Abs(entry)
	if err != nil {
		return err
	}
	modules := filepath.Join(root, "frontend", "node_modules")
	tsc := filepath.Join(modules, "typescript", "bin", "tsc")
	if _, err := os.Stat(tsc); err != nil {
		return fmt.Errorf("%s has no typescript: run `npm ci --ignore-scripts` in frontend/ first", modules)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("type checking needs Node.js: %w", err)
	}
	dir, err := os.MkdirTemp("", "flint-webview-check")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	config, err := checkConfig(root, filepath.Dir(entry), dir)
	if err != nil {
		return err
	}
	cmd := exec.Command(node, tsc, "-p", config, "--pretty")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("type check of %s failed", filepath.Dir(entry))
	}
	return nil
}

// checkConfig writes tsconfig.json (and a declaration for CSS imports) into dir
// and returns its path.
func checkConfig(root, src, dir string) (string, error) {
	modules := filepath.Join(root, "frontend", "node_modules")
	ui := filepath.Join(root, "ui")
	decls := filepath.Join(dir, "flint-webview.d.ts")
	if err := os.WriteFile(decls, []byte("declare module '*.css'\n"), 0o644); err != nil {
		return "", err
	}
	config := map[string]any{
		"compilerOptions": map[string]any{
			"target":           "ESNext",
			"lib":              []string{"DOM", "DOM.Iterable", "ESNext"},
			"module":           "ESNext",
			"moduleResolution": "Bundler",
			"jsx":              "react-jsx",
			"strict":           true,
			"skipLibCheck":     true,
			"isolatedModules":  true,
			"noEmit":           true,
			"typeRoots":        []string{filepath.Join(modules, "@types")},
			"paths": map[string][]string{
				"@flint-ide/ui":             {filepath.Join(ui, "src", "index.ts")},
				"@flint-ide/ui/icons":       {filepath.Join(ui, "src", "icons.ts")},
				"@flint-ide/ui/styles.css":  {filepath.Join(ui, "css", "index.css")},
				"@flint-ide/ui/webview.css": {filepath.Join(ui, "css", "webview.css")},
				"*":                         {filepath.Join(modules, "@types", "*"), filepath.Join(modules, "*")},
			},
		},
		"include": []string{filepath.Join(src, "**", "*.ts"), filepath.Join(src, "**", "*.tsx"), decls},
		"exclude": []string{filepath.Join(src, "**", "node_modules")},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "tsconfig.json")
	return path, os.WriteFile(path, data, 0o644)
}
