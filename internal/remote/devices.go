package remote

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Scopes a paired device can hold. Phase 1 grants and checks only read.
const (
	ScopeRead    = "read"    // list chats and tasks, follow the event stream
	ScopeRespond = "respond" // answer permissions, prompt, cancel (later)
	ScopeTasks   = "tasks"   // create tasks, PR, merge (later)
)

// Security limits: constants, not settings.
const (
	// PairingTTL is how long a pairing secret is valid.
	PairingTTL = 2 * time.Minute
	// maxPairingAttempts wrong secrets cancel the pairing.
	maxPairingAttempts = 5
	maxDevices         = 32
	maxDeviceName      = 64
	// pairingSecretLen is the length of a pairing secret: 80 random bits in
	// base32, short enough to type, and it lives 2 minutes for 5 tries.
	pairingSecretLen = 16
	deviceIDLen      = 16 // hex
	// lastSeenSaveInterval bounds how often a device's last-seen time is
	// written to state.json.
	lastSeenSaveInterval = time.Minute
)

// ErrPairing is returned for a wrong, used or expired pairing secret.
var ErrPairing = errors.New("pairing secret wrong, used or expired")

// Device is a paired phone as Flint remembers it (state.json). Its token is
// not here: only a hash of it, in the Keychain.
type Device struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	Created  int64    `json:"created"`  // Unix ms
	LastSeen int64    `json:"lastSeen"` // Unix ms, 0 = never
}

// Secrets keeps one value per account (the Keychain; a fake in tests).
type Secrets interface {
	Get(account string) (string, error)
	Set(account, value string) error
	Delete(account string) error
}

// Pairing is an open invitation to pair one device.
type Pairing struct {
	Secret  string    `json:"secret"`
	Expires time.Time `json:"expires"`
}

type device struct {
	Device
	hash      []byte    // sha256 of the token's secret part; nil until read
	loadAt    time.Time // last Keychain read attempt (failed reads are retried slowly)
	savedSeen int64     // LastSeen as last written
}

// Devices are the paired devices and the open pairing, if any. Safe for
// concurrent use. The Keychain and state.json are never touched with mu
// held: a slow Keychain must not stall every request.
type Devices struct {
	secrets Secrets
	save    func([]Device) error
	now     func() time.Time

	saveMu sync.Mutex // serializes saves; each writes the list as it is then

	mu       sync.Mutex
	devices  map[string]*device
	revoked  map[string]bool // revocations whose Keychain delete and save both failed: retried
	pairHash []byte          // sha256 of the open pairing secret; nil = none
	pairEnds time.Time
	attempts int
}

// keychainRetry spaces the Keychain reads of a device whose hash couldn't
// be read, so bad requests can't make Flint start /usr/bin/security for each.
const keychainRetry = 10 * time.Second

// NewDevices loads the remembered devices; their token hashes are read from
// secrets by LoadHashes, or when first needed. save is called with the full
// list after every change.
func NewDevices(list []Device, secrets Secrets, save func([]Device) error) *Devices {
	d := &Devices{secrets: secrets, save: save, now: time.Now, devices: map[string]*device{}, revoked: map[string]bool{}}
	for _, dev := range list {
		if len(dev.ID) == deviceIDLen && isHex(dev.ID) && len(d.devices) < maxDevices {
			dev.Scopes = slices.Clone(dev.Scopes)
			d.devices[dev.ID] = &device{Device: dev, savedSeen: dev.LastSeen}
		}
	}
	return d
}

// BeginPairing opens a pairing for one device, replacing any open one.
func (d *Devices) BeginPairing() (Pairing, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.devices) >= maxDevices {
		return Pairing{}, fmt.Errorf("at most %d paired devices: revoke one first", maxDevices)
	}
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Pairing{}, err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	sum := sha256.Sum256([]byte(secret))
	d.pairHash, d.pairEnds, d.attempts = sum[:], d.now().Add(PairingTTL), 0
	return Pairing{Secret: secret, Expires: d.pairEnds}, nil
}

// CancelPairing closes the open pairing, if any.
func (d *Devices) CancelPairing() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pairHash = nil
}

// Pairing reports whether a pairing is open.
func (d *Devices) Pairing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pairHash != nil && d.now().Before(d.pairEnds)
}

// groupSecret shows a pairing secret in groups of four (ABCD-EFGH-…).
func groupSecret(s string) string {
	var parts []string
	for len(s) > 4 {
		parts, s = append(parts, s[:4]), s[4:]
	}
	return strings.Join(append(parts, s), "-")
}

// normalizeSecret accepts a typed secret in any case, with dashes or spaces.
func normalizeSecret(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s))
}

// CheckDeviceName reports why name can't name a device. Format characters
// (bidi overrides, zero-width) are refused too: the name is shown in
// Settings and in the log.
func CheckDeviceName(name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return errors.New("device name is empty")
	case utf8.RuneCountInString(name) > maxDeviceName || !utf8.ValidString(name):
		return fmt.Errorf("device name longer than %d characters", maxDeviceName)
	case strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }):
		return errors.New("device name contains control or format characters")
	}
	return nil
}

// Pair exchanges the open pairing's secret for a device token. The secret
// works once; wrong secrets count, and too many cancel the pairing.
func (d *Devices) Pair(secret, name string) (Device, string, error) {
	sum := sha256.Sum256([]byte(normalizeSecret(secret)))
	if err := d.claimPairing(sum[:], name); err != nil {
		return Device{}, "", err
	}
	id, secretPart, err := newToken()
	if err != nil {
		return Device{}, "", err
	}
	tokenSum := sha256.Sum256([]byte(secretPart))
	if err := d.secrets.Set(id, hex.EncodeToString(tokenSum[:])); err != nil {
		return Device{}, "", fmt.Errorf("store the device token in the Keychain: %w", err)
	}
	now := d.now().UnixMilli()
	dev := &device{
		Device: Device{ID: id, Name: strings.TrimSpace(name), Scopes: []string{ScopeRead}, Created: now, LastSeen: now},
		hash:   tokenSum[:], savedSeen: now,
	}
	d.mu.Lock()
	d.devices[id] = dev
	d.mu.Unlock()
	if err := d.persist(); err != nil {
		d.mu.Lock()
		delete(d.devices, id)
		d.mu.Unlock()
		_ = d.secrets.Delete(id)
		return Device{}, "", err
	}
	return dev.copyLocked(&d.mu), id + "." + secretPart, nil
}

// claimPairing checks a pairing secret's hash and consumes the pairing.
func (d *Devices) claimPairing(sum []byte, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pairHash == nil || !d.now().Before(d.pairEnds) {
		d.pairHash = nil
		return ErrPairing
	}
	if subtle.ConstantTimeCompare(sum, d.pairHash) != 1 {
		d.attempts++
		if d.attempts >= maxPairingAttempts {
			d.pairHash = nil
		}
		return ErrPairing
	}
	if err := CheckDeviceName(name); err != nil {
		return err // the secret stays valid for a corrected request
	}
	if len(d.devices) >= maxDevices {
		return fmt.Errorf("at most %d paired devices", maxDevices)
	}
	d.pairHash = nil
	return nil
}

// newToken returns a device id and the token's secret part (256 bits).
func newToken() (id, secret string, err error) {
	var idb [deviceIDLen / 2]byte
	var sb [32]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(sb[:]); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(idb[:]), base64.RawURLEncoding.EncodeToString(sb[:]), nil
}

// TokenDevice returns the device id a token claims to belong to, if that
// device is paired (nothing is verified: Authenticate does that).
func (d *Devices) TokenDevice(token string) (string, bool) {
	id, _, ok := strings.Cut(token, ".")
	if !ok || len(id) != deviceIDLen || !isHex(id) {
		return "", false
	}
	return id, d.Has(id)
}

// Has reports whether a device is paired (not revoked).
func (d *Devices) Has(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.devices[id] != nil
}

// LoadHashes reads the token hashes of the devices that don't have one yet
// (when the server starts, so requests don't wait for the Keychain).
func (d *Devices) LoadHashes() {
	d.mu.Lock()
	var ids []string
	for id, dev := range d.devices {
		if dev.hash == nil {
			ids = append(ids, id)
			dev.loadAt = d.now()
		}
	}
	d.mu.Unlock()
	for _, id := range ids {
		d.storeHash(id, d.readHash(id))
	}
}

// readHash reads a device's token hash from the Keychain (nil on failure).
func (d *Devices) readHash(id string) []byte {
	stored, err := d.secrets.Get(id)
	raw, decErr := hex.DecodeString(stored)
	if err != nil || decErr != nil || len(raw) != sha256.Size {
		return nil
	}
	return raw
}

func (d *Devices) storeHash(id string, hash []byte) {
	if hash == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if dev := d.devices[id]; dev != nil {
		dev.hash = hash
	}
}

// Authenticate returns the device a token belongs to. The token's hash is
// compared in constant time; the device's last-seen time is updated.
func (d *Devices) Authenticate(token string) (Device, bool) {
	id, secret, ok := strings.Cut(token, ".")
	if !ok || len(id) != deviceIDLen || !isHex(id) || secret == "" || len(secret) > 128 {
		return Device{}, false
	}
	sum := sha256.Sum256([]byte(secret))
	d.mu.Lock()
	dev := d.devices[id]
	if dev == nil {
		d.mu.Unlock()
		return Device{}, false
	}
	hash, load := dev.hash, false
	if hash == nil && d.now().Sub(dev.loadAt) >= keychainRetry {
		dev.loadAt, load = d.now(), true
	}
	d.mu.Unlock()
	if hash == nil {
		if !load {
			return Device{}, false
		}
		if hash = d.readHash(id); hash == nil {
			return Device{}, false
		}
		d.storeHash(id, hash)
	}
	if subtle.ConstantTimeCompare(sum[:], hash) != 1 {
		return Device{}, false
	}
	d.mu.Lock()
	if d.devices[id] != dev { // revoked meanwhile
		d.mu.Unlock()
		return Device{}, false
	}
	dev.LastSeen = d.now().UnixMilli()
	save := time.Duration(dev.LastSeen-dev.savedSeen)*time.Millisecond >= lastSeenSaveInterval
	if save {
		dev.savedSeen = dev.LastSeen
	}
	out := dev.copy()
	d.mu.Unlock()
	if save {
		_ = d.persist() // only the last-seen time is lost
	}
	return out, true
}

// Revoke forgets a device at once and deletes its token hash. If both the
// Keychain delete and the save fail, the device could come back after a
// restart: the revocation is kept, retried (RetryRevocations), and the
// error returned.
func (d *Devices) Revoke(id string) error {
	d.mu.Lock()
	if d.devices[id] == nil {
		d.mu.Unlock()
		return fmt.Errorf("no paired device %q", id)
	}
	delete(d.devices, id)
	d.revoked[id] = true
	d.mu.Unlock()
	return d.RetryRevocations()
}

// RevokeAll forgets every device.
func (d *Devices) RevokeAll() error {
	d.mu.Lock()
	for id := range d.devices {
		d.revoked[id] = true
	}
	d.devices = map[string]*device{}
	d.pairHash = nil
	d.mu.Unlock()
	return d.RetryRevocations()
}

// RetryRevocations finishes revocations: it deletes their Keychain entries
// and saves the list. A revocation is done once either worked (without the
// hash, or without the state entry, the device can't come back).
func (d *Devices) RetryRevocations() error {
	d.mu.Lock()
	ids := slices.Collect(maps.Keys(d.revoked))
	d.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	deleted := map[string]bool{}
	var errs []error
	for _, id := range ids {
		if err := d.secrets.Delete(id); err != nil {
			errs = append(errs, fmt.Errorf("delete the device token from the Keychain: %w", err))
		} else {
			deleted[id] = true
		}
	}
	saveErr := d.persist()
	d.mu.Lock()
	for _, id := range ids {
		if deleted[id] || saveErr == nil {
			delete(d.revoked, id)
		}
	}
	pending := len(d.revoked)
	d.mu.Unlock()
	if pending == 0 {
		return nil // done, even if one of the two steps failed
	}
	return errors.Join(append(errs, saveErr)...)
}

// PendingRevocations counts revocations not yet saved anywhere.
func (d *Devices) PendingRevocations() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.revoked)
}

// List returns the paired devices, oldest first (never nil).
func (d *Devices) List() []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Device, 0, len(d.devices))
	for _, dev := range d.devices {
		out = append(out, dev.copy())
	}
	slices.SortFunc(out, func(a, b Device) int {
		return cmp.Or(cmp.Compare(a.Created, b.Created), strings.Compare(a.ID, b.ID))
	})
	return out
}

// persist saves the current list. Saves are serialized and each takes the
// list inside the serialization, so the last write is the latest state.
func (d *Devices) persist() error {
	if d.save == nil {
		return nil
	}
	d.saveMu.Lock()
	defer d.saveMu.Unlock()
	return d.save(d.List())
}

func (dev *device) copy() Device {
	out := dev.Device
	out.Scopes = slices.Clone(dev.Scopes)
	return out
}

// copyLocked copies dev under mu.
func (dev *device) copyLocked(mu *sync.Mutex) Device {
	mu.Lock()
	defer mu.Unlock()
	return dev.copy()
}

// has reports whether a device holds scope.
func (dev Device) has(scope string) bool { return slices.Contains(dev.Scopes, scope) }

func isHex(s string) bool {
	for _, r := range s {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}
