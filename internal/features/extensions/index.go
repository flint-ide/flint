package extensions

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"flint/internal/extensions/host"
	"flint/internal/extensions/index"
	"flint/internal/extensions/manifest"
	"flint/internal/update"
)

// IndexStatus is what Settings ▸ Extensions shows of the signed extension
// index (internal/extensions/index): where it is, whether a key to verify
// it is configured, and what the last verified download lists.
type IndexStatus struct {
	// URL is the index Flint downloads when asked.
	URL string `json:"url"`
	// Official: URL and key are the built-in ones (nothing set in settings).
	Official bool `json:"official"`
	// Configured: there is a public key to verify the index with. False for
	// the official index until its key is built into Flint.
	Configured bool `json:"configured"`
	// Fetched: Entries come from an index whose signature verified.
	Fetched bool `json:"fetched"`
	// FetchedAt is when that index was downloaded; GeneratedAt when it was built.
	FetchedAt   time.Time `json:"fetchedAt"`
	GeneratedAt time.Time `json:"generatedAt"`
	// Platform is this machine's archive key ("darwin-arm64").
	Platform string       `json:"platform"`
	Entries  []IndexEntry `json:"entries"`
}

// IndexEntry is one extension of the index, for display before anything is
// downloaded.
type IndexEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Publisher   string   `json:"publisher"`
	Version     string   `json:"version"`
	Kind        string   `json:"kind"`
	Permissions []string `json:"permissions"`
	// PermissionTexts explain Permissions, in the same order.
	PermissionTexts []string     `json:"permissionTexts"`
	AgentTools      []index.Tool `json:"agentTools"`
	// Size is the download for this platform, in bytes.
	Size int64 `json:"size"`
	// Error says why it can't be installed here ("" when it can).
	Error string `json:"error"`
}

// indexStore keeps the last verified index and installs from it. Nothing
// here runs on its own: every request is the result of a user action in
// Settings ▸ Extensions or the command palette.
type indexStore struct {
	host     *host.Host
	settings func() Settings
	client   *index.Client
	// dir caches the verified index and its signature between launches, so
	// "Update to x.y.z" shows without a request; both are verified again when
	// they are read.
	dir string

	mu     sync.Mutex
	loaded bool // the cache was read
	ix     *index.Index
	at     time.Time
	source string // url + "\n" + key the index was verified for
}

const (
	cacheIndex  = index.FileName
	cacheSig    = index.FileName + index.SignatureSuffix
	cacheSource = "source"
)

var errNotConfigured = errors.New("the extension index is not configured: no public key to verify it is built into this Flint or set in extensions.indexPublicKey")

func sourceKey(url, key string) string { return url + "\n" + key }

// current is the verified index for the configured URL and key, nil when
// none was fetched; it reads the cache once.
func (s *indexStore) current() (*index.Index, time.Time) {
	u, key, _ := s.settings().indexSource()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ix != nil && s.source != sourceKey(u, key) {
		s.ix = nil // fetched from another index, or verified with another key
		s.loaded = false
	}
	if s.ix == nil && !s.loaded {
		s.loaded = true
		if ix, at, err := s.readCache(u, key); err == nil {
			s.ix, s.at, s.source = ix, at, sourceKey(u, key)
		}
	}
	return s.ix, s.at
}

func (s *indexStore) readCache(u, key string) (*index.Index, time.Time, error) {
	pub, err := index.ParsePublicKey(key)
	if err != nil {
		return nil, time.Time{}, err
	}
	src, err := readSmall(filepath.Join(s.dir, cacheSource), 4<<10)
	if err != nil || string(src) != u {
		return nil, time.Time{}, errors.New("no cached index for this address")
	}
	data, err := readSmall(filepath.Join(s.dir, cacheIndex), index.MaxIndexBytes)
	if err != nil {
		return nil, time.Time{}, err
	}
	sig, err := readSmall(filepath.Join(s.dir, cacheSig), index.MaxSignatureBytes)
	if err != nil {
		return nil, time.Time{}, err
	}
	ix, err := index.Open(data, sig, pub)
	if err != nil {
		return nil, time.Time{}, err
	}
	at := time.Time{}
	if info, err := os.Stat(filepath.Join(s.dir, cacheIndex)); err == nil {
		at = info.ModTime()
	}
	return ix, at, nil
}

func readSmall(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is too large", filepath.Base(path))
	}
	return os.ReadFile(path)
}

// writeCache is best effort: without it the index is asked for again.
func (s *indexStore) writeCache(u string, f *index.Fetched) {
	if os.MkdirAll(s.dir, 0o700) != nil {
		return
	}
	// The source goes last, so a half-written cache is never read as valid
	// for this address (and a wrong pair fails its signature anyway).
	_ = os.Remove(filepath.Join(s.dir, cacheSource))
	for name, data := range map[string][]byte{cacheIndex: f.Data, cacheSig: f.Signature} {
		if os.WriteFile(filepath.Join(s.dir, name), data, 0o600) != nil {
			return
		}
	}
	_ = os.WriteFile(filepath.Join(s.dir, cacheSource), []byte(u), 0o600)
}

// status describes the index without any request.
func (s *indexStore) status() IndexStatus {
	u, key, own := s.settings().indexSource()
	st := IndexStatus{
		URL: u, Official: !own && u == index.DefaultURL, Configured: key != "",
		Platform: index.Platform(), Entries: []IndexEntry{},
	}
	ix, at := s.current()
	if ix == nil {
		return st
	}
	st.Fetched, st.FetchedAt, st.GeneratedAt = true, at, ix.GeneratedAt
	for _, e := range ix.Extensions {
		st.Entries = append(st.Entries, entryInfo(e))
	}
	return st
}

func entryInfo(e index.Entry) IndexEntry {
	out := IndexEntry{
		ID: e.ID, Name: e.Name, Description: e.Description, Publisher: e.Publisher, Version: e.Version, Kind: e.Kind,
		Permissions: e.Permissions, PermissionTexts: []string{}, AgentTools: e.AgentTools,
	}
	for _, p := range e.Permissions {
		out.PermissionTexts = append(out.PermissionTexts, host.PermissionText(p))
	}
	a, err := installable(e)
	if err != nil {
		out.Error = err.Error()
	}
	out.Size = a.Size
	return out
}

// installable is the entry's archive for this machine, or why there is none.
func installable(e index.Entry) (index.Archive, error) {
	if e.Contract != manifest.Contract {
		if e.Contract > manifest.Contract {
			return index.Archive{}, fmt.Errorf("%s needs a newer Flint (extension contract %d; this Flint supports %d)", e.Name, e.Contract, manifest.Contract)
		}
		return index.Archive{}, fmt.Errorf("%s was built for an older Flint (extension contract %d; this Flint supports %d)", e.Name, e.Contract, manifest.Contract)
	}
	a, ok := e.ArchiveFor(index.Platform())
	if !ok {
		return index.Archive{}, fmt.Errorf("%s has no build for this computer (%s)", e.Name, index.Platform())
	}
	return a, nil
}

// fetch downloads the index and its signature, verifies them and remembers
// the result. A failed fetch keeps what was there.
func (s *indexStore) fetch(ctx context.Context) (IndexStatus, error) {
	u, key, _ := s.settings().indexSource()
	if key == "" {
		return s.status(), errNotConfigured
	}
	pub, err := index.ParsePublicKey(key)
	if err != nil {
		return s.status(), err
	}
	f, err := s.client.Fetch(ctx, u, pub)
	if err != nil {
		return s.status(), fmt.Errorf("couldn't get the extension index from %s: %w", hostOf(u), err)
	}
	s.mu.Lock()
	s.ix, s.at, s.source, s.loaded = f.Index, time.Now(), sourceKey(u, key), true
	s.writeCache(u, f)
	s.mu.Unlock()
	return s.status(), nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// install downloads the entry's archive for this machine, checks it against
// the index, unpacks it into a temporary folder and hands that folder to
// the host's install: the same manifest rules, native confirmation and copy
// as "Install from Folder…", after the manifest's id and version were
// compared with the entry's.
func (s *indexStore) install(ctx context.Context, id string) (host.Info, error) {
	ix, _ := s.current()
	if ix == nil {
		return host.Info{}, errors.New("get the extension index first: click Browse Extensions in Settings ▸ Extensions")
	}
	e, ok := ix.Find(id)
	if !ok {
		return host.Info{}, fmt.Errorf("%s is not in the extension index. Click Check for Updates to see what it lists now", id)
	}
	a, err := installable(e)
	if err != nil {
		return host.Info{}, err
	}
	tmp, err := os.MkdirTemp("", "flint-extension-")
	if err != nil {
		return host.Info{}, err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, "download.tar.gz")
	if err := s.client.Download(ctx, a, file); err != nil {
		return host.Info{}, fmt.Errorf("couldn't download %s %s: %w", e.Name, e.Version, err)
	}
	dir := filepath.Join(tmp, e.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return host.Info{}, err
	}
	if err := index.Extract(file, dir); err != nil {
		return host.Info{}, fmt.Errorf("%s %s can't be installed: %w", e.Name, e.Version, err)
	}
	u, _, _ := s.settings().indexSource()
	return s.host.InstallExpected(dir, host.Expected{ID: e.ID, Version: e.Version, Note: installNote(u, e, s.installedVersion(e.ID))})
}

func (s *indexStore) installedVersion(id string) string {
	for _, info := range s.host.List() {
		if info.ID == id {
			return info.Version
		}
	}
	return ""
}

// installNote opens the confirmation of an install from the index.
func installNote(indexURL string, e index.Entry, installed string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Downloaded from the extension index at %s. The index's signature and this download's SHA-256 were checked.", hostOf(indexURL))
	if order, ok := update.Compare(e.Version, installed); ok && order < 0 {
		fmt.Fprintf(&b, "\nThis is an older version (%s) than the one installed (%s).", e.Version, installed)
	}
	return b.String()
}

// Index is the signed extension index as last downloaded and verified (or
// read from its cache); it makes no request.
func (a *API) Index() IndexStatus { return a.index.status() }

// FetchIndex downloads the index and its signature and verifies them. It
// runs only when the user asks: Browse Extensions or Check for Updates.
func (a *API) FetchIndex() (IndexStatus, error) { return a.index.fetch(a.ctx()) }

// InstallFromIndex downloads, verifies and unpacks an extension of the
// index, then installs it like Install (native confirmation); nil when the
// user cancelled.
func (a *API) InstallFromIndex(id string) (*host.Info, error) {
	info, err := a.index.install(a.ctx(), id)
	if errors.Is(err, host.ErrCancelled) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}
