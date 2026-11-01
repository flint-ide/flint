package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const indexHTML = `<!DOCTYPE html>
<html><head><title>t</title></head>
<body><div id="root"></div>
<script src="./src/main.tsx" type="module"></script>
</body></html>`

func newFrontend(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBuildBundlesTSXAndCSS(t *testing.T) {
	root := newFrontend(t, map[string]string{
		"index.html":    indexHTML,
		"src/main.tsx":  "import './style.css'\nimport { label } from './label'\ndocument.title = label(<b />)\n",
		"src/label.ts":  "export const label = (x: unknown): string => String(x)\n",
		"src/style.css": "body { color: red }\n",
		// The automatic JSX runtime import must resolve.
		"node_modules/react/jsx-runtime.js": "export const jsx = () => null\n",
		"node_modules/react/package.json":   `{"name":"react","exports":{"./jsx-runtime":"./jsx-runtime.js"}}`,
	})
	if err := build(root); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"assets/main.js", "assets/main.css", "index.html"} {
		if _, err := os.Stat(filepath.Join(root, distDir, f)); err != nil {
			t.Errorf("missing dist/%s: %v", f, err)
		}
	}
	html, _ := os.ReadFile(filepath.Join(root, distDir, "index.html"))
	for _, want := range []string{`href="./assets/main.css"`, `src="./assets/main.js"`} {
		if !strings.Contains(string(html), want) {
			t.Errorf("index.html lacks %s:\n%s", want, html)
		}
	}
	if strings.Contains(string(html), "main.tsx") || strings.Contains(string(html), "EventSource") {
		t.Errorf("production index.html keeps dev bits:\n%s", html)
	}
}

// The boot script runs before the first paint, so it is a classic script of its
// own in <head>, not part of the module bundle.
func TestBuildBundlesTheBootScript(t *testing.T) {
	root := newFrontend(t, map[string]string{
		"index.html": `<!DOCTYPE html>
<html><head><title>t</title><script src="./src/boot.ts"></script></head>
<body><div id="root"></div>
<script src="./src/main.tsx" type="module"></script>
</body></html>`,
		"src/main.tsx": "document.title = 'x'\n",
		"src/boot.ts":  "import { theme } from './theme'\ndocument.documentElement.dataset.theme = theme()\n",
		"src/theme.ts": "export const theme = (): string => 'dark'\n",
	})
	if err := build(root); err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(root, distDir, "assets/boot.js"))
	if err != nil {
		t.Fatalf("missing dist/assets/boot.js: %v", err)
	}
	if strings.Contains(string(js), "import") || strings.Contains(string(js), "export") {
		t.Errorf("boot.js must be a classic script:\n%s", js)
	}
	html, _ := os.ReadFile(filepath.Join(root, distDir, "index.html"))
	if !strings.Contains(string(html), `<script src="./assets/boot.js"></script>`) || strings.Contains(string(html), "boot.ts") {
		t.Errorf("index.html doesn't load the built boot script:\n%s", html)
	}
}

func TestBuildReportsErrors(t *testing.T) {
	root := newFrontend(t, map[string]string{
		"index.html":   indexHTML,
		"src/main.tsx": "import './nope'\n", // unused TS imports are elided, side effects are not
	})
	if err := build(root); err == nil {
		t.Fatal("expected an error for an unresolved import")
	}
}

func TestDevIndexAddsLiveReload(t *testing.T) {
	root := newFrontend(t, map[string]string{"index.html": indexHTML})
	if err := cleanDist(root); err != nil {
		t.Fatal(err)
	}
	if err := writeIndex(root, true); err != nil {
		t.Fatal(err)
	}
	html, _ := os.ReadFile(filepath.Join(root, distDir, "index.html"))
	if !strings.Contains(string(html), "/esbuild") {
		t.Fatalf("no live reload:\n%s", html)
	}
}

func TestWriteIndexNeedsEntry(t *testing.T) {
	root := newFrontend(t, map[string]string{"index.html": "<html></html>"})
	_ = cleanDist(root)
	if err := writeIndex(root, false); err == nil {
		t.Fatal("expected an error without the entry script")
	}
}

func TestCleanDistKeepsGitkeep(t *testing.T) {
	root := newFrontend(t, map[string]string{
		"dist/gitkeep":       "",
		"dist/assets/old.js": "",
		"dist/index.html":    "",
	})
	if err := cleanDist(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist/gitkeep")); err != nil {
		t.Error("gitkeep removed")
	}
	if _, err := os.Stat(filepath.Join(root, "dist/assets")); err == nil {
		t.Error("old assets kept")
	}
}

func TestFindTestsFilters(t *testing.T) {
	root := newFrontend(t, map[string]string{
		"src/a.test.ts":    "",
		"src/b/c.test.tsx": "",
		"src/b/c.ts":       "",
		"src/d.spec.ts":    "",
	})
	all, err := findTests(filepath.Join(root, "src"), nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %v, %v", all, err)
	}
	some, _ := findTests(filepath.Join(root, "src"), []string{"/b/"})
	if len(some) != 1 || !strings.HasSuffix(some[0], "c.test.tsx") {
		t.Fatalf("filtered = %v", some)
	}
}

func TestFrontendDir(t *testing.T) {
	root := newFrontend(t, map[string]string{"frontend/index.html": indexHTML})
	t.Chdir(root)
	got, err := frontendDir("")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "frontend" {
		t.Fatalf("got %s", got)
	}
	if _, err := frontendDir("nowhere"); err == nil {
		t.Fatal("expected an error for a missing dir")
	}
}

func TestDevProxyAllowsWailsHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Host) // esbuild only accepts "localhost"
	}))
	defer upstream.Close()
	proxy := httptest.NewServer(devProxy(upstream.Listener.Addr().(*net.TCPAddr).Port))
	defer proxy.Close()

	for host, want := range map[string]int{"wails.localhost": 200, "localhost:5173": 200, "evil.example": 403} {
		req, _ := http.NewRequest("GET", proxy.URL+"/index.html", nil)
		req.Host = host
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("Host %s: status %d, want %d", host, res.StatusCode, want)
		}
		if want == 200 && string(body) != "localhost" {
			t.Errorf("Host %s reached esbuild as %q", host, body)
		}
	}
}
