package vscodeimport

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"flint/internal/settings"
)

// themeExtension writes an extension contributing one dark theme named label
// whose editor background is bg.
func themeExtension(t *testing.T, dir, label, bg string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "package.json"),
		`{"contributes":{"themes":[{"label":"`+label+`","uiTheme":"vs-dark","path":"./theme.json"}]}}`)
	writeFile(t, filepath.Join(dir, "theme.json"), `{"colors":{"editor.background":"`+bg+`"}}`)
}

func themeBackground(t *testing.T, extDir, label string) string {
	t.Helper()
	ref, err := findTheme([]string{extDir}, label)
	if err != nil {
		t.Fatalf("findTheme() error = %v", err)
	}
	th, _, err := loadTheme(ref.path, ref.root, 0)
	if err != nil {
		t.Fatalf("loadTheme() error = %v", err)
	}
	return convertTheme(th, label, ref.base).Colors["--bg-editor"]
}

func TestNewestExtensionVersionWins(t *testing.T) {
	ext := t.TempDir()
	themeExtension(t, filepath.Join(ext, "acme.night-1.9.0"), "Night", "#000009")
	themeExtension(t, filepath.Join(ext, "acme.night-1.10.0"), "Night", "#000010")
	themeExtension(t, filepath.Join(ext, "acme.night-1.2.0-darwin-arm64"), "Night", "#000002")
	if got := themeBackground(t, ext, "Night"); got != "#000010" {
		t.Errorf("background = %s, want 1.10.0's", got)
	}
}

func TestObsoleteExtensionsAreSkipped(t *testing.T) {
	ext := t.TempDir()
	themeExtension(t, filepath.Join(ext, "acme.night-1.9.0"), "Night", "#000009")
	themeExtension(t, filepath.Join(ext, "acme.night-1.10.0"), "Night", "#000010")
	writeFile(t, filepath.Join(ext, ".obsolete"), `{"acme.night-1.10.0":true}`)
	if got := themeBackground(t, ext, "Night"); got != "#000009" {
		t.Errorf("background = %s, want 1.9.0's (1.10.0 is obsolete)", got)
	}
	writeFile(t, filepath.Join(ext, ".obsolete"), `{"acme.night-1.10.0":true,"acme.night-1.9.0":true}`)
	if _, err := findTheme([]string{ext}, "Night"); err == nil {
		t.Error("found a theme of an uninstalled extension")
	}
	if !hasExtension(filepath.Join("testdata", "home", ".vscode", "extensions"), "vscodevim.vim") {
		t.Error("vscodevim.vim not found")
	}
	writeFile(t, filepath.Join(ext, "vscodevim.vim-1.27.0", "package.json"), `{}`)
	writeFile(t, filepath.Join(ext, ".obsolete"), `{"vscodevim.vim-1.27.0":true}`)
	if hasExtension(ext, "vscodevim.vim") {
		t.Error("an obsolete Vim extension counts as installed")
	}
}

func TestParseExtension(t *testing.T) {
	for name, want := range map[string]string{
		"zhuangtongfa.material-theme-3.17.0":     "zhuangtongfa.material-theme 3.17.0",
		"ms-python.python-2024.1.0-darwin-arm64": "ms-python.python 2024.1.0",
		"VSCodeVim.vim-1.27.0":                   "vscodevim.vim 1.27.0",
		"local.dev-ext":                          "local.dev-ext ",
	} {
		e := parseExtension(name, "")
		var parts []string
		for _, n := range e.version {
			parts = append(parts, strconv.Itoa(n))
		}
		if got := e.id + " " + strings.Join(parts, "."); got != want {
			t.Errorf("parseExtension(%s) = %q, want %q", name, got, want)
		}
	}
	if compareVersions([]int{1, 10}, []int{1, 9, 9}) <= 0 || compareVersions([]int{1, 0}, []int{1}) != 0 {
		t.Error("compareVersions orders wrongly")
	}
}

func TestSymlinkedExtensionFolder(t *testing.T) {
	ext, elsewhere := t.TempDir(), t.TempDir()
	themeExtension(t, filepath.Join(elsewhere, "dev-theme"), "Linked", "#123456")
	if err := os.Symlink(filepath.Join(elsewhere, "dev-theme"), filepath.Join(ext, "acme.linked-0.0.1")); err != nil {
		t.Fatal(err)
	}
	if got := themeBackground(t, ext, "Linked"); got != "#123456" {
		t.Errorf("background = %s, want the linked extension's", got)
	}
}

func TestThemeSymlinkLeavingTheExtensionIsRefused(t *testing.T) {
	ext, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(ext, "acme.sneaky-1.0.0")
	writeFile(t, filepath.Join(dir, "package.json"), `{"contributes":{"themes":[{"label":"Sneaky","path":"./theme.json"}]}}`)
	writeFile(t, filepath.Join(outside, "secret.json"), `{"colors":{"editor.background":"#666666"}}`)
	if err := os.Symlink(filepath.Join(outside, "secret.json"), filepath.Join(dir, "theme.json")); err != nil {
		t.Fatal(err)
	}
	ref, err := findTheme([]string{ext}, "Sneaky")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadTheme(ref.path, ref.root, 0); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("loadTheme(symlink out of the extension) error = %v", err)
	}
}

func TestFIFOIsNotOpened(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "theme.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := loadTheme(fifo, dir, 0)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("loadTheme(FIFO) error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loadTheme blocked on a FIFO")
	}
}

func TestLongThemeNamesFitFlint(t *testing.T) {
	long := strings.Repeat("夜", 60) // 180 bytes
	got := themeName(long + "\n")
	if len(got) > settings.MaxThemeNameLen || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Errorf("themeName() = %q (%d bytes)", got, len(got))
	}
	if themeName("Short") != "Short" || themeName("a\tb") != "ab" {
		t.Error("short names must stay as they are, minus control characters")
	}

	// End to end: such a theme imports and validates.
	home := t.TempDir()
	user := filepath.Join(home, "Library", "Application Support", "Code", "User")
	writeFile(t, filepath.Join(user, "settings.json"), `{"workbench.colorTheme":"`+long+`"}`)
	themeExtension(t, filepath.Join(home, ".vscode", "extensions", "acme.cjk-1.0.0"), long, "#101010")
	src, ok := FindSource(home, "vscode")
	if !ok {
		t.Fatal("source not found")
	}
	s := BuildPlan(src, settings.Default()).Apply(settings.Default())
	if s.Theme != settings.ThemeCustom {
		t.Fatalf("theme = %q, want custom", s.Theme)
	}
	if err := settings.Validate(s); err != nil {
		t.Errorf("imported settings fail validation: %v", err)
	}
}
