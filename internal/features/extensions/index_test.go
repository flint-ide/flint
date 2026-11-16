package extensions

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"flint/internal/extensions/host"
	"flint/internal/extensions/index"
	"flint/internal/extensions/manifest"
)

// indexApp is the Flint side of the host: it records what was asked and
// answers every confirmation with confirm.
type indexApp struct {
	mu      sync.Mutex
	asked   []string
	confirm bool
}

func (a *indexApp) Roots() []string                        { return nil }
func (a *indexApp) Trusted(string) bool                    { return true }
func (a *indexApp) Emit(string, ...any)                    {}
func (a *indexApp) OpenURL(string) error                   { return nil }
func (a *indexApp) SetEnabled(_ string, def bool) bool     { return def }
func (a *indexApp) Enabled(string) bool                    { return true }
func (a *indexApp) Version() string                        { return "test" }
func (a *indexApp) Settings(string) json.RawMessage        { return nil }
func (a *indexApp) Choose(string, string, []string) string { return "" }
func (a *indexApp) Confirm(title, message, _ string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, title+"\n"+message)
	return a.confirm
}

func (a *indexApp) lastAsked() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.asked) == 0 {
		return ""
	}
	return a.asked[len(a.asked)-1]
}

const (
	themesV1 = `{"contract":1,"id":"me.colors","name":"Colors","version":"1.0.0",
 "contributes":{"themes":[{"id":"me.colors.dark","label":"Dark","base":"dark","colors":{"--bg":"#101010"}}]}}`
	toolV1 = `{"contract":1,"id":"me.tool","name":"Tool","version":"1.0.0","run":{"command":"tool"},
 "permissions":["workspace.read"],"activation":["onCommand:me.tool.go"],
 "contributes":{"commands":[{"id":"me.tool.go","title":"Go"}],
  "agentTools":[{"name":"look","description":"Looks.","inputSchema":{"type":"object"}}]}}`
	toolV2 = `{"contract":1,"id":"me.tool","name":"Tool","version":"1.1.0","run":{"command":"tool"},
 "permissions":["workspace.read","terminal","network:api.example.com"],"activation":["onCommand:me.tool.go"],
 "contributes":{"commands":[{"id":"me.tool.go","title":"Go"}],
  "agentTools":[{"name":"look","description":"Looks.","inputSchema":{"type":"object"}},
                {"name":"change","description":"Changes.","inputSchema":{"type":"object"},"acts":true}]}}`
)

func extFolder(t *testing.T, man string, program bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	if program {
		if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// indexFixture is an index server (plain http on loopback), a key pair and
// a store over an empty extensions folder.
type indexFixture struct {
	t        *testing.T
	srv      *httptest.Server
	files    string // what the server serves
	pub      string
	sec      ed25519.PrivateKey
	settings Settings
	app      *indexApp
	host     *host.Host
	store    *indexStore
	mu       sync.Mutex
	requests []string
}

func newIndexFixture(t *testing.T) *indexFixture {
	t.Helper()
	f := &indexFixture{t: t, files: t.TempDir(), app: &indexApp{confirm: true}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		f.mu.Unlock()
		http.FileServer(http.Dir(f.files)).ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	pub, sec, err := index.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	f.pub = pub
	if f.sec, err = index.ParseSecretKey(sec); err != nil {
		t.Fatal(err)
	}
	f.settings = Settings{IndexURL: f.srv.URL + "/index.json", IndexPublicKey: pub}
	config := t.TempDir()
	f.host = host.New(host.Options{Dir: filepath.Join(config, "extensions"), App: f.app, Environ: host.MinimalEnv})
	t.Cleanup(f.host.Close)
	f.store = f.newStore(filepath.Join(config, "extension-index"))
	return f
}

func (f *indexFixture) newStore(dir string) *indexStore {
	return &indexStore{host: f.host, settings: func() Settings { return f.settings }, client: &index.Client{Version: "test"}, dir: dir}
}

// publish builds and signs an index of the folders (programs for this
// platform) into the served folder.
func (f *indexFixture) publish(folders ...string) *index.Index {
	f.t.Helper()
	var inputs []index.Input
	for _, dir := range folders {
		in := index.Input{Path: dir}
		if _, err := os.Stat(filepath.Join(dir, "tool")); err == nil {
			in.Platform = index.Platform()
		}
		inputs = append(inputs, in)
	}
	ix, err := index.Build(index.BuildOptions{Inputs: inputs, OutDir: f.files, BaseURL: f.srv.URL})
	if err != nil {
		f.t.Fatal(err)
	}
	f.sign()
	return ix
}

// sign signs the served index.json as it is now.
func (f *indexFixture) sign() {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.files, "index.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.files, "index.json.sig"), index.Sign(f.sec, data), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// rewrite changes the served index and signs it again.
func (f *indexFixture) rewrite(edit func(ix *index.Index)) {
	f.t.Helper()
	p := filepath.Join(f.files, "index.json")
	data, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	ix, err := index.Parse(data)
	if err != nil {
		f.t.Fatal(err)
	}
	edit(ix)
	if data, err = index.Marshal(ix); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.sign()
}

func (f *indexFixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *indexFixture) installed(id string) string {
	for _, info := range f.host.List() {
		if info.ID == id {
			return info.Version
		}
	}
	return ""
}

func TestIndexFetchAndInstall(t *testing.T) {
	f := newIndexFixture(t)
	f.publish(extFolder(t, themesV1, false), extFolder(t, toolV1, true))
	ctx := context.Background()

	// Nothing is requested before the user asks.
	st := f.store.status()
	if st.Fetched || !st.Configured || st.Official || len(st.Entries) != 0 || st.URL != f.settings.IndexURL {
		t.Fatalf("status before a fetch = %+v", st)
	}
	if _, err := f.store.install(ctx, "me.colors"); err == nil || !strings.Contains(err.Error(), "get the extension index first") {
		t.Errorf("install before a fetch: %v", err)
	}
	if n := f.requestCount(); n != 0 {
		t.Fatalf("%d requests before the user asked", n)
	}

	st, err := f.store.fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Fetched || len(st.Entries) != 2 || st.Entries[0].ID != "me.colors" || st.Entries[0].Kind != index.KindThemes || st.Entries[0].Size == 0 {
		t.Fatalf("status = %+v", st)
	}
	tool := st.Entries[1]
	if tool.Kind != index.KindProgram || tool.Error != "" || len(tool.PermissionTexts) != 1 || !strings.Contains(tool.PermissionTexts[0], "workspace.read") ||
		len(tool.AgentTools) != 1 || tool.AgentTools[0].Name != "look" {
		t.Errorf("tool = %+v", tool)
	}

	info, err := f.store.install(ctx, "me.tool")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "me.tool" || info.Version != "1.0.0" || f.installed("me.tool") != "1.0.0" {
		t.Errorf("installed = %+v", info)
	}
	asked := f.app.lastAsked()
	for _, want := range []string{`Install the extension "Tool"?`, "Downloaded from the extension index at " + strings.TrimPrefix(f.srv.URL, "http://"), "SHA-256", "Program: tool", "workspace.read", "Agent tools: look"} {
		if !strings.Contains(asked, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, asked)
		}
	}
	if st, err := os.Stat(filepath.Join(info.Dir, "tool")); err != nil || st.Mode().Perm()&0o111 == 0 {
		t.Errorf("the installed program is not executable: %v, %v", st, err)
	}

	// Saying no in the confirmation installs nothing.
	f.app.confirm = false
	if _, err := f.store.install(ctx, "me.colors"); err != host.ErrCancelled {
		t.Errorf("cancelled install: %v", err)
	}
	if f.installed("me.colors") != "" {
		t.Error("installed without the confirmation")
	}
	if _, err := f.store.install(ctx, "me.unknown"); err == nil || !strings.Contains(err.Error(), "not in the extension index") {
		t.Errorf("unknown id: %v", err)
	}
}

func TestIndexUpdateShowsWhatChanged(t *testing.T) {
	f := newIndexFixture(t)
	ctx := context.Background()
	// Installed from a folder, as before the index existed.
	if _, err := f.host.Install(extFolder(t, toolV1, true)); err != nil {
		t.Fatal(err)
	}
	f.publish(extFolder(t, toolV2, true))
	if _, err := f.store.fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if f.installed("me.tool") != "1.0.0" {
		t.Fatal("fetching the index changed an installed extension")
	}
	f.app.confirm = false
	if _, err := f.store.install(ctx, "me.tool"); err != host.ErrCancelled {
		t.Fatalf("cancelled update: %v", err)
	}
	if f.installed("me.tool") != "1.0.0" {
		t.Fatal("updated without the confirmation")
	}
	asked := f.app.lastAsked()
	for _, want := range []string{
		"Replace Tool 1.0.0 with 1.1.0?",
		"Changes from the installed version:",
		"• Version: 1.0.0 → 1.1.0",
		"• New permission: Open terminal tabs",
		"• New permission: Network access to api.example.com",
		"• New agent tool: change (acts)",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, asked)
		}
	}
	if strings.Contains(asked, "New permission: Read and open files") || strings.Contains(asked, "New agent tool: look") {
		t.Errorf("a permission or tool the installed version already had is listed as new:\n%s", asked)
	}
	f.app.confirm = true
	if info, err := f.store.install(ctx, "me.tool"); err != nil || info.Version != "1.1.0" {
		t.Fatalf("update: %+v, %v", info, err)
	}

	// Going back to an older version is said so.
	f.publish(extFolder(t, toolV1, true))
	if _, err := f.store.fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.install(ctx, "me.tool"); err != nil {
		t.Fatal(err)
	}
	if asked := f.app.lastAsked(); !strings.Contains(asked, "older version (1.0.0) than the one installed (1.1.0)") || !strings.Contains(asked, "No new permissions or agent tools") {
		t.Errorf("downgrade confirmation:\n%s", asked)
	}
}

func TestIndexRefusesWhatDoesNotMatch(t *testing.T) {
	ctx := context.Background()
	archive := func(f *indexFixture) string {
		return filepath.Join(f.files, index.ArchiveName("me.colors", "1.0.0", index.Any))
	}
	t.Run("archive changed after signing", func(t *testing.T) {
		f := newIndexFixture(t)
		f.publish(extFolder(t, themesV1, false))
		if _, err := f.store.fetch(ctx); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(archive(f))
		data[len(data)-1] ^= 1 // same size, other content
		if err := os.WriteFile(archive(f), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.install(ctx, "me.colors"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
			t.Errorf("err = %v", err)
		}
		if len(f.app.asked) != 0 || f.installed("me.colors") != "" {
			t.Error("the user was asked about, or got, a download that didn't match")
		}
	})
	t.Run("version differs from the index", func(t *testing.T) {
		f := newIndexFixture(t)
		f.publish(extFolder(t, themesV1, false))
		f.rewrite(func(ix *index.Index) { ix.Extensions[0].Version = "2.0.0" })
		if _, err := f.store.fetch(ctx); err != nil {
			t.Fatal(err)
		}
		_, err := f.store.install(ctx, "me.colors")
		if err == nil || !strings.Contains(err.Error(), "the download holds me.colors 1.0.0, but the extension index lists me.colors 2.0.0") {
			t.Errorf("err = %v", err)
		}
		if len(f.app.asked) != 0 || f.installed("me.colors") != "" {
			t.Error("asked or installed despite the mismatch")
		}
	})
	t.Run("id differs from the index", func(t *testing.T) {
		f := newIndexFixture(t)
		f.publish(extFolder(t, themesV1, false))
		f.rewrite(func(ix *index.Index) { ix.Extensions[0].ID = "me.other" })
		if _, err := f.store.fetch(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.install(ctx, "me.other"); err == nil || !strings.Contains(err.Error(), "but the extension index lists me.other") {
			t.Errorf("err = %v", err)
		}
		if len(f.app.asked) != 0 || len(f.host.List()) != 0 {
			t.Error("asked or installed despite the mismatch")
		}
	})
	t.Run("index changed after signing", func(t *testing.T) {
		f := newIndexFixture(t)
		f.publish(extFolder(t, themesV1, false))
		p := filepath.Join(f.files, "index.json")
		data, _ := os.ReadFile(p)
		if err := os.WriteFile(p, []byte(strings.Replace(string(data), "Colors", "Colorz", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		st, err := f.store.fetch(ctx)
		if err == nil || !strings.Contains(err.Error(), "signature doesn't match") {
			t.Errorf("err = %v", err)
		}
		if st.Fetched || len(st.Entries) != 0 {
			t.Errorf("entries of an unverified index are shown: %+v", st)
		}
	})
	t.Run("another contract or platform", func(t *testing.T) {
		f := newIndexFixture(t)
		f.publish(extFolder(t, themesV1, false), extFolder(t, toolV1, true))
		f.rewrite(func(ix *index.Index) {
			ix.Extensions[0].Contract = manifest.Contract + 1
			a := ix.Extensions[1].Archives[index.Platform()]
			other := "linux-arm64"
			if index.Platform() == other {
				other = "darwin-amd64"
			}
			ix.Extensions[1].Archives = map[string]index.Archive{other: a}
		})
		st, err := f.store.fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(st.Entries[0].Error, "needs a newer Flint") || !strings.Contains(st.Entries[1].Error, "no build for this computer") {
			t.Errorf("entries = %+v", st.Entries)
		}
		before := f.requestCount()
		for _, id := range []string{"me.colors", "me.tool"} {
			if _, err := f.store.install(ctx, id); err == nil {
				t.Errorf("%s installed", id)
			}
		}
		if f.requestCount() != before {
			t.Error("something was downloaded for an entry that can't be installed")
		}
	})
}

func TestIndexWithoutAKeyMakesNoRequest(t *testing.T) {
	f := newIndexFixture(t)
	f.publish(extFolder(t, themesV1, false))
	f.settings.IndexPublicKey = ""
	if index.OfficialPublicKey != "" {
		t.Skip("an official key is built in")
	}
	st := f.store.status()
	if st.Configured {
		t.Errorf("status = %+v", st)
	}
	st, err := f.store.fetch(context.Background())
	if err != errNotConfigured || st.Fetched {
		t.Errorf("fetch = %+v, %v", st, err)
	}
	if n := f.requestCount(); n != 0 {
		t.Errorf("%d requests without a key", n)
	}
	// The official index with nothing set: not configured, and said so.
	f.settings = Settings{}
	if st := f.store.status(); !st.Official || st.Configured || st.URL != index.DefaultURL {
		t.Errorf("official status = %+v", st)
	}
}

func TestIndexCacheIsVerifiedAgain(t *testing.T) {
	f := newIndexFixture(t)
	f.publish(extFolder(t, themesV1, false))
	ctx := context.Background()
	if _, err := f.store.fetch(ctx); err != nil {
		t.Fatal(err)
	}
	before := f.requestCount()

	// A new launch reads the cache: no request.
	again := f.newStore(f.store.dir)
	st := again.status()
	if !st.Fetched || len(st.Entries) != 1 || st.FetchedAt.IsZero() {
		t.Fatalf("status from the cache = %+v", st)
	}
	if f.requestCount() != before {
		t.Error("reading the cache made a request")
	}

	// Another key, or another address, in settings: the cache is not used.
	own := f.settings
	otherPub, _, _ := index.GenerateKey()
	f.settings.IndexPublicKey = otherPub
	if st := f.newStore(f.store.dir).status(); st.Fetched {
		t.Error("a cache signed with another key is shown")
	}
	if st := again.status(); st.Fetched {
		t.Error("an index verified with the old key is still shown after the key changed")
	}
	f.settings = own
	f.settings.IndexURL = f.srv.URL + "/other/index.json"
	if st := f.newStore(f.store.dir).status(); st.Fetched {
		t.Error("the cache of another address is shown")
	}
	f.settings = own

	// A cache someone edited fails its signature.
	p := filepath.Join(f.store.dir, "index.json")
	data, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), "Colors", "Colorz", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := f.newStore(f.store.dir).status(); st.Fetched {
		t.Error("an edited cache is shown")
	}
}
