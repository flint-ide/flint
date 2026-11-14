package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func webviewManifest(contributes string) string {
	return `{"contract":1,"id":"flint.database","name":"Database","version":"0.1.0",
  "run":{"command":"bin/db"},"activation":["onView:flint.database.console"],
  "contributes":` + contributes + `}`
}

const webviewContributes = `{
  "panels":[{"id":"flint.database.tree","title":"Databases","icon":"database","order":50,"view":"webview","webview":{"html":"ui/panel.html"}}],
  "tabViews":[{"id":"flint.database.console","title":"SQL Console","icon":"database","view":"webview","webview":{"html":"ui/console.html"},"restore":true}]
}`

func TestParseWebviews(t *testing.T) {
	m, err := Parse([]byte(webviewManifest(webviewContributes)))
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasWebviews() || len(m.Contributes.Unsupported) != 0 {
		t.Errorf("webviews %v, unsupported %v", m.HasWebviews(), m.Contributes.Unsupported)
	}
	tv, ok := m.TabView("flint.database.console")
	if !ok || !tv.Restore || tv.Webview.HTML != "ui/console.html" {
		t.Errorf("tab view = %+v", tv)
	}
	if w, ok := m.WebviewOf("flint.database.tree"); !ok || w.HTML != "ui/panel.html" {
		t.Errorf("panel webview = %+v", w)
	}
	if _, ok := m.WebviewOf("flint.database.other"); ok {
		t.Error("unknown view has a webview")
	}
}

func TestWebviewRules(t *testing.T) {
	for name, tc := range map[string]struct{ contributes, want string }{
		"no html":       {`{"panels":[{"id":"flint.database.p","title":"P","icon":"database","view":"webview"}]}`, `needs "webview"`},
		"outside":       {`{"panels":[{"id":"flint.database.p","title":"P","icon":"database","view":"webview","webview":{"html":"../x.html"}}]}`, "clean relative path"},
		"absolute":      {`{"panels":[{"id":"flint.database.p","title":"P","icon":"database","view":"webview","webview":{"html":"/x.html"}}]}`, "clean relative path"},
		"not html":      {`{"panels":[{"id":"flint.database.p","title":"P","icon":"database","view":"webview","webview":{"html":"ui/x.js"}}]}`, "not an .html file"},
		"tab not ours":  {`{"tabViews":[{"id":"other.x","title":"X","view":"webview","webview":{"html":"x.html"}}]}`, `must start with "flint.database."`},
		"tab kind":      {`{"tabViews":[{"id":"flint.database.t","title":"X","view":"stack"}]}`, "not supported"},
		"tab no title":  {`{"tabViews":[{"id":"flint.database.t","view":"webview","webview":{"html":"x.html"}}]}`, "title"},
		"bad activator": {`{"panels":[]}`, "activation event"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(webviewManifest(tc.contributes)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReadWebviewStaysInsideTheFolder(t *testing.T) {
	m, err := Parse([]byte(webviewManifest(webviewContributes)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui", "panel.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if html, err := m.ReadWebview(dir, "flint.database.tree"); err != nil || html != "<h1>hi</h1>" {
		t.Errorf("ReadWebview() = %q, %v", html, err)
	}
	// A symbolic link out of the folder is refused.
	outside := filepath.Join(t.TempDir(), "secret.html")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "ui", "console.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadWebview(dir, "flint.database.console"); err == nil {
		t.Error("read a file outside the extension's folder")
	}
	if _, err := m.ReadWebview(dir, "flint.database.nope"); err == nil {
		t.Error("read an undeclared view")
	}
	if err := os.WriteFile(filepath.Join(dir, "ui", "panel.html"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadWebview(dir, "flint.database.tree"); err == nil {
		t.Error("read a file that isn't UTF-8")
	}
}
