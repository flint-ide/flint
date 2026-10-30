package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// runTests bundles every src/**/*.test.ts(x) for Node into a temp directory and
// runs them with the built-in test runner. Arguments starting with "-" are
// passed to node (e.g. --test-reporter=tap, --test-name-pattern=… from Flint's
// test runner); the others are filters that keep only test files whose path
// contains one of them. Returns node's exit code.
func runTests(root string, args []string) (int, error) {
	var filters, nodeFlags []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			nodeFlags = append(nodeFlags, a)
		} else {
			filters = append(filters, a)
		}
	}
	tests, err := findTests(filepath.Join(root, "src"), filters)
	if err != nil {
		return 1, err
	}
	if len(tests) == 0 {
		return 1, errors.New("no test files found")
	}
	out, err := os.MkdirTemp("", "webbuild-test-")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(out)
	// Source maps hold paths relative to the output; node resolves them from the
	// real path, so a symlinked temp dir (/var → /private/var on macOS) would
	// make stack traces point at files that don't exist.
	if real, err := filepath.EvalSymlinks(out); err == nil {
		out = real
	}

	result := api.Build(api.BuildOptions{
		AbsWorkingDir: root,
		EntryPoints:   tests,
		Outdir:        out,
		Outbase:       filepath.Join(root, "src"),
		OutExtension:  map[string]string{".js": ".mjs"},
		Bundle:        true,
		Write:         true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformNode,
		Target:        api.ESNext,
		JSX:           api.JSXAutomatic,
		Sourcemap:     api.SourceMapInline,
		Define:        map[string]string{"process.env.NODE_ENV": `"test"`},
		// CSS imported by components is irrelevant under Node.
		Loader:   map[string]api.Loader{".css": api.LoaderEmpty},
		LogLevel: api.LogLevelWarning,
		Color:    api.ColorIfTerminal,
	})
	if len(result.Errors) > 0 {
		return 1, fmt.Errorf("compiling tests failed with %d error(s)", len(result.Errors))
	}

	nodeArgs := append([]string{"--enable-source-maps", "--test"}, nodeFlags...)
	for _, t := range tests {
		rel, _ := filepath.Rel(filepath.Join(root, "src"), t)
		nodeArgs = append(nodeArgs, filepath.Join(out, strings.TrimSuffix(rel, filepath.Ext(rel))+".mjs"))
	}
	cmd := exec.Command("node", nodeArgs...)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return exit.ExitCode(), nil
		}
		return 1, fmt.Errorf("run node: %w", err)
	}
	return 0, nil
}

func findTests(src string, filters []string) ([]string, error) {
	var tests []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".test.ts") && !strings.HasSuffix(name, ".test.tsx") {
			return nil
		}
		if matchesAny(path, filters) {
			tests = append(tests, path)
		}
		return nil
	})
	return tests, err
}

func matchesAny(path string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if strings.Contains(path, f) {
			return true
		}
	}
	return false
}
