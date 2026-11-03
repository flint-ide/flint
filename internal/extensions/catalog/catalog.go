// Package catalog is the extensions repository Settings ▸ Extensions shows
// by default: a shallow git clone of github.com/flint-ide/extensions (or
// FLINT_EXTENSIONS_REPO) kept in Flint's cache folder, read as one entry per
// folder with a flint-extension.json, and built on the user's machine before
// it is installed.
//
// git runs with the user's own credentials (credential helper, SSH keys), so
// a private repository works for whoever can clone it. Nothing here installs:
// Prepare returns a folder the host's Install copies after its native
// confirmation, exactly like "Install from Folder…".
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"flint/internal/extensions/manifest"
)

// DefaultURL is the repository of Flint's own extensions.
const DefaultURL = "https://github.com/flint-ide/extensions.git"

// URLEnv overrides DefaultURL (a fork, a mirror, a local path).
const URLEnv = "FLINT_EXTENSIONS_REPO"

// Limits on the git and build commands.
const (
	gitTimeout   = 2 * time.Minute
	buildTimeout = 5 * time.Minute
	maxOutput    = 4 << 10
)

// ErrUnknown: no such folder in the repository.
var ErrUnknown = errors.New("this extension is not in the extensions repository")

// Options configure a Catalog.
type Options struct {
	// Dir is where the clone lives (created on first Refresh).
	Dir string
	// URL is the repository; "" uses URLEnv, then DefaultURL.
	URL string
	// Before runs before every git or go command (the login-shell PATH
	// import); nil does nothing.
	Before func()
}

// Catalog is the cloned repository.
//
// Two locks: build serializes go builds; files guards the clone's contents
// (readers: List and every install from Prepare until its release; the
// writer: Refresh). A refresh therefore never changes a folder while its
// install confirmation is open, and a long build never blocks List.
type Catalog struct {
	o     Options
	build sync.Mutex
	files sync.RWMutex
}

// New returns the catalog; nothing runs until List, Refresh or Prepare.
func New(o Options) *Catalog {
	if o.URL == "" {
		o.URL = os.Getenv(URLEnv)
	}
	if o.URL == "" {
		o.URL = DefaultURL
	}
	if o.Before == nil {
		o.Before = func() {}
	}
	return &Catalog{o: o}
}

// Entry is one extension of the repository.
type Entry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
	// Folder is the extension's folder in the repository: what identifies
	// the entry (two folders may claim one id).
	Folder string `json:"folder"`
	// Program: the extension runs a program (built with Go on install);
	// false for a theme-only extension.
	Program     bool     `json:"program"`
	Permissions []string `json:"permissions"`
	Themes      int      `json:"themes"`
	// Error says why the folder can't be installed (a broken manifest).
	Error string `json:"error"`
	// command is the program's path in Folder on this platform.
	command string
}

// Listing is what the repository offers now.
type Listing struct {
	// Source is the repository's URL.
	Source string `json:"source"`
	// Revision is the commit the clone is at ("" before the first fetch).
	Revision string  `json:"revision"`
	Entries  []Entry `json:"entries"`
}

// Fetched reports whether the repository was cloned already.
func (c *Catalog) Fetched() bool {
	_, err := os.Stat(filepath.Join(c.o.Dir, ".git"))
	return err == nil
}

// List reads the clone, cloning it first when there is none yet, or when it
// was cloned from another URL.
func (c *Catalog) List(ctx context.Context) (Listing, error) {
	c.o.Before()
	if !c.Fetched() || c.origin(ctx) != c.o.URL {
		return c.Refresh(ctx)
	}
	c.files.RLock()
	defer c.files.RUnlock()
	return c.read(ctx)
}

// Refresh clones the repository, or brings the clone to the remote's
// default branch, then reads it.
func (c *Catalog) Refresh(ctx context.Context) (Listing, error) {
	c.o.Before()
	c.files.Lock()
	defer c.files.Unlock()
	if err := c.fetch(ctx); err != nil {
		return Listing{Source: c.o.URL, Entries: []Entry{}}, err
	}
	return c.read(ctx)
}

// origin is the URL the clone was made from ("" without a clone).
func (c *Catalog) origin(ctx context.Context) string {
	out, err := c.git(ctx, c.o.Dir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (c *Catalog) fetch(ctx context.Context) error {
	if c.Fetched() && c.origin(ctx) == c.o.URL {
		if _, err := c.git(ctx, c.o.Dir, "fetch", "--depth", "1", "--no-tags", "origin", "HEAD"); err != nil {
			return err
		}
		_, err := c.git(ctx, c.o.Dir, "reset", "--hard", "--quiet", "FETCH_HEAD")
		return err
	}
	parent := filepath.Dir(c.o.Dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".clone-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := c.git(ctx, parent, "clone", "--depth", "1", "--no-tags", "--quiet", "--", c.o.URL, tmp); err != nil {
		return err
	}
	_ = os.RemoveAll(c.o.Dir) // a leftover, or a clone of another URL
	return os.Rename(tmp, c.o.Dir)
}

func (c *Catalog) read(ctx context.Context) (Listing, error) {
	l := Listing{Source: c.o.URL, Entries: []Entry{}}
	if rev, err := c.git(ctx, c.o.Dir, "rev-parse", "--short", "HEAD"); err == nil {
		l.Revision = strings.TrimSpace(rev)
	}
	dirs, err := os.ReadDir(c.o.Dir)
	if err != nil {
		return l, err
	}
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		if e, ok := readEntry(filepath.Join(c.o.Dir, d.Name())); ok {
			l.Entries = append(l.Entries, e)
		}
	}
	slices.SortFunc(l.Entries, func(a, b Entry) int {
		if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return strings.Compare(a.Folder, b.Folder)
	})
	return l, nil
}

// readEntry reads a folder's manifest without requiring its program: a Go
// extension's program exists only once it was built.
func readEntry(dir string) (Entry, bool) {
	data, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, false
	}
	e := Entry{ID: filepath.Base(dir), Name: filepath.Base(dir), Folder: filepath.Base(dir), Permissions: []string{}}
	if err != nil {
		e.Error = err.Error()
		return e, true
	}
	m, err := manifest.Parse(data)
	if err != nil {
		e.Error = err.Error()
		return e, true
	}
	e.ID, e.Name, e.Version, e.Publisher, e.Description = m.ID, m.Name, m.Version, m.Publisher, m.Description
	e.Program = m.HasProgram()
	e.command = m.ProgramPath(runtime.GOOS)
	e.Permissions = slices.Clone(m.Permissions)
	e.Themes = len(m.Contributes.Themes)
	return e, true
}

// Prepare makes the repository's folder ready to install and returns it: a
// Go extension (go.mod in its folder) is built to the program its manifest
// names; any other is used as it is. The folder stays as it is until release
// is called (after the install), which the caller must do when err is nil.
func (c *Catalog) Prepare(ctx context.Context, folder string) (dir string, release func(), err error) {
	c.build.Lock()
	defer c.build.Unlock()
	c.files.RLock()
	defer func() {
		if err != nil {
			c.files.RUnlock()
		}
	}()
	l, err := c.read(ctx)
	if err != nil {
		return "", nil, err
	}
	i := slices.IndexFunc(l.Entries, func(e Entry) bool { return e.Folder == folder })
	if i < 0 {
		return "", nil, ErrUnknown
	}
	e := l.Entries[i]
	if e.Error != "" {
		return "", nil, fmt.Errorf("%s: %s", e.Folder, e.Error)
	}
	dir = filepath.Join(c.o.Dir, e.Folder)
	if e.Program {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			if err = c.goBuild(ctx, dir, e.command); err != nil {
				return "", nil, err
			}
		} // else prebuilt, or another language: Install checks the program
	}
	var once sync.Once
	return dir, func() { once.Do(c.files.RUnlock) }, nil
}

func (c *Catalog) goBuild(ctx context.Context, dir, command string) error {
	out, err := buildTarget(dir, command)
	if err != nil {
		return err
	}
	c.o.Before()
	goBin, err := findGo()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-trimpath", "-o="+out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build in %s: %w\n%s", filepath.Base(dir), err, clip(b))
	}
	return nil
}

// buildTarget is where go build writes command: inside dir with every
// symbolic link resolved, and not itself a link, so a link checked into the
// repository can't send the program anywhere else.
func buildTarget(dir, command string) (string, error) {
	bad := fmt.Errorf("run.command %q can't be built", command)
	if command == "" || filepath.IsAbs(command) || slices.Contains(strings.Split(filepath.ToSlash(command), "/"), "..") {
		return "", bad
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	out := filepath.Join(root, filepath.FromSlash(command))
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", fmt.Errorf("run.command %q leads outside the extension's folder", command)
	}
	out = filepath.Join(real, filepath.Base(out))
	if info, err := os.Lstat(out); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("run.command %q is a symbolic link", command)
	}
	return out, nil
}

// findGo is the go command: on PATH, else the usual install locations.
func findGo() (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/usr/local/go/bin/go", "/opt/homebrew/bin/go", "/usr/lib/go/bin/go"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("this extension is built with Go, and the go command was not found: install Go (go.dev/dl) and try again")
}

func (c *Catalog) git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Never wait for a prompt: credentials come from the user's credential
	// helper or SSH agent and keys (their own ssh config is kept), or the
	// command fails.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git was not found: Flint needs git to read the extensions repository")
		}
		return "", fmt.Errorf("git %s: %w\n%s", args[0], err, clip(out))
	}
	return string(out), nil
}

func clip(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > maxOutput {
		s = "…" + s[len(s)-maxOutput:]
	}
	return s
}
