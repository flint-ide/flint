package cilog

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Bounds of what one resolver does for an untrusted log (security limits, not
// settings): a log can name any number of packages and paths.
const (
	// maxPathLen bounds a path read from a log.
	maxPathLen = 1024
	// MaxPkgDirs caps the failed Go packages whose folders bare names are looked up in.
	MaxPkgDirs = 32
	// maxPkgCandidates caps the FAIL lines looked at to find them.
	maxPkgCandidates = 256
	// MaxStats caps the file-system lookups of one resolver.
	MaxStats = 4096
)

// Resolver maps paths written by CI (absolute paths on the runner, paths from
// the repository root, bare file names printed by go test) to files of a
// workspace folder. It only checks which files exist, never reads them, never
// leaves the folder (symlinks included: lookups go through os.Root), and stops
// after MaxStats lookups or when ctx ends.
type Resolver struct {
	ctx  context.Context
	root *os.Root
	abs  string // the folder, slash-separated
	// pkgDirs are root-relative folders of Go packages the log says failed,
	// where go test's bare file names (review_test.go:316) live.
	pkgDirs []string
	cache   map[string]string
	stats   int
}

// failRE: go test's "FAIL<TAB>import/path ..." summary lines.
var failRE = regexp.MustCompile(`^FAIL\s+(\S+)`)

// NewResolver returns a resolver for dir that knows the failed Go packages
// named in log. Close it when done.
func NewResolver(ctx context.Context, dir string, log []string) *Resolver {
	r := &Resolver{ctx: ctx, abs: filepath.ToSlash(filepath.Clean(dir)), cache: map[string]string{}}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return r // resolves nothing
	}
	r.root = root
	module := r.goModule()
	if module == "" {
		return r
	}
	seen := map[string]bool{}
	candidates := 0
	for _, l := range log {
		if len(r.pkgDirs) == MaxPkgDirs || candidates == maxPkgCandidates {
			break
		}
		m := failRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		dir := ""
		switch {
		case m[1] == module:
			dir = "."
		case strings.HasPrefix(m[1], module+"/"):
			dir = path.Clean(strings.TrimPrefix(m[1], module+"/"))
		default:
			continue
		}
		if seen[dir] || !filepath.IsLocal(dir) {
			continue
		}
		seen[dir] = true
		candidates++
		if fi, ok := r.stat(dir); ok && fi.IsDir() {
			r.pkgDirs = append(r.pkgDirs, dir)
		}
	}
	return r
}

// Close releases the folder.
func (r *Resolver) Close() {
	if r.root != nil {
		r.root.Close()
	}
}

// goModule reads the module path from the folder's go.mod (its first 64 KB).
func (r *Resolver) goModule() string {
	f, err := r.root.Open("go.mod")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 64<<10))
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// Resolve returns the root-relative, slash-separated, clean path of the
// workspace file p refers to, and whether there is one.
func (r *Resolver) Resolve(p string) (string, bool) {
	if got, ok := r.cache[p]; ok {
		return got, got != ""
	}
	got := r.resolve(p)
	r.cache[p] = got
	return got, got != ""
}

func (r *Resolver) resolve(p string) string {
	if r.root == nil || p == "" || len(p) > maxPathLen || strings.ContainsRune(p, 0) {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) > 2 && p[1] == ':' && p[2] == '/' { // a Windows drive
		p = p[2:]
	}
	if strings.HasPrefix(p, "/") {
		p = path.Clean(p)
		if rel, ok := strings.CutPrefix(p, r.abs+"/"); ok && r.isFile(rel) {
			return rel
		}
		return r.suffix(strings.Split(strings.Trim(p, "/"), "/"), 2)
	}
	p = path.Clean(p)
	if !filepath.IsLocal(p) {
		return ""
	}
	if r.isFile(p) {
		return p
	}
	for _, dir := range r.pkgDirs {
		if c := path.Join(dir, p); r.isFile(c) {
			return c
		}
	}
	if !strings.Contains(p, "/") {
		return ""
	}
	return r.suffix(strings.Split(p, "/")[1:], 1)
}

// suffix tries parts, then parts without its first element, and so on, while
// at least minParts remain: /home/runner/work/repo/repo/cmd/x.go → cmd/x.go.
func (r *Resolver) suffix(parts []string, minParts int) string {
	for i := 0; len(parts)-i >= minParts; i++ {
		c := path.Clean(strings.Join(parts[i:], "/"))
		if r.isFile(c) {
			return c
		}
	}
	return ""
}

func (r *Resolver) isFile(rel string) bool {
	fi, ok := r.stat(rel)
	return ok && fi.Mode().IsRegular()
}

// stat looks rel up inside the folder, within the budget.
func (r *Resolver) stat(rel string) (fs.FileInfo, bool) {
	if !filepath.IsLocal(rel) || r.stats >= MaxStats || r.ctx.Err() != nil {
		return nil, false
	}
	r.stats++
	fi, err := r.root.Stat(filepath.FromSlash(rel))
	return fi, err == nil
}
