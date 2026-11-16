package index

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Limits on an archive being unpacked: the same as on a folder being
// installed (internal/extensions/host/install.go).
const (
	MaxExtractFiles = 10000
	MaxExtractBytes = 512 << 20
	MaxExtractDepth = 32
)

// Extract unpacks the .tar.gz at src into the existing, empty folder dst.
// The archive's root is the extension's folder (flint-extension.json at the
// top). Only folders and regular files are accepted: an absolute path, a
// ".." element, a symbolic or hard link, a device, a FIFO, a name used
// twice, more than MaxExtractFiles entries, folders deeper than
// MaxExtractDepth or more than MaxExtractBytes of content make the whole
// archive fail. Files are written through an os.Root, so nothing can land
// outside dst whatever a name looks like. Executable bits are kept; other
// mode bits, owners and times are not.
func Extract(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	return ExtractFrom(f, dst)
}

// ExtractFrom is Extract for a stream.
func ExtractFrom(r io.Reader, dst string) error {
	return extract(r, dst, MaxExtractBytes)
}

// extract is ExtractFrom with the byte limit as a parameter (tests).
func extract(r io.Reader, dst string, maxBytes int64) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("the archive is not a .tar.gz file: %w", err)
	}
	defer zr.Close()
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(zr)
	entries, size := 0, int64(0)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the archive is damaged: %w", err)
		}
		name, err := entryName(h.Name)
		if err != nil {
			return err
		}
		if name == "." {
			if h.Typeflag != tar.TypeDir {
				return fmt.Errorf("the archive entry %q is not a folder", clip(h.Name))
			}
			continue
		}
		if entries++; entries > MaxExtractFiles {
			return fmt.Errorf("the archive holds more than %d files and folders", MaxExtractFiles)
		}
		if strings.Count(name, "/") >= MaxExtractDepth {
			return fmt.Errorf("the archive nests folders deeper than %d", MaxExtractDepth)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 || h.Size > maxBytes-size {
				return fmt.Errorf("the archive unpacks to more than %d MiB", maxBytes>>20)
			}
			if dir := path.Dir(name); dir != "." {
				if err := root.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			n, err := extractFile(root, name, tr, h, maxBytes-size)
			size += n
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("the archive entry %q is a %s: only files and folders are allowed", clip(h.Name), typeName(h.Typeflag))
		}
	}
}

// entryName checks a name from the archive and returns it cleaned,
// slash-separated and relative.
func entryName(raw string) (string, error) {
	bad := fmt.Errorf("the archive entry %q has a path Flint refuses (absolute, with \"..\", or with unusual characters)", clip(raw))
	if raw == "" || strings.ContainsAny(raw, "\\\x00") || strings.HasPrefix(raw, "/") {
		return "", bad
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", bad
		}
	}
	name := path.Clean(raw)
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", bad
	}
	return name, nil
}

func extractFile(root *os.Root, name string, r io.Reader, h *tar.Header, budget int64) (int64, error) {
	const tooLarge = "the archive unpacks to more than Flint allows"
	mode := fs.FileMode(0o644)
	if h.Mode&0o111 != 0 {
		mode = 0o755
	}
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, fmt.Errorf("the archive holds %q twice", clip(name))
		}
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(r, budget+1))
	if err == nil && n > budget {
		err = errors.New(tooLarge)
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, fmt.Errorf("unpack %s: %w", clip(name), err)
	}
	return n, nil
}

func typeName(flag byte) string {
	switch flag {
	case tar.TypeSymlink:
		return "symbolic link"
	case tar.TypeLink:
		return "hard link"
	case tar.TypeChar, tar.TypeBlock:
		return "device"
	case tar.TypeFifo:
		return "FIFO"
	}
	return fmt.Sprintf("special entry (type %q)", flag)
}

// Pack writes the folder dir as a .tar.gz to w, the way Extract reads it:
// the folder's content at the archive's root, folders and regular files
// only (a symbolic link or special file is an error), .git left out. The
// output depends only on names, content and executable bits: entries are
// sorted, and times and owners are zero, so the same folder always packs to
// the same bytes.
func Pack(dir string, w io.Writer) error {
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	entries, size := 0, int64(0)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if entries++; entries > MaxExtractFiles {
			return fmt.Errorf("more than %d files and folders", MaxExtractFiles)
		}
		if strings.Count(name, "/") >= MaxExtractDepth {
			return fmt.Errorf("folders nested deeper than %d", MaxExtractDepth)
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				entries--
				return fs.SkipDir
			}
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o755, ModTime: time.Unix(0, 0), Format: tar.FormatPAX})
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file (links and special files can't be packed)", name)
		}
		if size += info.Size(); size > MaxExtractBytes {
			return fmt.Errorf("more than %d MiB", MaxExtractBytes>>20)
		}
		mode := int64(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode, Size: info.Size(), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		// CopyN: a file that grew or shrank since Stat fails here.
		_, err = io.CopyN(tw, f, info.Size())
		return err
	})
	if err != nil {
		return fmt.Errorf("pack %s: %w", dir, err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}
