//go:build linux

package desktop

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// revealTimeout bounds the D-Bus call: a file manager that has to start first
// can take a moment, but a missing one must not hang the caller.
const revealTimeout = 5 * time.Second

// Reveal shows path selected in the file manager through the freedesktop
// FileManager1 D-Bus interface (Nautilus, Dolphin, Nemo, Thunar and others
// implement it), or opens its folder with xdg-open when none answers.
func Reveal(path string) error {
	if err := showItems(path); err == nil {
		return nil
	}
	return run("xdg-open", filepath.Dir(path))
}

func showItems(path string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)
	defer cancel()
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return conn.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1").
		CallWithContext(ctx, "org.freedesktop.FileManager1.ShowItems", 0, []string{uri}, "").Err
}

// PrefersDark reads the desktop's color scheme (GNOME 42+ and the desktops
// that follow its setting). Unknown means Flint's default, dark.
func PrefersDark() bool {
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err != nil {
		return true
	}
	return parseColorScheme(string(out))
}

// parseColorScheme interprets `gsettings get … color-scheme` ('prefer-dark',
// 'prefer-light' or 'default', which GNOME shows as light).
func parseColorScheme(out string) bool {
	scheme := strings.Trim(strings.TrimSpace(out), "'")
	return scheme != "prefer-light" && scheme != "default"
}
