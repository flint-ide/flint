package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// page is what an extension webview writes: a few components, two icons, the bridge.
const page = `
import { createRoot } from 'react-dom/client'
import { useState } from 'react'
import { Button, Keys, flint, syncFlintTheme } from '@flint-ide/ui'
import { Play, Square } from '@flint-ide/ui/icons'

function App() {
  const [on, setOn] = useState(false)
  return (
    <Button onClick={() => { setOn(!on); void flint().request('toggle', { on: !on }) }}>
      {on ? <Square /> : <Play />} Run <Keys keys={['⌘', 'R']} />
    </Button>
  )
}
syncFlintTheme()
createRoot(document.getElementById('root')!).render(<App />)
`

func flintRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "frontend", "node_modules", "@wunderui", "react")); err != nil {
		t.Skip("frontend/node_modules not installed")
	}
	return root
}

func TestBuildPage(t *testing.T) {
	root := flintRoot(t)
	dir := t.TempDir()
	entry := filepath.Join(dir, "panel.tsx")
	if err := os.WriteFile(entry, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "ui", "panel.html")
	size, err := buildPage(root, entry, out, "K8s <panel>")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	t.Logf("page: %d KB", size>>10)

	for _, want := range []string{
		"<title>K8s &lt;panel&gt;</title>", // escaped
		`<div id="root"></div>`,
		"--wui-border",             // WunderUI's styles, renamed for Flint
		"--brand-tint",             // the palette mapping
		"fui-diffstat",             // the composites' styles
		"-webkit-user-select:none", // webview.css
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// One file: nothing to fetch (the frame has no network).
	for _, bad := range []string{"<script src", "<link ", "@import", "url(http", "url(/", "import("} {
		if strings.Contains(html, bad) {
			t.Errorf("page references an external resource: %q", bad)
		}
	}
	// One inline script: "<script>" may also appear as text inside it, but only
	// the real end tag can close it (esbuild escapes the rest).
	if strings.Count(html, "</script>") != 1 || strings.Index(html, "<script>") > strings.Index(html, `<div id="root">`)+len(`<div id="root"></div>`) {
		t.Error("want exactly one inline script, after the root element")
	}
	// Unused components and icons are dropped: React alone is ~190 KB; every
	// component and icon would be several MB.
	if strings.Contains(html, "ProseMirror") {
		t.Error("the rich text editor ended up in a page that doesn't use it")
	}
	if size > 800<<10 {
		t.Errorf("page is %d KB: unused parts of the kit were not dropped", size>>10)
	}
}

func TestBuildPageErrors(t *testing.T) {
	root := flintRoot(t)
	dir := t.TempDir()
	if _, err := buildPage(root, filepath.Join(dir, "missing.tsx"), filepath.Join(dir, "x.html"), ""); err == nil {
		t.Error("a missing entry built")
	}
	bad := filepath.Join(dir, "bad.tsx")
	if err := os.WriteFile(bad, []byte("import { Nope } from './nowhere'\nNope()"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPage(root, bad, filepath.Join(dir, "x.html"), ""); err == nil {
		t.Error("an entry with an unresolved import built")
	}
	if _, err := buildPage(dir, bad, filepath.Join(dir, "x.html"), ""); err == nil || !strings.Contains(err.Error(), "npm ci") {
		t.Errorf("a checkout without node_modules: got %v, want a hint to run npm ci", err)
	}
}

func TestAssembleRejectsEarlyClose(t *testing.T) {
	if _, err := assemble("t", []byte(`x="</SCRIPT>"`), nil); err == nil {
		t.Error("a script containing </script was accepted")
	}
	if _, err := assemble("t", []byte("x"), []byte(`a{content:"</style>"}`)); err == nil {
		t.Error("a stylesheet containing </style was accepted")
	}
}

func TestRichTextKeptWhenUsed(t *testing.T) {
	root := flintRoot(t)
	dir := t.TempDir()
	entry := filepath.Join(dir, "notes.tsx")
	src := "import { createRoot } from 'react-dom/client'\nimport { RichTextEditor } from '@flint-ide/ui'\ncreateRoot(document.getElementById('root')!).render(<RichTextEditor />)\n"
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "notes.html")
	if _, err := buildPage(root, entry, out, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ProseMirror") {
		t.Error("a page using RichTextEditor lost the editor")
	}
}
