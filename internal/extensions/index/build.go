package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"flint/internal/extensions/manifest"
)

// Input is one built extension for Build: a folder (packed by Build) or a
// .tar.gz made by Pack, for one platform.
type Input struct {
	// Platform is one of Platforms, Any, or "" (Any; an extension with a
	// program must name its platform).
	Platform string
	Path     string
}

// ParseInput reads a command-line argument: "<platform>=<path>" or "<path>".
func ParseInput(arg string) Input {
	if platform, p, ok := strings.Cut(arg, "="); ok && (platform == Any || slices.Contains(Platforms, platform)) {
		return Input{Platform: platform, Path: p}
	}
	return Input{Path: arg}
}

// BuildOptions configure Build.
type BuildOptions struct {
	Inputs []Input
	// OutDir receives the archives and index.json (created if missing).
	OutDir string
	// BaseURL is where the files of OutDir will be served: an archive's URL
	// is BaseURL + its file name.
	BaseURL string
	// Now is the index's generatedAt; zero uses the current time.
	Now time.Time
}

// ArchiveName is the file name Build gives an archive.
func ArchiveName(id, version, platform string) string {
	return id + "-" + version + "-" + platform + ".tar.gz"
}

// Build packs the inputs into o.OutDir and writes index.json there (not
// signed: see Sign). Every input's manifest is checked with Flint's rules,
// a program must exist and be executable for its platform, and the inputs
// of one extension must agree on everything the index shows.
func Build(o BuildOptions) (*Index, error) {
	if len(o.Inputs) == 0 {
		return nil, errors.New("nothing to build: name at least one extension folder or archive")
	}
	base := o.BaseURL
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if err := CheckURL(base); err != nil {
		return nil, fmt.Errorf("base URL: %w", err)
	}
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return nil, err
	}
	entries := map[string]*Entry{}
	for _, in := range o.Inputs {
		e, platform, a, err := buildInput(in, o.OutDir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", in.Path, err)
		}
		a.URL = base + ArchiveName(e.ID, e.Version, platform)
		have := entries[e.ID]
		if have == nil {
			entries[e.ID] = &e
			have = &e
		} else if !sameSummary(*have, e) {
			return nil, fmt.Errorf("%s: %s differs from the other builds of this extension (version, permissions, tools and description must be the same on every platform)", in.Path, e.ID)
		}
		if _, dup := have.Archives[platform]; dup {
			return nil, fmt.Errorf("%s: a second %s archive of %s", in.Path, platform, e.ID)
		}
		have.Archives[platform] = a
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	ix := &Index{Format: Format, GeneratedAt: now.UTC().Truncate(time.Second), Extensions: []Entry{}}
	for _, id := range slices.Sorted(maps.Keys(entries)) {
		ix.Extensions = append(ix.Extensions, *entries[id])
	}
	data, err := Marshal(ix)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(data); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(o.OutDir, FileName), data, 0o644); err != nil {
		return nil, err
	}
	return ix, nil
}

// Marshal encodes an index the way Build writes it.
func Marshal(ix *Index) ([]byte, error) {
	data, err := json.MarshalIndent(ix, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func sameSummary(a, b Entry) bool {
	a.Archives, b.Archives = nil, nil
	return reflect.DeepEqual(a, b)
}

// buildInput puts one input's archive into outDir.
func buildInput(in Input, outDir string) (Entry, string, Archive, error) {
	fail := func(err error) (Entry, string, Archive, error) { return Entry{}, "", Archive{}, err }
	info, err := os.Stat(in.Path)
	if err != nil {
		return fail(err)
	}
	dir := in.Path
	if !info.IsDir() {
		// An archive: unpack it (with Flint's own limits) to read what it holds.
		tmp, err := os.MkdirTemp("", "flint-extindex-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(tmp)
		if err := Extract(in.Path, tmp); err != nil {
			return fail(err)
		}
		dir = tmp
	}
	data, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return fail(fmt.Errorf("no %s: %w", manifest.FileName, err))
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return fail(err)
	}
	platform, err := checkPlatform(dir, m, in.Platform)
	if err != nil {
		return fail(err)
	}
	tmp, err := os.CreateTemp(outDir, ".archive-*")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	w := &countWriter{w: io.MultiWriter(tmp, h)}
	if info.IsDir() {
		err = Pack(dir, w)
	} else {
		var f *os.File
		if f, err = os.Open(in.Path); err == nil {
			_, err = io.Copy(w, f)
			f.Close()
		}
	}
	if err == nil {
		err = tmp.Close()
	}
	if err != nil {
		return fail(err)
	}
	if w.n > MaxArchiveBytes {
		return fail(fmt.Errorf("the archive is larger than %d MiB", MaxArchiveBytes>>20))
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(outDir, ArchiveName(m.ID, m.Version, platform))); err != nil {
		return fail(err)
	}
	return EntryOf(m), platform, Archive{SHA256: hex.EncodeToString(h.Sum(nil)), Size: w.n}, nil
}

// checkPlatform settles an input's platform and checks that its program is
// there for it.
func checkPlatform(dir string, m *manifest.Manifest, platform string) (string, error) {
	if !m.HasProgram() {
		if platform != "" && platform != Any {
			return "", fmt.Errorf("%s has no program, so it is packed once, as %q, not for %s", m.ID, Any, platform)
		}
		return Any, nil
	}
	if !slices.Contains(Platforms, platform) {
		return "", fmt.Errorf("%s runs a program: name the platform it was built for, as <platform>=<path> (%s)", m.ID, strings.Join(Platforms, ", "))
	}
	goos, _, _ := strings.Cut(platform, "-")
	command := m.ProgramPath(goos)
	if command == "" || !filepath.IsLocal(filepath.FromSlash(command)) {
		return "", fmt.Errorf("run.command %q is not a path inside the folder", command)
	}
	info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(command)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("the program %s is missing: build it for %s first", command, platform)
		}
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("the program %s is not an executable file", command)
	}
	return platform, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// CheckFiles compares the archives the index names with the files of dir
// (by the last element of each URL): each must exist with the index's size
// and SHA-256.
func CheckFiles(ix *Index, dir string) error {
	for _, e := range ix.Extensions {
		for platform, a := range e.Archives {
			name := path.Base(a.URL)
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				return fmt.Errorf("%s (%s): %w", e.ID, platform, err)
			}
			h := sha256.New()
			n, err := io.Copy(h, f)
			f.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
				return fmt.Errorf("%s is not the file the index describes (size or SHA-256 differ)", name)
			}
		}
	}
	return nil
}
