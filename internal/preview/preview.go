// Package preview renders Markdown to HTML and reads images for the preview tabs.
package preview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// MaxImageBytes caps an image sent to the webview (as a data URL, so ~4/3 of it).
const MaxImageBytes = 25 << 20

// ErrOutside is returned for paths outside every workspace folder.
var ErrOutside = errors.New("outside the open folders")

// md renders GitHub-flavoured Markdown. Raw HTML is not passed through and
// dangerous link schemes (javascript:, vbscript:, file:) are dropped: goldmark's
// renderer does both unless told it's "unsafe", which it never is here.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// Markdown renders src to HTML.
func Markdown(src string) (string, error) {
	var b bytes.Buffer
	b.Grow(len(src) * 3 / 2)
	if err := md.Convert([]byte(src), &b); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return b.String(), nil
}

// remoteImg matches goldmark's <img> tags with a remote source.
var remoteImg = regexp.MustCompile(`<img src="(https?://)`)

// MarkdownBlockingRemote renders src like Markdown but keeps remote images
// from loading: their address moves to data-remote-src, and the preview
// loads them only when asked (a file's tracking pixel would otherwise learn
// who opened it, and when).
func MarkdownBlockingRemote(src string) (string, error) {
	html, err := Markdown(src)
	if err != nil {
		return "", err
	}
	return remoteImg.ReplaceAllString(html, `<img data-remote-src="$1`), nil
}

// Image is an image file ready for an <img>.
type Image struct {
	DataURL string `json:"dataUrl"` // empty when TooLarge
	Mime    string `json:"mime"`
	Size    int64  `json:"size"`
	// TooLarge: over MaxImageBytes, not sent.
	TooLarge bool `json:"tooLarge"`
}

// imageTypes are the extensions the image tab opens, with their MIME types
// (sniffing doesn't know SVG or ICO reliably).
var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".ico": "image/x-icon", ".bmp": "image/bmp",
}

// IsImage reports whether path has an extension the image tab shows.
func IsImage(path string) bool {
	_, ok := imageTypes[strings.ToLower(filepath.Ext(path))]
	return ok
}

// ReadImage reads an image inside one of roots (symlinks resolved, so a link
// can't point the preview outside the open folders).
func ReadImage(path string, roots []string) (Image, error) {
	mime, ok := imageTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return Image{}, fmt.Errorf("not an image: %s", filepath.Base(path))
	}
	real, err := inside(path, roots)
	if err != nil {
		return Image{}, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return Image{}, err
	}
	img := Image{Mime: mime, Size: info.Size()}
	if info.Size() > MaxImageBytes {
		img.TooLarge = true
		return img, nil
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return Image{}, err
	}
	if mime != "image/svg+xml" && mime != "image/x-icon" {
		// Trust the content over the extension for raster formats.
		if sniffed := http.DetectContentType(data); strings.HasPrefix(sniffed, "image/") {
			img.Mime = sniffed
		}
	}
	img.DataURL = "data:" + img.Mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	return img, nil
}

// inside resolves path and checks it lies in one of roots.
func inside(path string, roots []string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q is not absolute", path)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if real == r || strings.HasPrefix(real, r+string(filepath.Separator)) {
			return real, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrOutside, path)
}
