package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

const (
	entry     = "src/main.tsx"
	distDir   = "dist"
	assetsDir = "assets"
	// WKWebView on the oldest macOS Flint supports.
	browserTarget = "safari16"
)

// entryScript matches the dev entry in index.html, replaced by the built bundle.
var entryScript = regexp.MustCompile(`<script[^>]*src="\./src/main\.tsx"[^>]*></script>`)

// liveReload listens to esbuild's change stream; a full reload is fast enough
// for this app and keeps the dev loop free of HMR machinery.
const liveReload = `<script>new EventSource('/esbuild').addEventListener('change', () => location.reload())</script>`

func bundleOptions(root string, dev bool) api.BuildOptions {
	mode := "production"
	if dev {
		mode = "development"
	}
	opts := api.BuildOptions{
		AbsWorkingDir: root,
		EntryPoints:   []string{entry},
		Outdir:        filepath.Join(root, distDir, assetsDir),
		EntryNames:    "[name]",
		AssetNames:    "[name]-[hash]",
		Bundle:        true,
		Write:         true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformBrowser,
		Target:        api.ESNext,
		Engines:       []api.Engine{{Name: api.EngineSafari, Version: strings.TrimPrefix(browserTarget, "safari")}},
		JSX:           api.JSXAutomatic,
		Define:        map[string]string{"process.env.NODE_ENV": fmt.Sprintf("%q", mode)},
		Loader: map[string]api.Loader{
			".woff": api.LoaderFile, ".woff2": api.LoaderFile, ".ttf": api.LoaderFile,
			".png": api.LoaderFile, ".svg": api.LoaderFile, ".wasm": api.LoaderFile,
		},
		LogLevel: api.LogLevelWarning,
		Color:    api.ColorIfTerminal,
	}
	if dev {
		opts.Sourcemap = api.SourceMapLinked
	} else {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
		opts.LegalComments = api.LegalCommentsNone
	}
	return opts
}

// build writes a production bundle and index.html into dist/.
func build(root string) error {
	start := time.Now()
	if err := cleanDist(root); err != nil {
		return err
	}
	result := api.Build(bundleOptions(root, false))
	if len(result.Errors) > 0 {
		return fmt.Errorf("bundle failed with %d error(s)", len(result.Errors))
	}
	if err := writeIndex(root, false); err != nil {
		return err
	}
	fmt.Printf("webbuild: %s/%s in %s\n", filepath.Base(root), distDir, time.Since(start).Round(time.Millisecond))
	return nil
}

// serve rebuilds on every change and serves dist/ with esbuild's live-reload stream.
func serve(root string, port int) error {
	if err := cleanDist(root); err != nil {
		return err
	}
	if err := writeIndex(root, true); err != nil {
		return err
	}
	ctx, ctxErr := api.Context(bundleOptions(root, true))
	if ctxErr != nil {
		return fmt.Errorf("esbuild context: %v", ctxErr.Errors)
	}
	defer ctx.Dispose()
	if err := ctx.Watch(api.WatchOptions{}); err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	inner, err := freePort()
	if err != nil {
		return err
	}
	res, err := ctx.Serve(api.ServeOptions{Port: inner, Host: "127.0.0.1", Servedir: filepath.Join(root, distDir)})
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	fmt.Printf("webbuild: watching %s, serving http://localhost:%d\n", filepath.Base(root), port)
	return http.ListenAndServe(fmt.Sprintf("localhost:%d", port), devProxy(int(res.Port)))
}

// devHosts are the Host headers the dev server answers. Wails dev proxies the
// webview's requests as wails.localhost, which esbuild's DNS-rebinding guard
// (localhost only, not configurable) would reject.
var devHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "wails.localhost": true}

// devProxy fronts esbuild's server: it keeps the Host check, but for our hosts.
func devProxy(esbuildPort int) http.Handler {
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", esbuildPort)}
	proxy := httputil.NewSingleHostReverseProxy(target) // flushes the SSE reload stream immediately
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !devHosts[host] {
			http.Error(w, fmt.Sprintf("403 - Forbidden: the host %q is not allowed", host), http.StatusForbidden)
			return
		}
		r.Host = "localhost"
		proxy.ServeHTTP(w, r)
	})
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// cleanDist removes previous output but keeps dist/gitkeep, which go:embed needs.
func cleanDist(root string) error {
	dist := filepath.Join(root, distDir)
	if err := os.RemoveAll(filepath.Join(dist, assetsDir)); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dist, "index.html")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(dist, 0o755)
}

func writeIndex(root string, dev bool) error {
	src, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		return err
	}
	if !entryScript.Match(src) {
		return errors.New(`index.html has no <script src="./src/main.tsx"> entry`)
	}
	tags := `<script type="module" src="./assets/main.js"></script>`
	if dev {
		tags += "\n" + liveReload
	}
	html := entryScript.ReplaceAllLiteral(src, []byte(tags))
	html = bytes.Replace(html, []byte("</head>"), []byte(`    <link rel="stylesheet" href="./assets/main.css"/>`+"\n</head>"), 1)
	return os.WriteFile(filepath.Join(root, distDir, "index.html"), html, 0o644)
}
