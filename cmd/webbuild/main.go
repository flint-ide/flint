// Command webbuild bundles the React frontend with esbuild's Go API, so the build
// needs no JavaScript toolchain beyond the runtime dependencies themselves.
//
//	webbuild            production build into frontend/dist (--typecheck: tsc first)
//	webbuild --watch    rebuild on change and reload the page (wails dev)
//	webbuild --test     compile *.test.ts(x) and run them with `node --test`
//	                    (--test -- [node flags] [file filters])
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	watch := flag.Bool("watch", false, "rebuild on change and serve with live reload")
	test := flag.Bool("test", false, "compile *.test.ts(x) files and run them with node --test")
	typecheck := flag.Bool("typecheck", false, "run tsc --noEmit before the build (release builds)")
	port := flag.Int("port", 5173, "dev server port for --watch (frontend:dev:serverUrl)")
	dir := flag.String("dir", "", "frontend directory (default: . when it has index.html, else ./frontend)")
	flag.Parse()

	root, err := frontendDir(*dir)
	if err != nil {
		fail(err)
	}
	switch {
	case *test:
		code, err := runTests(root, flag.Args())
		if err != nil {
			fail(err)
		}
		os.Exit(code)
	case *watch:
		err = serve(root, *port)
	default:
		if *typecheck {
			err = runTypecheck(root)
		}
		if err == nil {
			err = build(root)
		}
	}
	if err != nil {
		fail(err)
	}
}

// frontendDir resolves the frontend root: wails runs frontend commands from
// frontend/, make and humans usually from the repository root.
func frontendDir(flagDir string) (string, error) {
	candidates := []string{".", "frontend"}
	if flagDir != "" {
		candidates = []string{flagDir}
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
			return filepath.Abs(c)
		}
	}
	return "", fmt.Errorf("no frontend found (looked for index.html in %v)", candidates)
}

// runTypecheck runs the locally installed TypeScript compiler: esbuild only strips
// types, so without it a release build would ship type errors.
func runTypecheck(root string) error {
	cmd := exec.Command(filepath.Join(root, "node_modules", ".bin", "tsc"), "--noEmit", "-p", root)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("typecheck failed: %w", err)
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "webbuild:", err)
	os.Exit(1)
}
