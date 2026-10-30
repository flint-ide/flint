package runconfig

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/shop\n\ngo 1.26\n")
	write(t, root, "main.go", "package main\nfunc main() {}\n")
	write(t, root, "cmd/worker/main.go", "// worker\npackage main\n")
	write(t, root, "cmd/worker/main_test.go", "package main\n")
	write(t, root, "internal/lib/lib.go", "package lib\n")
	write(t, root, "internal/lib/testdata/x/main.go", "package main\n")
	write(t, root, ".hidden/main.go", "package main\n")
	write(t, root, "node_modules/x/main.go", "package main\n")
	write(t, root, "tools/go.mod", "module tools\n")
	write(t, root, "tools/main.go", "package main\n")
	write(t, root, "Makefile", "VAR := 1\nX ?= 2\n.PHONY: build test\nbuild: ## Build\n\tgo build\ntest test-go: build\n%.o: %.c\n\tcc\nrun:\n")
	write(t, root, "web/package.json", `{"scripts":{"dev":"vite","build":"vite build"}}`)
	write(t, root, "web/yarn.lock", "")
	write(t, root, "package.json", `{"name":"x"}`)

	var names []string
	for _, c := range Detect(root) {
		if !IsDetected(c.ID) {
			t.Errorf("%s: id %q lacks the detected prefix", c.Name, c.ID)
		}
		if err := Validate(c); err != nil {
			t.Errorf("%s: detected config is invalid: %v", c.Name, err)
		}
		names = append(names, c.Name)
	}
	want := []string{"shop", "worker", "make build", "make test", "make test-go", "make run", "yarn run build (web)", "yarn run dev (web)"}
	if !slices.Equal(names, want) {
		t.Errorf("detected\n got %q\nwant %q", names, want)
	}
}

func TestDetectEmpty(t *testing.T) {
	if got := Detect(t.TempDir()); len(got) != 0 {
		t.Errorf("empty dir: %+v", got)
	}
}
