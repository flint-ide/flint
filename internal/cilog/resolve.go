package cilog

import (
	"bufio"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// maxPathLen bounds a path read from a log.
const maxPathLen = 1024

// Resolver maps paths written by CI (absolute paths on the runner, paths from
// the repository root, bare file names printed by go test) to files of a
// workspace folder. It only checks which files exist; it never reads them.
type Resolver struct {
	root string
	// pkgDirs are root-relative folders of Go packages the log says failed,
	// where go test's bare file names (review_test.go:316) live.
	pkgDirs []string
	cache   map[string]string
}

// failRE: go test's "FAIL<TAB>import/path ..." summary lines.
var failRE = regexp.MustCompile(`^FAIL\s+(\S+)`)

// NewResolver returns a resolver for root that knows the failed Go packages
// named in log.
func NewResolver(root string, log []string) *Resolver {
	r := &Resolver{root: root, cache: map[string]string{}}
	module := goModule(filepath.Join(root, "go.mod"))
	if module == "" {
		return r
	}
	seen := map[string]bool{}
	for _, l := range log {
		m := failRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		dir := ""
		switch {
		case m[1] == module:
			dir = "."
		case strings.HasPrefix(m[1], module+"/"):
			dir = strings.TrimPrefix(m[1], module+"/")
		default:
			continue
		}
		if !seen[dir] && filepath.IsLocal(dir) {
			seen[dir] = true
			r.pkgDirs = append(r.pkgDirs, dir)
		}
	}
	return r
}

// goModule reads the module path from a go.mod (its first 64 KB).
func goModule(gomod string) string {
	f, err := os.Open(gomod)
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

// Resolve returns the root-relative, slash-separated path of the workspace
// file p refers to, and whether there is one.
func (r *Resolver) Resolve(p string) (string, bool) {
	if got, ok := r.cache[p]; ok {
		return got, got != ""
	}
	got := r.resolve(p)
	r.cache[p] = got
	return got, got != ""
}

func (r *Resolver) resolve(p string) string {
	if p == "" || len(p) > maxPathLen || strings.ContainsRune(p, 0) {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) > 2 && p[1] == ':' && p[2] == '/' { // a Windows drive
		p = p[2:]
	}
	if strings.HasPrefix(p, "/") {
		if rel, ok := strings.CutPrefix(p, filepath.ToSlash(r.root)+"/"); ok && r.isFile(rel) {
			return path.Clean(rel)
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
		c := strings.Join(parts[i:], "/")
		if filepath.IsLocal(c) && r.isFile(c) {
			return c
		}
	}
	return ""
}

func (r *Resolver) isFile(rel string) bool {
	if !filepath.IsLocal(rel) {
		return false
	}
	fi, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(rel)))
	return err == nil && fi.Mode().IsRegular()
}
