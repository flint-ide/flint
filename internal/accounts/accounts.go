// Package accounts keeps the accounts Flint is signed in to (GitHub now; other
// providers register the same way). Tokens live only in the OS keyring, behind
// Secrets; what the UI shows about an account (login, avatar, host, scopes) is
// plain metadata the app keeps in its state file. Signing in is the OAuth 2.0
// Device Authorization Grant (RFC 8628, device.go): no client secret, no local
// server, nothing for the web view to intercept.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// SecretService is the keyring service tokens are stored under; the account
// is Key(provider, host).
const SecretService = "flint-accounts"

// Bounds of what a provider's answers may put into Flint's state.
const (
	maxLoginLen  = 100
	maxNameLen   = 200
	maxURLLen    = 2048
	maxScopes    = 50
	maxScopeLen  = 100
	maxTokenLen  = 8192
	maxAccounts  = 32
	maxSourceLen = 32
)

// Sources of a stored token.
const (
	SourceDevice = "device" // signed in with the device flow
	SourceCLI    = "cli"    // imported from the provider's CLI (gh auth token)
)

// Account is what Flint shows about a signed-in account. It holds no secret.
type Account struct {
	Provider  string   `json:"provider"`
	Host      string   `json:"host"`
	Login     string   `json:"login"`
	Name      string   `json:"name"`
	AvatarURL string   `json:"avatarUrl"`
	Scopes    []string `json:"scopes"`
	// Source tells how the token was obtained (SourceDevice, SourceCLI).
	Source     string `json:"source"`
	SignedInAt string `json:"signedInAt"`
}

// Token is an OAuth token as stored in the keyring.
type Token struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expiry  time.Time `json:"expiry,omitzero"`
	Scopes  []string  `json:"scopes,omitempty"`
}

// Valid reports whether the token can be sent: present and not expired.
func (t Token) Valid(now time.Time) bool {
	return t.Access != "" && (t.Expiry.IsZero() || now.Before(t.Expiry))
}

// Key is the keyring account of a provider's token for host.
func Key(provider, host string) string { return provider + ":" + host }

// Secrets is the OS keyring (a fake in tests). Set with "" deletes.
type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

// Store reads and writes tokens through Secrets, caching what it read (each
// keyring read can start a process and prompt).
type Store struct {
	secrets Secrets
	mu      sync.Mutex
	cache   map[string]Token // "" Access: known to be absent
}

// NewStore returns a store over secrets.
func NewStore(secrets Secrets) *Store {
	return &Store{secrets: secrets, cache: map[string]Token{}}
}

// Token returns the stored token of provider on host; ok is false when there is none.
func (s *Store) Token(provider, host string) (Token, bool, error) {
	key := Key(provider, host)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, cached := s.cache[key]; cached {
		return t, t.Access != "", nil
	}
	raw, err := s.secrets.Get(key)
	if err != nil {
		return Token{}, false, fmt.Errorf("read the %s token from the keyring: %w", provider, err)
	}
	var t Token
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &t); err != nil || t.Access == "" {
			return Token{}, false, fmt.Errorf("the %s token in the keyring is unreadable: sign in again", provider)
		}
	}
	s.cache[key] = t
	return t, t.Access != "", nil
}

// Save stores a token (replacing an older one).
func (s *Store) Save(provider, host string, t Token) error {
	if err := CheckToken(t.Access); err != nil {
		return err
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	key := Key(provider, host)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.secrets.Set(key, string(raw)); err != nil {
		return fmt.Errorf("store the %s token in the keyring: %w", provider, err)
	}
	s.cache[key] = t
	return nil
}

// Delete removes the stored token (none stored is fine).
func (s *Store) Delete(provider, host string) error {
	key := Key(provider, host)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.secrets.Set(key, ""); err != nil {
		return fmt.Errorf("delete the %s token from the keyring: %w", provider, err)
	}
	s.cache[key] = Token{}
	return nil
}

// CheckToken accepts a token that can go into an Authorization header: one
// printable ASCII word of bounded length.
func CheckToken(tok string) error {
	if tok == "" || len(tok) > maxTokenLen || strings.ContainsFunc(tok, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return errors.New("not a valid token")
	}
	return nil
}

var idRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Clean bounds what a provider said about an account before it is stored or
// shown: one-line strings, an https avatar URL, a bounded scope list.
func Clean(a Account) Account {
	out := Account{
		Provider:   a.Provider,
		Host:       a.Host,
		Login:      oneLine(a.Login, maxLoginLen),
		Name:       oneLine(a.Name, maxNameLen),
		Source:     oneLine(a.Source, maxSourceLen),
		SignedInAt: oneLine(a.SignedInAt, 64),
		Scopes:     []string{},
	}
	if u, err := url.Parse(a.AvatarURL); err == nil && u.Scheme == "https" && u.Host != "" && len(a.AvatarURL) <= maxURLLen {
		out.AvatarURL = a.AvatarURL
	}
	for _, s := range a.Scopes {
		s = strings.TrimSpace(s)
		if s != "" && len(s) <= maxScopeLen && len(out.Scopes) < maxScopes && !strings.ContainsFunc(s, unicode.IsSpace) && !slices.Contains(out.Scopes, s) {
			out.Scopes = append(out.Scopes, s)
		}
	}
	return out
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// Upsert returns list with a replacing the account of the same provider and host.
func Upsert(list []Account, a Account) []Account {
	out := Without(list, a.Provider, a.Host)
	if len(out) >= maxAccounts {
		out = out[1:]
	}
	return append(out, a)
}

// Without returns list without the account of provider on host.
func Without(list []Account, provider, host string) []Account {
	out := make([]Account, 0, len(list)+1)
	for _, a := range list {
		if a.Provider != provider || a.Host != host {
			out = append(out, a)
		}
	}
	return out
}

// Find returns the account of provider on host.
func Find(list []Account, provider, host string) (Account, bool) {
	for _, a := range list {
		if a.Provider == provider && a.Host == host {
			return a, true
		}
	}
	return Account{}, false
}
