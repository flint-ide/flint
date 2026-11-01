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
	hash      []byte // sha256 of the token's secret part; nil until read
	savedSeen int64  // LastSeen as last written
}

// Devices are the paired devices and the open pairing, if any. Safe for
// concurrent use.
type Devices struct {
	secrets Secrets
	save    func([]Device) error
	now     func() time.Time

	mu       sync.Mutex
	devices  map[string]*device
	pairHash []byte // sha256 of the open pairing secret; nil = none
	pairEnds time.Time
	attempts int
}

// NewDevices loads the remembered devices; their token hashes are read from
// secrets when first needed. save is called with the full list after every
// change.
func NewDevices(list []Device, secrets Secrets, save func([]Device) error) *Devices {
	d := &Devices{secrets: secrets, save: save, now: time.Now, devices: map[string]*device{}}
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

// CheckDeviceName reports why name can't name a device.
func CheckDeviceName(name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return errors.New("device name is empty")
	case utf8.RuneCountInString(name) > maxDeviceName || !utf8.ValidString(name):
		return fmt.Errorf("device name longer than %d characters", maxDeviceName)
	case strings.ContainsFunc(name, unicode.IsControl):
		return errors.New("device name contains control characters")
	}
	return nil
}

// Pair exchanges the open pairing's secret for a device token. The secret
// works once; wrong secrets count, and too many cancel the pairing.
func (d *Devices) Pair(secret, name string) (Device, string, error) {
	sum := sha256.Sum256([]byte(normalizeSecret(secret)))
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pairHash == nil || !d.now().Before(d.pairEnds) {
		d.pairHash = nil
		return Device{}, "", ErrPairing
	}
	if subtle.ConstantTimeCompare(sum[:], d.pairHash) != 1 {
		d.attempts++
		if d.attempts >= maxPairingAttempts {
			d.pairHash = nil
		}
		return Device{}, "", ErrPairing
	}
	if err := CheckDeviceName(name); err != nil {
		return Device{}, "", err // the secret stays valid for a corrected request
	}
	d.pairHash = nil
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
	d.devices[id] = dev
	if err := d.saveLocked(); err != nil {
		delete(d.devices, id)
		_ = d.secrets.Delete(id)
		return Device{}, "", err
	}
	return dev.copy(), id + "." + secretPart, nil
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

// Authenticate returns the device a token belongs to. The token's hash is
// compared in constant time; the device's last-seen time is updated.
func (d *Devices) Authenticate(token string) (Device, bool) {
	id, secret, ok := strings.Cut(token, ".")
	if !ok || len(id) != deviceIDLen || !isHex(id) || secret == "" || len(secret) > 128 {
		return Device{}, false
	}
	sum := sha256.Sum256([]byte(secret))
	d.mu.Lock()
	defer d.mu.Unlock()
	dev := d.devices[id]
	if dev == nil {
		return Device{}, false
	}
	if dev.hash == nil {
		stored, err := d.secrets.Get(id)
		raw, decErr := hex.DecodeString(stored)
		if err != nil || decErr != nil || len(raw) != sha256.Size {
			return Device{}, false
		}
		dev.hash = raw
	}
	if subtle.ConstantTimeCompare(sum[:], dev.hash) != 1 {
		return Device{}, false
	}
	dev.LastSeen = d.now().UnixMilli()
	if time.Duration(dev.LastSeen-dev.savedSeen)*time.Millisecond >= lastSeenSaveInterval {
		dev.savedSeen = dev.LastSeen
		_ = d.saveLocked() // only the last-seen time is lost
	}
	return dev.copy(), true
}

// Revoke forgets a device and deletes its token hash.
func (d *Devices) Revoke(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.devices[id] == nil {
		return fmt.Errorf("no paired device %q", id)
	}
	delete(d.devices, id)
	err := d.secrets.Delete(id)
	return errors.Join(err, d.saveLocked())
}

// RevokeAll forgets every device.
func (d *Devices) RevokeAll() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var errs []error
	for id := range d.devices {
		errs = append(errs, d.secrets.Delete(id))
	}
	d.devices = map[string]*device{}
	d.pairHash = nil
	return errors.Join(append(errs, d.saveLocked())...)
}

// List returns the paired devices, oldest first (never nil).
func (d *Devices) List() []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.listLocked()
}

func (d *Devices) listLocked() []Device {
	out := make([]Device, 0, len(d.devices))
	for _, dev := range d.devices {
		out = append(out, dev.copy())
	}
	slices.SortFunc(out, func(a, b Device) int {
		return cmp.Or(cmp.Compare(a.Created, b.Created), strings.Compare(a.ID, b.ID))
	})
	return out
}

func (d *Devices) saveLocked() error {
	if d.save == nil {
		return nil
	}
	return d.save(d.listLocked())
}

func (dev *device) copy() Device {
	out := dev.Device
	out.Scopes = slices.Clone(dev.Scopes)
	return out
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
