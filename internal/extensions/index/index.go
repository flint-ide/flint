// Package index is the signed extension index (docs/design/extensions.md,
// "Distribution"): one JSON document that lists extensions with a prebuilt
// archive per platform, and a detached Ed25519 signature over its exact
// bytes. It is not a marketplace: one index, curated in one repository
// (flint-ide/extensions publishes it as release assets), trusted through one
// public key.
//
// Flint downloads the index only when the user asks (Settings ▸ Extensions),
// shows nothing of it before the signature verified, and installs an archive
// only after its SHA-256 matched the index, it was unpacked with the limits
// in archive.go, its manifest passed the usual rules and the user confirmed
// in the same native dialog as "Install from Folder…".
//
// cmd/flint-extindex builds, signs and verifies an index with this package.
package index

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"

	"flint/internal/extensions/manifest"
)

const (
	// Format is the index format this Flint reads ("format" in the document).
	Format = 1
	// DefaultURL is the index flint-ide/extensions publishes with each
	// release; its signature is at DefaultURL + SignatureSuffix.
	DefaultURL = "https://github.com/flint-ide/extensions/releases/latest/download/index.json"
	// SignatureSuffix is appended to the index's URL (and file name) for the
	// detached signature.
	SignatureSuffix = ".sig"
	// FileName is the index's usual file name.
	FileName = "index.json"

	// Any is the platform key of an archive without a program (themes,
	// languages): it installs everywhere.
	Any = "any"
)

// Security limits (constants, not settings).
const (
	// MaxIndexBytes bounds the index document.
	MaxIndexBytes = 4 << 20
	// MaxSignatureBytes bounds the signature file.
	MaxSignatureBytes = 1 << 10
	// MaxEntries bounds the extensions one index lists.
	MaxEntries = 2000
	// MaxArchiveBytes is the hard cap on a downloaded archive, whatever the
	// index says.
	MaxArchiveBytes = 256 << 20
	maxURLLen       = 2000
)

// Platforms are the keys of Entry.Archives besides Any.
var Platforms = []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"}

// Kinds of extension, for display before download.
const (
	KindProgram   = "program"   // runs a program
	KindThemes    = "themes"    // color themes only
	KindLanguages = "languages" // languages (with their servers, debuggers, test runners) only
	KindData      = "data"      // other data without a program
)

// Index is the signed document.
type Index struct {
	Format int `json:"format"`
	// GeneratedAt is when the index was built (RFC 3339, UTC).
	GeneratedAt time.Time `json:"generatedAt"`
	Extensions  []Entry   `json:"extensions"`
}

// Entry is one extension: what its manifest says, for display before
// anything is downloaded, and where its archives are. The installed
// manifest is what counts: Flint requires its id and version to equal the
// entry's and shows the manifest itself in the confirmation.
type Entry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	Version     string `json:"version"`
	// Contract is the extension contract the extension was built for.
	Contract int `json:"contract"`
	// Kind is one of the Kind constants.
	Kind        string   `json:"kind"`
	Permissions []string `json:"permissions"`
	AgentTools  []Tool   `json:"agentTools"`
	// Archives by platform ("darwin-arm64", …) or Any.
	Archives map[string]Archive `json:"archives"`
}

// Tool is an agent tool the extension offers.
type Tool struct {
	Name string `json:"name"`
	// Acts: the tool changes something (Flint asks before each call).
	Acts bool `json:"acts,omitempty"`
}

// Archive is a .tar.gz of the extension's folder for one platform.
type Archive struct {
	URL string `json:"url"`
	// SHA256 is the archive's hash, 64 lower-case hex digits.
	SHA256 string `json:"sha256"`
	// Size is the archive's size in bytes.
	Size int64 `json:"size"`
}

// Platform is this machine's archive key.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// ArchiveFor is the entry's archive for platform: the platform's own, else
// the one for Any.
func (e Entry) ArchiveFor(platform string) (Archive, bool) {
	if a, ok := e.Archives[platform]; ok {
		return a, true
	}
	a, ok := e.Archives[Any]
	return a, ok
}

// Find returns the entry with this id.
func (ix *Index) Find(id string) (Entry, bool) {
	i := slices.IndexFunc(ix.Extensions, func(e Entry) bool { return e.ID == id })
	if i < 0 {
		return Entry{}, false
	}
	return ix.Extensions[i], true
}

var (
	versionPattern = regexp.MustCompile(`^\d{1,6}\.\d{1,6}\.\d{1,6}([-+][0-9A-Za-z.-]{1,40})?$`)
	toolPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Parse decodes and checks an index. Call it only on bytes whose signature
// verified (Verify): nothing here makes an unsigned index trustworthy.
func Parse(data []byte) (*Index, error) {
	if len(data) > MaxIndexBytes {
		return nil, fmt.Errorf("the extension index is larger than %d MiB", MaxIndexBytes>>20)
	}
	var head struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("the extension index is not valid JSON: %w", err)
	}
	if head.Format != Format {
		return nil, fmt.Errorf("the extension index has format %d; this Flint reads format %d. Update Flint to use it", head.Format, Format)
	}
	var ix Index
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&ix); err != nil {
		return nil, fmt.Errorf("the extension index is not valid: %w", err)
	}
	if err := ix.check(); err != nil {
		return nil, err
	}
	return &ix, nil
}

func (ix *Index) check() error {
	if len(ix.Extensions) > MaxEntries {
		return fmt.Errorf("the extension index lists more than %d extensions", MaxEntries)
	}
	seen := map[string]bool{}
	for i := range ix.Extensions {
		e := &ix.Extensions[i]
		if !manifest.ValidID(e.ID) {
			return fmt.Errorf("extension index: entry %d has the invalid id %q", i+1, clip(e.ID))
		}
		if seen[e.ID] {
			return fmt.Errorf("extension index: %s is listed twice", e.ID)
		}
		seen[e.ID] = true
		if err := e.check(); err != nil {
			return fmt.Errorf("extension index: %s: %w", e.ID, err)
		}
	}
	return nil
}

func (e *Entry) check() error {
	for _, f := range []struct {
		name, value string
		max         int
		required    bool
	}{
		{"name", e.Name, 60, true},
		{"publisher", e.Publisher, 60, false},
		{"description", e.Description, 500, false},
	} {
		if err := checkText(f.name, f.value, f.max, f.required); err != nil {
			return err
		}
	}
	if !versionPattern.MatchString(e.Version) {
		return fmt.Errorf("version %q: use major.minor.patch", clip(e.Version))
	}
	if e.Contract < 1 {
		return errors.New(`"contract" is missing`)
	}
	if !slices.Contains([]string{KindProgram, KindThemes, KindLanguages, KindData}, e.Kind) {
		return fmt.Errorf("unknown kind %q", clip(e.Kind))
	}
	if len(e.Permissions) > 50 {
		return errors.New("too many permissions")
	}
	for _, p := range e.Permissions {
		if err := checkText("permission", p, 300, true); err != nil {
			return err
		}
	}
	if len(e.AgentTools) > 20 {
		return errors.New("too many agent tools")
	}
	for _, t := range e.AgentTools {
		if !toolPattern.MatchString(t.Name) {
			return fmt.Errorf("agent tool name %q is not valid", clip(t.Name))
		}
	}
	if len(e.Archives) == 0 {
		return errors.New("no archives")
	}
	for platform, a := range e.Archives {
		if platform != Any && !slices.Contains(Platforms, platform) {
			return fmt.Errorf("unknown platform %q", clip(platform))
		}
		if (platform == Any) != (e.Kind != KindProgram) {
			if platform == Any {
				return fmt.Errorf("a program needs an archive per platform, not %q", Any)
			}
			return fmt.Errorf("an extension without a program has one archive, %q, not %q", Any, platform)
		}
		if err := a.check(); err != nil {
			return fmt.Errorf("archive %s: %w", platform, err)
		}
	}
	if e.Permissions == nil {
		e.Permissions = []string{}
	}
	if e.AgentTools == nil {
		e.AgentTools = []Tool{}
	}
	return nil
}

func (a Archive) check() error {
	if err := CheckURL(a.URL); err != nil {
		return err
	}
	if !shaPattern.MatchString(a.SHA256) {
		return errors.New("sha256 must be 64 lower-case hex digits")
	}
	if a.Size <= 0 || a.Size > MaxArchiveBytes {
		return fmt.Errorf("size must be between 1 byte and %d MiB", MaxArchiveBytes>>20)
	}
	return nil
}

// hash is the SHA-256 as bytes.
func (a Archive) hash() []byte {
	b, _ := hex.DecodeString(a.SHA256)
	return b
}

// CheckURL accepts what Flint is willing to request for an index or an
// archive: an absolute https URL without credentials. Plain http is accepted
// for a loopback host only (tests, a local mirror on the same machine).
func CheckURL(raw string) error {
	if len(raw) > maxURLLen {
		return errors.New("the address is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("%q is not an address Flint can download from: use https://…", clip(raw))
	}
	return checkParsedURL(u)
}

func checkParsedURL(u *url.URL) error {
	if u.User != nil {
		return errors.New("the address must not contain a user name or password")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%s is not https: Flint downloads extensions over https only (http works for localhost)", u.Redacted())
	}
	return fmt.Errorf("%q is not an address Flint can download from: use https://…", clip(u.Redacted()))
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// EntryOf summarizes a manifest as an index entry (without archives).
func EntryOf(m *manifest.Manifest) Entry {
	e := Entry{
		ID: m.ID, Name: m.Name, Description: m.Description, Publisher: m.Publisher, Version: m.Version,
		Contract: m.Contract, Kind: KindOf(m), Permissions: slices.Clone(m.Permissions), AgentTools: []Tool{},
		Archives: map[string]Archive{},
	}
	if e.Permissions == nil {
		e.Permissions = []string{}
	}
	for _, t := range m.Contributes.AgentTools {
		e.AgentTools = append(e.AgentTools, Tool{Name: t.Name, Acts: t.Acts})
	}
	return e
}

// KindOf is the manifest's kind.
func KindOf(m *manifest.Manifest) string {
	c := m.Contributes
	langs := len(c.Languages) + len(c.Debuggers) + len(c.TestRunners)
	switch {
	case m.HasProgram():
		return KindProgram
	case len(c.Themes) > 0 && langs == 0:
		return KindThemes
	case langs > 0 && len(c.Themes) == 0:
		return KindLanguages
	}
	return KindData
}

func checkText(name, value string, limit int, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%q is missing", name)
		}
		return nil
	}
	if len([]rune(value)) > limit {
		return fmt.Errorf("%q is longer than %d characters", name, limit)
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%q contains control characters", name)
	}
	return nil
}

// clip keeps untrusted text in an error message short and printable.
func clip(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return s
}
