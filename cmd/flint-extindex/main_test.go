package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"flint/internal/extensions/index"
)

const themeManifest = `{"contract":1,"id":"me.colors","name":"Colors","version":"1.2.0",
 "contributes":{"themes":[{"id":"me.colors.dark","label":"Dark","base":"dark","colors":{"--bg":"#101010"}}]}}`

const programManifest = `{"contract":1,"id":"me.tool","name":"Tool","version":"0.3.1","run":{"command":"tool"},
 "activation":["onCommand:me.tool.go"],"contributes":{"commands":[{"id":"me.tool.go","title":"Go"}]}}`

func write(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// cmd runs the command with env as its environment.
func cmd(env map[string]string, args ...string) (string, error) {
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut, func(k string) string { return env[k] })
	return out.String() + errOut.String(), err
}

func TestKeygenBuildSignVerify(t *testing.T) {
	out, err := cmd(nil, "keygen")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`public key: (\S+)\nsecret key: (\S+)\n`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("keygen printed:\n%s", out)
	}
	public, secret := m[1], m[2]
	if _, err := index.ParsePublicKey(public); err != nil {
		t.Fatal(err)
	}
	if again, _ := cmd(nil, "keygen"); strings.Contains(again, secret) {
		t.Fatal("keygen printed the same key twice")
	}

	theme, linux, mac := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, theme, "flint-extension.json", themeManifest, 0o644)
	for _, dir := range []string{linux, mac} {
		write(t, dir, "flint-extension.json", programManifest, 0o644)
		write(t, dir, "tool", "program for "+dir, 0o755)
	}
	dist := filepath.Join(t.TempDir(), "dist")
	out, err = cmd(nil, "build", "-out", dist, "-base-url", "https://example.com/releases/download/v1", theme, "linux-amd64="+linux, "darwin-arm64="+mac)
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, want := range []string{"me.colors 1.2.0 (themes): any", "me.tool 0.3.1 (program): darwin-arm64, linux-amd64", "not signed yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("build output lacks %q:\n%s", want, out)
		}
	}
	indexPath := filepath.Join(dist, "index.json")
	for _, name := range []string{"index.json", "me.colors-1.2.0-any.tar.gz", "me.tool-0.3.1-linux-amd64.tar.gz", "me.tool-0.3.1-darwin-arm64.tar.gz"} {
		if _, err := os.Stat(filepath.Join(dist, name)); err != nil {
			t.Error(err)
		}
	}
	if data, _ := os.ReadFile(indexPath); !strings.Contains(string(data), `"url": "https://example.com/releases/download/v1/me.colors-1.2.0-any.tar.gz"`) {
		t.Errorf("index:\n%s", data)
	}

	// Not signed yet: verify fails.
	if _, err := cmd(nil, "verify", "-key", public, indexPath); err == nil || !strings.Contains(err.Error(), "no signature") {
		t.Errorf("verify without a signature: %v", err)
	}

	// Signing needs the key, and says so without publishing anything.
	for name, env := range map[string]map[string]string{"unset": nil, "blank": {DefaultKeyEnv: "  \n"}} {
		_, err := cmd(env, "sign", indexPath)
		if err == nil || !strings.Contains(err.Error(), "no signing key") || !strings.Contains(err.Error(), DefaultKeyEnv) {
			t.Errorf("sign with the key %s: %v", name, err)
		}
	}
	if _, err := cmd(map[string]string{DefaultKeyEnv: public}, "sign", indexPath); err == nil {
		t.Error("signed with a public key")
	}
	if _, err := os.Stat(indexPath + ".sig"); err == nil {
		t.Fatal("a signature was written without a usable key")
	}

	out, err = cmd(map[string]string{DefaultKeyEnv: secret + "\n"}, "sign", indexPath)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if strings.Contains(out, secret) || !strings.Contains(out, public) {
		t.Errorf("sign must print the public key and never the secret one:\n%s", out)
	}
	if out, err = cmd(nil, "verify", "-key", public, "-archives", dist, indexPath); err != nil || !strings.Contains(out, "extensions listed: 2") {
		t.Errorf("verify: %v\n%s", err, out)
	}

	// Another variable, and a key file.
	if _, err := cmd(map[string]string{"MY_KEY": secret}, "sign", "-key-env", "MY_KEY", indexPath); err != nil {
		t.Errorf("sign -key-env: %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "key")
	write(t, filepath.Dir(keyFile), "key", secret+"\n", 0o600)
	if _, err := cmd(nil, "sign", "-key-file", keyFile, indexPath); err != nil {
		t.Errorf("sign -key-file: %v", err)
	}

	// Wrong key, changed index, changed archive.
	otherPublic, _, _ := index.GenerateKey()
	if _, err := cmd(nil, "verify", "-key", otherPublic, indexPath); !errors.Is(err, index.ErrBadSignature) {
		t.Errorf("verify with another key: %v", err)
	}
	write(t, dist, "me.colors-1.2.0-any.tar.gz", "changed", 0o644)
	if _, err := cmd(nil, "verify", "-key", public, "-archives", dist, indexPath); err == nil || !strings.Contains(err.Error(), "not the file the index describes") {
		t.Errorf("verify with a changed archive: %v", err)
	}
	if _, err := cmd(nil, "verify", "-key", public, indexPath); err != nil {
		t.Errorf("verify without -archives: %v", err)
	}
	data, _ := os.ReadFile(indexPath)
	write(t, dist, "index.json", strings.Replace(string(data), "Colors", "Colorz", 1), 0o644)
	if _, err := cmd(nil, "verify", "-key", public, indexPath); !errors.Is(err, index.ErrBadSignature) {
		t.Errorf("verify a changed index: %v", err)
	}
}

func TestSignRefusesAnIndexFlintWouldRefuse(t *testing.T) {
	_, secret, _ := index.GenerateKey()
	dir := t.TempDir()
	write(t, dir, "index.json", `{"format":1,"generatedAt":"2026-01-01T00:00:00Z","extensions":[{"id":"bad id"}]}`, 0o644)
	if _, err := cmd(map[string]string{DefaultKeyEnv: secret}, "sign", filepath.Join(dir, "index.json")); err == nil || !strings.Contains(err.Error(), "invalid id") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json.sig")); err == nil {
		t.Error("a signature was written")
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil, {"help"}, {"keygen", "extra"}, {"build"}, {"build", "-out", "x"}, {"build", "-out", "x", "-base-url", "https://e/"},
		{"sign"}, {"sign", "a", "b"}, {"verify", "index.json"}, {"verify", "-key", "k"}, {"sign", "-nope", "x"},
	} {
		if _, err := cmd(nil, args...); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v, want usage", args, err)
		}
	}
}
