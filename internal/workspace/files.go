package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"

	"flint/internal/ripgrep"
)

// MaxListedFiles caps the quick-open index so huge monorepos stay responsive.
const MaxListedFiles = 200_000

// skippedDirs are ignored by the pure-Go fallback (rg uses .gitignore instead).
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, ".idea": true, ".venv": true,
	"__pycache__": true, "dist": true, "target": true, ".next": true,
}

// ListFiles returns paths relative to root for quick-open. It prefers `rg --files`,
// which honours .gitignore, and falls back to a plain directory walk.
func ListFiles(ctx context.Context, root string) ([]string, error) {
	if err := requireAbs(root); err != nil {
		return nil, err
	}
	if rg := ripgrep.Path(); rg != "" {
		return listWithRipgrep(ctx, rg, root)
	}
	return listWithWalk(ctx, root)
}

func listWithRipgrep(ctx context.Context, rg, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, rg, "--files", "--hidden", "--glob", "!.git")
	cmd.Dir = root
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("rg stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start rg: %w", err)
	}

	files := make([]string, 0, 1024)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() && len(files) < MaxListedFiles {
		files = append(files, filepath.ToSlash(sc.Text()))
	}
	scanErr := sc.Err()
	if len(files) >= MaxListedFiles {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()

	if scanErr != nil {
		return nil, fmt.Errorf("read rg output: %w", scanErr)
	}
	// rg exits 1 when nothing matched (empty dir) and 2 on partial errors such as
	// unreadable directories; both still produce a usable list.
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && ctx.Err() == nil {
		return nil, fmt.Errorf("rg --files: %w", waitErr)
	}
	return files, nil
}

func listWithWalk(ctx context.Context, root string) ([]string, error) {
	files := make([]string, 0, 1024)
	errLimit := errors.New("limit reached")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != root && SkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= MaxListedFiles {
			return errLimit
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLimit) {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return files, nil
}

// SkipDir reports whether a directory is ignored when rg (and .gitignore) isn't available.
func SkipDir(name string) bool {
	return skippedDirs[name]
}
