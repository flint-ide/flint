package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTypecheck(t *testing.T) {
	root := flintRoot(t)
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	dir := t.TempDir()
	entry := filepath.Join(dir, "panel.tsx")
	// A page using the kit, its icons and a CSS file of its own type checks.
	if err := os.WriteFile(entry, []byte("import './panel.css'\n"+page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := typecheck(root, entry); err != nil {
		t.Fatalf("valid page: %v", err)
	}
	// A wrong prop is an error.
	bad := filepath.Join(dir, "bad.tsx")
	if err := os.WriteFile(bad, []byte("import { Button } from '@flint-ide/ui'\nexport const B = () => <Button size=\"huge\">x</Button>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := typecheck(root, entry); err == nil {
		t.Fatal("a page with a type error passed")
	}
}
