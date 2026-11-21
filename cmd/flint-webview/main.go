// Command flint-webview builds an extension's webview page from TypeScript/React
// into the one self-contained HTML file the extension contract requires
// (site/extensions/protocol.md ▸ Webviews): every script and style inline, fonts
// and images as data: URLs. The page may import @flint-ide/ui (ui/: WunderUI on
// Flint's palette, Flint's composites, '@flint-ide/ui/icons'); it is bundled from
// this checkout's source and frontend/node_modules, so the extension needs no
// npm, and the page carries only the components and icons it uses.
//
//	go run ./cmd/flint-webview -o path/to/ext/ui/panel.html [-title Kubernetes] path/to/ext/webview/panel.tsx
//
// Run it from a Flint checkout after `npm ci --ignore-scripts` in frontend/.
// The base webview styles (ui/css/webview.css) and the kit's styles are always
// included; the entry renders into <div id="root">. WunderUI's rich text editor
// (Tiptap, ~400 KB) is included only when a file next to the entry mentions
// RichTextEditor.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// maxPage is the contract's limit for a webview file.
const maxPage = 32 << 20

// safariTarget matches cmd/webbuild: WKWebView on the oldest macOS Flint supports.
const safariTarget = "16"

func main() {
	out := flag.String("o", "", "output HTML file (required)")
	title := flag.String("title", "", "page <title> (default: the entry's file name)")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: flint-webview -o page.html [-title T] entry.tsx")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *out == "" || flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	size, err := buildPage(".", flag.Arg(0), *out, *title)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flint-webview:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "flint-webview: %s (%d KB)\n", *out, size>>10)
}

// buildPage bundles entry against the Flint checkout at flintRoot and writes the
// page to out. It returns the page's size.
func buildPage(flintRoot, entry, out, title string) (int, error) {
	root, err := filepath.Abs(flintRoot)
	if err != nil {
		return 0, err
	}
	modules := filepath.Join(root, "frontend", "node_modules")
	if _, err := os.Stat(filepath.Join(modules, "@wunderui", "react")); err != nil {
		return 0, fmt.Errorf("%s has no @wunderui/react: run `npm ci --ignore-scripts` in frontend/ first", modules)
	}
	entry, err = filepath.Abs(entry)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(entry); err != nil {
		return 0, err
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(entry), filepath.Ext(entry))
	}
	richText, err := usesRichText(filepath.Dir(entry))
	if err != nil {
		return 0, err
	}
	js, css, err := bundle(root, modules, entry, richText)
	if err != nil {
		return 0, err
	}
	page, err := assemble(title, js, css)
	if err != nil {
		return 0, err
	}
	if len(page) > maxPage {
		return 0, fmt.Errorf("page is %d MiB, over the contract's 32 MiB", len(page)>>20)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return 0, err
	}
	return len(page), os.WriteFile(out, page, 0o644)
}

// bundle compiles the entry (with the kit's styles first) into one script and one stylesheet.
func bundle(root, modules, entry string, richText bool) (js, css []byte, err error) {
	ui := filepath.Join(root, "ui")
	stdin := fmt.Sprintf("import '@flint-ide/ui/webview.css'\nimport '@flint-ide/ui/styles.css'\nimport %q\n", entry)
	result := api.Build(api.BuildOptions{
		Stdin:     &api.StdinOptions{Contents: stdin, ResolveDir: filepath.Dir(entry), Loader: api.LoaderTS, Sourcefile: "flint-webview-entry.ts"},
		Outdir:    "out", // not written: Write is false
		Bundle:    true,
		Write:     false,
		Format:    api.FormatIIFE,
		Platform:  api.PlatformBrowser,
		Target:    api.ESNext,
		Engines:   []api.Engine{{Name: api.EngineSafari, Version: safariTarget}},
		JSX:       api.JSXAutomatic,
		NodePaths: []string{modules},
		Define:    map[string]string{"process.env.NODE_ENV": `"production"`},
		Alias: map[string]string{
			"@flint-ide/ui/icons":       filepath.Join(ui, "src", "icons.ts"),
			"@flint-ide/ui/styles.css":  filepath.Join(ui, "css", "index.css"),
			"@flint-ide/ui/webview.css": filepath.Join(ui, "css", "webview.css"),
			"@flint-ide/ui":             filepath.Join(ui, "src", "index.ts"),
		},
		// The frame loads nothing, so assets travel inside the page.
		Loader: map[string]api.Loader{
			".woff": api.LoaderDataURL, ".woff2": api.LoaderDataURL, ".ttf": api.LoaderDataURL,
			".png": api.LoaderDataURL, ".jpg": api.LoaderDataURL, ".gif": api.LoaderDataURL,
			".svg": api.LoaderDataURL, ".webp": api.LoaderDataURL,
		},
		Plugins:           []api.Plugin{richTextStub(richText)},
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		LegalComments:     api.LegalCommentsEndOfFile,
		LogLevel:          api.LogLevelWarning,
		Color:             api.ColorIfTerminal,
	})
	if len(result.Errors) > 0 {
		return nil, nil, fmt.Errorf("bundling %s failed with %d error(s)", entry, len(result.Errors))
	}
	for _, f := range result.OutputFiles {
		switch filepath.Ext(f.Path) {
		case ".js":
			js = f.Contents
		case ".css":
			css = f.Contents
		}
	}
	if js == nil {
		return nil, nil, errors.New("no script was produced")
	}
	return js, css, nil
}

// richTextChunk is the module WunderUI's RichTextEditor loads lazily: the Tiptap
// and ProseMirror editor, about 400 KB. A one-file page can't load it lazily, so
// it would be inlined into every page, used or not.
var richTextChunk = regexp.MustCompile(`^\./rich-text-editor-inner-[\w-]+\.js$`)

// richTextStub replaces that module with an empty component unless the page uses
// the editor.
func richTextStub(keep bool) api.Plugin {
	return api.Plugin{Name: "rich-text-stub", Setup: func(b api.PluginBuild) {
		if keep {
			return
		}
		b.OnResolve(api.OnResolveOptions{Filter: richTextChunk.String()}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if !strings.Contains(filepath.ToSlash(args.Importer), "/@wunderui/react/") {
				return api.OnResolveResult{}, nil
			}
			return api.OnResolveResult{Path: "rich-text-editor", Namespace: "flint-webview-stub"}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "flint-webview-stub"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
			contents := "export default function RichTextEditorNotBundled() { return null }\n"
			return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
		})
	}}
}

// usesRichText reports whether any TypeScript file next to the entry (or below)
// mentions RichTextEditor, the only way a page reaches the lazy editor module.
func usesRichText(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found = bytes.Contains(data, []byte("RichTextEditor"))
		return nil
	})
	return found, err
}

// assemble writes the page. Flint puts its content security policy first in
// <head>; inline scripts and styles are what it allows.
func assemble(title string, js, css []byte) ([]byte, error) {
	// esbuild escapes "</script" and "</style" inside strings; anything left
	// would end the element early.
	if strings.Contains(strings.ToLower(string(js)), "</script") {
		return nil, errors.New("the script contains </script")
	}
	if strings.Contains(strings.ToLower(string(css)), "</style") {
		return nil, errors.New("the stylesheet contains </style")
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n<!-- Generated by flint-webview from the Flint repository. Do not edit; rebuild from the source. -->\n")
	b.WriteString(`<html lang="en"><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title><style>")
	b.Write(css)
	b.WriteString("</style></head><body><div id=\"root\"></div><script>")
	b.Write(js)
	b.WriteString("</script></body></html>\n")
	return []byte(b.String()), nil
}
