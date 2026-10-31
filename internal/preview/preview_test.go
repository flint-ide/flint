package preview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownRendersGFM(t *testing.T) {
	html, err := Markdown("# Title\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n\n~~gone~~ https://example.com\n\n```go\nx := 1\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h1 id="title">Title</h1>`, "<table>", `type="checkbox"`, "<del>gone</del>", `<a href="https://example.com">`, `class="language-go"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
}

func TestMarkdownDropsRawHTMLAndDangerousLinks(t *testing.T) {
	html, err := Markdown("<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[click](javascript:alert(1)) [file](file:///etc/passwd)\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "onerror", "javascript:", "file:///"} {
		if strings.Contains(html, bad) {
			t.Errorf("unsafe %q kept in\n%s", bad, html)
		}
	}
}

func TestReadImageStaysInsideRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32))
	if err := os.WriteFile(filepath.Join(root, "a.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(root, "link.png")); err != nil {
		t.Fatal(err)
	}

	img, err := ReadImage(filepath.Join(root, "a.png"), []string{root})
	if err != nil || img.Mime != "image/png" || !strings.HasPrefix(img.DataURL, "data:image/png;base64,") {
		t.Fatalf("img %+v err %v", img, err)
	}
	for _, p := range []string{filepath.Join(outside, "secret.png"), filepath.Join(root, "link.png"), filepath.Join(root, "..", filepath.Base(outside), "secret.png")} {
		if _, err := ReadImage(p, []string{root}); !errors.Is(err, ErrOutside) {
			t.Errorf("%s: err = %v, want ErrOutside", p, err)
		}
	}
	if _, err := ReadImage(filepath.Join(root, "a.txt"), []string{root}); err == nil {
		t.Error("non-image accepted")
	}
}

func TestReadImageCapsSize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxImageBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	img, err := ReadImage(path, []string{root})
	if err != nil || !img.TooLarge || img.DataURL != "" {
		t.Fatalf("img %+v err %v", img, err)
	}
}

func TestRemoteImagesWaitForTheUser(t *testing.T) {
	html, err := MarkdownBlockingRemote("![pixel](https://tracker.example/p.gif?id=1) ![local](img/a.png)")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, `<img src="https://`) || !strings.Contains(html, `data-remote-src="https://tracker.example/p.gif?id=1"`) {
		t.Errorf("remote image would load:\n%s", html)
	}
	if !strings.Contains(html, `src="img/a.png"`) {
		t.Errorf("local image lost:\n%s", html)
	}
}
