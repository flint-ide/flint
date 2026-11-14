package index

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const themeManifest = `{"contract":1,"id":"me.colors","name":"Colors","version":"1.2.0","description":"Two themes.",
 "contributes":{"themes":[{"id":"me.colors.dark","label":"Dark","base":"dark","colors":{"--bg":"#101010"}}]}}`

const programManifest = `{"contract":1,"id":"me.tool","name":"Tool","version":"0.3.1","publisher":"Me",
 "run":{"command":"bin/tool"},"permissions":["workspace.read","network:example.com"],
 "activation":["onCommand:me.tool.go"],
 "contributes":{"commands":[{"id":"me.tool.go","title":"Go"}],
  "agentTools":[{"name":"look","description":"Looks.","inputSchema":{"type":"object"}},
                {"name":"change","description":"Changes.","inputSchema":{"type":"object"},"acts":true}]}}`

// folder writes files (name → content; a name ending in "*" is executable).
func folder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, "*") {
			name, mode = strings.TrimSuffix(name, "*"), 0o755
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func validIndex() map[string]any {
	return map[string]any{
		"format":      1,
		"generatedAt": "2026-01-02T03:04:05Z",
		"extensions": []any{
			map[string]any{
				"id": "me.colors", "name": "Colors", "version": "1.2.0", "contract": 1, "kind": "themes",
				"permissions": []string{}, "agentTools": []any{},
				"archives": map[string]any{"any": map[string]any{"url": "https://example.com/a.tar.gz", "sha256": strings.Repeat("ab", 32), "size": 10}},
			},
			map[string]any{
				"id": "me.tool", "name": "Tool", "version": "0.3.1", "contract": 1, "kind": "program",
				"permissions": []string{"workspace.read"}, "agentTools": []any{map[string]any{"name": "look"}, map[string]any{"name": "change", "acts": true}},
				"archives": map[string]any{
					"linux-amd64":  map[string]any{"url": "https://example.com/l.tar.gz", "sha256": strings.Repeat("cd", 32), "size": 20},
					"darwin-arm64": map[string]any{"url": "https://example.com/d.tar.gz", "sha256": strings.Repeat("ef", 32), "size": 30},
				},
			},
		},
	}
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParse(t *testing.T) {
	ix, err := Parse(encode(t, validIndex()))
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Extensions) != 2 || !ix.GeneratedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("index = %+v", ix)
	}
	tool, ok := ix.Find("me.tool")
	if !ok || tool.Kind != KindProgram || len(tool.AgentTools) != 2 || !tool.AgentTools[1].Acts {
		t.Fatalf("tool = %+v", tool)
	}
	if a, ok := tool.ArchiveFor("linux-amd64"); !ok || a.Size != 20 {
		t.Errorf("linux archive = %+v, %v", a, ok)
	}
	if _, ok := tool.ArchiveFor("linux-arm64"); ok {
		t.Error("an archive for a platform the index doesn't list")
	}
	colors, _ := ix.Find("me.colors")
	if a, ok := colors.ArchiveFor("linux-arm64"); !ok || a.Size != 10 {
		t.Errorf("the any archive doesn't serve every platform: %+v, %v", a, ok)
	}
	if _, ok := ix.Find("me.none"); ok {
		t.Error("found an entry that isn't there")
	}
}

func TestParseRefuses(t *testing.T) {
	entry := func(doc map[string]any, i int) map[string]any { return doc["extensions"].([]any)[i].(map[string]any) }
	archive := func(doc map[string]any, i int, platform string) map[string]any {
		return entry(doc, i)["archives"].(map[string]any)[platform].(map[string]any)
	}
	cases := []struct {
		name string
		edit func(doc map[string]any)
		want string
	}{
		{"newer format", func(d map[string]any) { d["format"] = 2 }, "format 2"},
		{"no format", func(d map[string]any) { delete(d, "format") }, "format 0"},
		{"bad id", func(d map[string]any) { entry(d, 0)["id"] = "../x" }, "invalid id"},
		{"duplicate id", func(d map[string]any) { entry(d, 1)["id"] = "me.colors" }, "listed twice"},
		{"no name", func(d map[string]any) { entry(d, 0)["name"] = "" }, `"name" is missing`},
		{"control characters", func(d map[string]any) { entry(d, 0)["description"] = "a\x1b[31mb" }, "control characters"},
		{"bad version", func(d map[string]any) { entry(d, 0)["version"] = "latest" }, "major.minor.patch"},
		{"no contract", func(d map[string]any) { delete(entry(d, 0), "contract") }, `"contract" is missing`},
		{"unknown kind", func(d map[string]any) { entry(d, 0)["kind"] = "virus" }, "unknown kind"},
		{"bad tool name", func(d map[string]any) { entry(d, 1)["agentTools"] = []any{map[string]any{"name": "Bad Name"}} }, "agent tool name"},
		{"no archives", func(d map[string]any) { entry(d, 0)["archives"] = map[string]any{} }, "no archives"},
		{"unknown platform", func(d map[string]any) {
			a := entry(d, 1)["archives"].(map[string]any)
			a["windows-amd64"] = a["linux-amd64"]
		}, "unknown platform"},
		{"program as any", func(d map[string]any) {
			a := entry(d, 1)["archives"].(map[string]any)
			entry(d, 1)["archives"] = map[string]any{"any": a["linux-amd64"]}
		}, "archive per platform"},
		{"themes per platform", func(d map[string]any) {
			a := entry(d, 0)["archives"].(map[string]any)
			entry(d, 0)["archives"] = map[string]any{"linux-amd64": a["any"]}
		}, "without a program"},
		{"http archive", func(d map[string]any) { archive(d, 0, "any")["url"] = "http://example.com/a.tar.gz" }, "not https"},
		{"file archive", func(d map[string]any) { archive(d, 0, "any")["url"] = "file:///etc/passwd" }, "use https://"},
		{"credentials in url", func(d map[string]any) { archive(d, 0, "any")["url"] = "https://u:p@example.com/a" }, "user name or password"},
		{"short hash", func(d map[string]any) { archive(d, 0, "any")["sha256"] = "abcd" }, "sha256"},
		{"upper-case hash", func(d map[string]any) { archive(d, 0, "any")["sha256"] = strings.Repeat("AB", 32) }, "sha256"},
		{"no size", func(d map[string]any) { archive(d, 0, "any")["size"] = 0 }, "size"},
		{"huge size", func(d map[string]any) { archive(d, 0, "any")["size"] = MaxArchiveBytes + 1 }, "size"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := validIndex()
			c.edit(doc)
			_, err := Parse(encode(t, doc))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
	if _, err := Parse([]byte("not json")); err == nil {
		t.Error("garbage parsed")
	}
	if _, err := Parse(make([]byte, MaxIndexBytes+1)); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("oversize: %v", err)
	}
	many := validIndex()
	var list []any
	for i := 0; i <= MaxEntries; i++ {
		list = append(list, entry(validIndex(), 0))
	}
	many["extensions"] = list
	if _, err := Parse(encode(t, many)); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too many entries: %v", err)
	}
}

func TestCheckURL(t *testing.T) {
	for _, ok := range []string{"https://example.com/index.json", "http://127.0.0.1:8080/i", "http://localhost/i", "http://[::1]:9/i"} {
		if err := CheckURL(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "index.json", "http://example.com/i", "ftp://example.com/i", "https://", "https://u@example.com/i",
		"http://127.0.0.1.example.com/i", "javascript:alert(1)", "https://example.com/" + strings.Repeat("a", 3000)} {
		if err := CheckURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func keyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, sec, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSecretKey(sec)
	if err != nil {
		t.Fatal(err)
	}
	if PublicOf(s) != pub {
		t.Fatal("the secret key's public half is not the public key")
	}
	return p, s
}

func TestSignAndVerify(t *testing.T) {
	pub, sec := keyPair(t)
	other, _ := keyPair(t)
	data := encode(t, validIndex())
	sig := Sign(sec, data)
	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("good signature: %v", err)
	}
	if err := Verify(pub, data, bytes.TrimSpace(sig)); err != nil {
		t.Errorf("signature without the final newline: %v", err)
	}
	tampered := bytes.Replace(data, []byte("Colors"), []byte("Colorz"), 1)
	if err := Verify(pub, tampered, sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered index: %v", err)
	}
	if err := Verify(pub, append(data, '\n'), sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("index with one more byte: %v", err)
	}
	if err := Verify(other, data, sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("wrong key: %v", err)
	}
	if err := Verify(nil, data, sig); !errors.Is(err, ErrNoKey) {
		t.Errorf("empty key: %v", err)
	}
	for _, bad := range [][]byte{nil, []byte("not base64!"), []byte(base64.StdEncoding.EncodeToString([]byte("short"))), bytes.Repeat([]byte("A"), MaxSignatureBytes+1)} {
		if err := Verify(pub, data, bad); !errors.Is(err, ErrBadSignature) {
			t.Errorf("signature %q: %v", bad, err)
		}
	}
	if _, err := Open(tampered, sig, pub); !errors.Is(err, ErrBadSignature) {
		t.Errorf("Open parsed a tampered index: %v", err)
	}
	if ix, err := Open(data, sig, pub); err != nil || len(ix.Extensions) != 2 {
		t.Errorf("Open: %v", err)
	}
}

func TestKeys(t *testing.T) {
	if OfficialPublicKey != "" {
		// The constant is set by the maintainer; it must then be a real key.
		if _, err := ParsePublicKey(OfficialPublicKey); err != nil {
			t.Fatalf("OfficialPublicKey: %v", err)
		}
	}
	if _, err := ParsePublicKey(""); !errors.Is(err, ErrNoKey) {
		t.Errorf("empty public key: %v", err)
	}
	pub, sec, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"x", sec, pub[:40], base64.StdEncoding.EncodeToString(make([]byte, 31))} {
		if _, err := ParsePublicKey(bad); err == nil || errors.Is(err, ErrNoKey) {
			t.Errorf("public key %q: %v", bad, err)
		}
	}
	for _, bad := range []string{"", "x", pub} {
		if _, err := ParseSecretKey(bad); err == nil {
			t.Errorf("secret key %q accepted", bad)
		}
	}
	raw, _ := base64.StdEncoding.DecodeString(sec)
	raw[40] ^= 1 // the public half no longer fits the seed
	if _, err := ParseSecretKey(base64.StdEncoding.EncodeToString(raw)); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("damaged secret key: %v", err)
	}
	if _, err := ParseSecretKey(sec[:20]); err == nil || strings.Contains(err.Error(), sec[:20]) {
		t.Errorf("a bad secret key is echoed or accepted: %v", err)
	}
}

// ---------- archives ----------

type tarEntry struct {
	name, body, link string
	flag             byte
	mode             int64
}

func tarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.flag, Mode: e.mode, Linkname: e.link, Size: int64(len(e.body))}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtract(t *testing.T) {
	dst := t.TempDir()
	data := tarball(t, []tarEntry{
		{name: "./", flag: tar.TypeDir, mode: 0o755},
		{name: "flint-extension.json", body: "{}"},
		{name: "bin/", flag: tar.TypeDir, mode: 0o755},
		{name: "bin/tool", body: "#!/bin/sh\n", mode: 0o4755},
		{name: "deep/er/file.txt", body: "x", mode: 0o600},
	})
	if err := ExtractFrom(bytes.NewReader(data), dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "bin", "tool"))
	if err != nil || info.Mode().Perm() != 0o755 || info.Mode()&os.ModeSetuid != 0 {
		t.Errorf("bin/tool: %v, %v", info, err)
	}
	info, err = os.Stat(filepath.Join(dst, "deep", "er", "file.txt"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("deep/er/file.txt: %v, %v", info, err)
	}
}

func TestExtractRefusesAttacks(t *testing.T) {
	deep := strings.Repeat("d/", MaxExtractDepth+1) + "f"
	cases := []struct {
		name    string
		entries []tarEntry
		want    string
	}{
		{"parent path", []tarEntry{{name: "../evil"}}, "path Flint refuses"},
		{"parent inside", []tarEntry{{name: "a/../../evil"}}, "path Flint refuses"},
		{"hidden parent", []tarEntry{{name: "a/b/../../../evil"}}, "path Flint refuses"},
		{"absolute path", []tarEntry{{name: "/etc/cron.d/evil"}}, "path Flint refuses"},
		{"backslash", []tarEntry{{name: `..\evil`}}, "path Flint refuses"},
		{"symlink", []tarEntry{{name: "link", flag: tar.TypeSymlink, link: "/etc/passwd"}}, "symbolic link"},
		{"symlink then file through it", []tarEntry{{name: "out", flag: tar.TypeSymlink, link: ".."}, {name: "out/evil", body: "x"}}, "symbolic link"},
		{"hard link", []tarEntry{{name: "a", body: "x"}, {name: "b", flag: tar.TypeLink, link: "a"}}, "hard link"},
		{"device", []tarEntry{{name: "null", flag: tar.TypeChar}}, "device"},
		{"fifo", []tarEntry{{name: "pipe", flag: tar.TypeFifo}}, "FIFO"},
		{"same file twice", []tarEntry{{name: "a", body: "1"}, {name: "./a", body: "2"}}, "twice"},
		{"file over folder", []tarEntry{{name: "a/", flag: tar.TypeDir}, {name: "a", body: "2"}}, "twice"},
		{"too deep", []tarEntry{{name: deep, body: "x"}}, "deeper"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			dst := filepath.Join(parent, "dst")
			if err := os.Mkdir(dst, 0o755); err != nil {
				t.Fatal(err)
			}
			err := ExtractFrom(bytes.NewReader(tarball(t, c.entries)), dst)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
			if _, err := os.Lstat(filepath.Join(parent, "evil")); err == nil {
				t.Error("a file was written outside the destination")
			}
		})
	}
	if err := ExtractFrom(strings.NewReader("plain text"), t.TempDir()); err == nil || !strings.Contains(err.Error(), ".tar.gz") {
		t.Errorf("not gzip: %v", err)
	}
}

func TestExtractLimits(t *testing.T) {
	var many []tarEntry
	for i := 0; i <= MaxExtractFiles; i++ {
		many = append(many, tarEntry{name: "f" + strings.Repeat("x", i%7) + "-" + hex.EncodeToString([]byte{byte(i >> 8), byte(i)})})
	}
	if err := ExtractFrom(bytes.NewReader(tarball(t, many)), t.TempDir()); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too many files: %v", err)
	}

	// A bomb: little compressed data, a declared size past the limit. It is
	// refused from the header, before anything is written.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Name: "big", Typeflag: tar.TypeReg, Mode: 0o644, Size: MaxExtractBytes + 1}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Flush()
	_ = zw.Close()
	dst := t.TempDir()
	if err := ExtractFrom(&buf, dst); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Errorf("bomb: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "big")); err == nil {
		t.Error("the oversized file was created")
	}

	// The total counts across files: two files that each fit, together not.
	body := strings.Repeat("x", 600)
	two := tarball(t, []tarEntry{{name: "a", body: body}, {name: "b", body: body}})
	if err := extract(bytes.NewReader(two), t.TempDir(), 1000); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("total size: %v", err)
	}
	if err := extract(bytes.NewReader(two), t.TempDir(), 1200); err != nil {
		t.Errorf("exactly at the limit: %v", err)
	}
}

func TestPackIsDeterministicAndRoundTrips(t *testing.T) {
	dir := folder(t, map[string]string{"flint-extension.json": programManifest, "bin/tool*": "#!/bin/sh\n", "web/page.html": "<p>hi</p>", ".git/config": "secret"})
	var a, b bytes.Buffer
	if err := Pack(dir, &a); err != nil {
		t.Fatal(err)
	}
	// Times must not matter.
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "web", "page.html"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := Pack(dir, &b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("packing the same folder twice gave different bytes")
	}
	dst := t.TempDir()
	if err := ExtractFrom(&a, dst); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dst, "bin", "tool")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("program not executable after the round trip: %v, %v", info, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "web", "page.html")); string(got) != "<p>hi</p>" {
		t.Errorf("page = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Error(".git was packed")
	}

	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := Pack(dir, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a symbolic link was packed: %v", err)
	}
}

// ---------- download ----------

func archiveOf(data []byte, url string) Archive {
	sum := sha256.Sum256(data)
	return Archive{URL: url, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

func TestFetch(t *testing.T) {
	pub, sec := keyPair(t)
	other, _ := keyPair(t)
	data := encode(t, validIndex())
	files := map[string][]byte{"/index.json": data, "/index.json.sig": Sign(sec, data)}
	var requests []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Clone(context.Background()))
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "track", Value: "me"})
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	c := &Client{Version: "v9.9.9"}
	got, err := c.Fetch(context.Background(), srv.URL+"/index.json", pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Index.Extensions) != 2 || !bytes.Equal(got.Data, data) {
		t.Errorf("fetched = %+v", got.Index)
	}
	if len(requests) != 2 {
		t.Fatalf("%d requests, want 2", len(requests))
	}
	for _, r := range requests {
		if r.Method != http.MethodGet || r.Header.Get("User-Agent") != "Flint/v9.9.9" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("request %s %s: headers %v", r.Method, r.URL, r.Header)
		}
	}

	requests = nil
	if _, err := c.Fetch(context.Background(), srv.URL+"/index.json", nil); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
	if len(requests) != 0 {
		t.Error("a request was made without a key to verify the answer")
	}
	if _, err := c.Fetch(context.Background(), srv.URL+"/index.json", other); !errors.Is(err, ErrBadSignature) {
		t.Errorf("wrong key: %v", err)
	}
	files["/index.json"] = bytes.Replace(data, []byte("Colors"), []byte("Colorz"), 1)
	if _, err := c.Fetch(context.Background(), srv.URL+"/index.json", pub); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered index: %v", err)
	}
	files["/index.json"] = data
	delete(files, "/index.json.sig")
	if _, err := c.Fetch(context.Background(), srv.URL+"/index.json", pub); err == nil || !strings.Contains(err.Error(), "no signature") {
		t.Errorf("missing signature: %v", err)
	}
	files["/big.json"] = make([]byte, MaxIndexBytes+1)
	if _, err := c.Fetch(context.Background(), srv.URL+"/big.json", pub); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("oversized index: %v", err)
	}
	if _, err := c.Fetch(context.Background(), "http://example.invalid/index.json", pub); err == nil || !strings.Contains(err.Error(), "not https") {
		t.Errorf("http index: %v", err)
	}
}

func TestDownload(t *testing.T) {
	body := []byte("the archive's bytes")
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.tar.gz":
			_, _ = w.Write(body)
		case "/long.tar.gz":
			_, _ = w.Write(append(bytes.Clone(body), "and more"...))
		case "/stream.tar.gz": // no Content-Length: the size is only known by reading
			w.(http.Flusher).Flush()
			_, _ = w.Write(append(bytes.Clone(body), "and more"...))
		case "/moved":
			http.Redirect(w, r, "/a.tar.gz", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/away":
			http.Redirect(w, r, "http://example.invalid/a.tar.gz", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer plain.Close()
	c := &Client{Version: "test"}
	ctx := context.Background()
	dst := func() string { return filepath.Join(t.TempDir(), "a.tar.gz") }

	good := dst()
	if err := c.Download(ctx, archiveOf(body, plain.URL+"/a.tar.gz"), good); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(good); !bytes.Equal(got, body) {
		t.Errorf("downloaded %q", got)
	}
	if err := c.Download(ctx, archiveOf(body, plain.URL+"/moved"), dst()); err != nil {
		t.Errorf("redirect on the same server: %v", err)
	}

	cases := []struct {
		name string
		a    Archive
		want string
	}{
		{"hash mismatch", Archive{URL: plain.URL + "/a.tar.gz", SHA256: strings.Repeat("00", 32), Size: int64(len(body))}, "SHA-256"},
		{"larger than the index says", archiveOf(body, plain.URL+"/long.tar.gz"), "larger"},
		{"larger, without a length", archiveOf(body, plain.URL+"/stream.tar.gz"), "not the size"},
		{"smaller than the index says", archiveOf(append(bytes.Clone(body), 'x'), plain.URL+"/a.tar.gz"), "not the size"},
		{"past the hard cap", Archive{URL: plain.URL + "/a.tar.gz", SHA256: strings.Repeat("00", 32), Size: MaxArchiveBytes + 1}, "size must be"},
		{"missing", archiveOf(body, plain.URL+"/nope"), "404"},
		{"redirect loop", archiveOf(body, plain.URL+"/loop"), "too many redirects"},
		{"redirect to a non-loopback http host", archiveOf(body, plain.URL+"/away"), "not https"},
		{"http to a non-loopback host", archiveOf(body, "http://example.invalid/a.tar.gz"), "not https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := dst()
			err := c.Download(ctx, tc.a, p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if _, err := os.Stat(p); err == nil {
				t.Error("a refused download left a file")
			}
		})
	}
}

// An https download must never continue over http, not even to loopback.
func TestDownloadRefusesRedirectFromHTTPSToHTTP(t *testing.T) {
	body := []byte("bytes")
	served := false
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		_, _ = w.Write(body)
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			_, _ = w.Write(body)
			return
		}
		http.Redirect(w, r, plain.URL+"/a.tar.gz", http.StatusFound)
	}))
	defer secure.Close()
	c := &Client{Version: "test", Transport: secure.Client().Transport}
	p := filepath.Join(t.TempDir(), "a")
	err := c.Download(context.Background(), archiveOf(body, secure.URL+"/a.tar.gz"), p)
	if err == nil || !strings.Contains(err.Error(), "from https to http") {
		t.Errorf("err = %v", err)
	}
	if served {
		t.Error("the http server was asked")
	}
	if err := c.Download(context.Background(), archiveOf(body, secure.URL+"/ok"), p); err != nil {
		t.Errorf("https download: %v", err)
	}
}

// ---------- build ----------

func TestBuild(t *testing.T) {
	theme := folder(t, map[string]string{"flint-extension.json": themeManifest, "README.md": "hi"})
	linux := folder(t, map[string]string{"flint-extension.json": programManifest, "bin/tool*": "linux"})
	mac := folder(t, map[string]string{"flint-extension.json": programManifest, "bin/tool*": "darwin"})
	// A prebuilt archive is accepted too.
	var packed bytes.Buffer
	if err := Pack(mac, &packed); err != nil {
		t.Fatal(err)
	}
	macArchive := filepath.Join(t.TempDir(), "mac.tar.gz")
	if err := os.WriteFile(macArchive, packed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	now := time.Date(2026, 5, 6, 7, 8, 9, 123, time.UTC)
	ix, err := Build(BuildOptions{
		Inputs:  []Input{ParseInput(theme), ParseInput("linux-amd64=" + linux), ParseInput("darwin-arm64=" + macArchive)},
		OutDir:  out,
		BaseURL: "https://example.com/dl",
		Now:     now,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, FileName))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("the built index doesn't parse: %v\n%s", err, data)
	}
	if len(parsed.Extensions) != 2 || parsed.Extensions[0].ID != "me.colors" || !parsed.GeneratedAt.Equal(now.Truncate(time.Second)) {
		t.Fatalf("index = %s", data)
	}
	colors, tool := parsed.Extensions[0], parsed.Extensions[1]
	if colors.Kind != KindThemes || colors.Archives[Any].URL != "https://example.com/dl/me.colors-1.2.0-any.tar.gz" {
		t.Errorf("colors = %+v", colors)
	}
	if tool.Kind != KindProgram || len(tool.Archives) != 2 || tool.Publisher != "Me" ||
		strings.Join(tool.Permissions, " ") != "workspace.read network:example.com" ||
		len(tool.AgentTools) != 2 || tool.AgentTools[0].Acts || !tool.AgentTools[1].Acts {
		t.Errorf("tool = %+v", tool)
	}
	if err := CheckFiles(ix, out); err != nil {
		t.Errorf("CheckFiles: %v", err)
	}
	// The archive Flint would download unpacks to an installable folder.
	dst := t.TempDir()
	if err := Extract(filepath.Join(out, ArchiveName("me.tool", "0.3.1", "linux-amd64")), dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "bin", "tool")); string(got) != "linux" {
		t.Errorf("program = %q", got)
	}
	if err := os.WriteFile(filepath.Join(out, ArchiveName("me.colors", "1.2.0", Any)), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckFiles(ix, out); err == nil {
		t.Error("CheckFiles accepted a changed archive")
	}
}

func TestBuildRefuses(t *testing.T) {
	theme := folder(t, map[string]string{"flint-extension.json": themeManifest})
	program := folder(t, map[string]string{"flint-extension.json": programManifest, "bin/tool*": "x"})
	unbuilt := folder(t, map[string]string{"flint-extension.json": programManifest})
	notExec := folder(t, map[string]string{"flint-extension.json": programManifest, "bin/tool": "x"})
	newer := folder(t, map[string]string{"flint-extension.json": strings.Replace(programManifest, "0.3.1", "0.4.0", 1), "bin/tool*": "x"})
	broken := folder(t, map[string]string{"flint-extension.json": `{"contract":1,"id":"Bad Id","name":"x","version":"1.0.0"}`})
	cases := []struct {
		name   string
		inputs []string
		base   string
		want   string
	}{
		{"nothing", nil, "https://example.com/", "nothing to build"},
		{"http base", []string{theme}, "http://example.com/", "not https"},
		{"program without platform", []string{program}, "https://example.com/", "name the platform"},
		{"theme with platform", []string{"linux-amd64=" + theme}, "https://example.com/", "no program"},
		{"program not built", []string{"linux-amd64=" + unbuilt}, "https://example.com/", "missing"},
		{"program not executable", []string{"linux-amd64=" + notExec}, "https://example.com/", "not an executable"},
		{"platforms disagree", []string{"linux-amd64=" + program, "darwin-arm64=" + newer}, "https://example.com/", "differs"},
		{"same platform twice", []string{"linux-amd64=" + program, "linux-amd64=" + program}, "https://example.com/", "second linux-amd64"},
		{"broken manifest", []string{broken}, "https://example.com/", "id"},
		{"no folder", []string{filepath.Join(theme, "nope")}, "https://example.com/", "no such file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var inputs []Input
			for _, in := range c.inputs {
				inputs = append(inputs, ParseInput(in))
			}
			out := t.TempDir()
			_, err := Build(BuildOptions{Inputs: inputs, OutDir: out, BaseURL: c.base})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
			if _, err := os.Stat(filepath.Join(out, FileName)); err == nil {
				t.Error("an index was written")
			}
		})
	}
}
