package remote

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// memSecrets stands in for the Keychain (tests never touch the real one).
type memSecrets struct {
	mu         sync.Mutex
	v          map[string]string
	reads      int
	fail       error
	failDelete error
	block      chan struct{} // Get waits for it when set
}

func newMemSecrets() *memSecrets { return &memSecrets{v: map[string]string{}} }

func (m *memSecrets) Get(account string) (string, error) {
	m.mu.Lock()
	block := m.block
	m.mu.Unlock()
	if block != nil {
		<-block
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	if m.fail != nil {
		return "", m.fail
	}
	return m.v[account], nil
}

func (m *memSecrets) Set(account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.v[account] = value
	return nil
}

func (m *memSecrets) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDelete != nil {
		return m.failDelete
	}
	delete(m.v, account)
	return nil
}

func (m *memSecrets) values() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for _, v := range m.v {
		out = append(out, v)
	}
	return out
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type savedDevices struct {
	mu   sync.Mutex
	list []Device
	n    int
	fail error
}

func (s *savedDevices) save(list []Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.list = append([]Device{}, list...)
	s.n++
	return nil
}

func newTestDevices(t *testing.T) (*Devices, *memSecrets, *clock, *savedDevices) {
	t.Helper()
	sec := newMemSecrets()
	clk := &clock{t: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	saved := &savedDevices{}
	d := NewDevices(nil, sec, saved.save)
	d.now = clk.now
	return d, sec, clk, saved
}

func TestPairingIssuesATokenAndStoresOnlyItsHash(t *testing.T) {
	d, sec, _, saved := newTestDevices(t)
	p, err := d.BeginPairing()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Secret) != pairingSecretLen {
		t.Fatalf("secret %q", p.Secret)
	}
	dev, token, err := d.Pair(strings.ToLower(groupSecret(p.Secret)), "Ola's iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Name != "Ola's iPhone" || len(dev.Scopes) != 1 || dev.Scopes[0] != ScopeRead || dev.Created == 0 {
		t.Fatalf("device %+v", dev)
	}
	if !strings.HasPrefix(token, dev.ID+".") || len(token) < 40 {
		t.Fatalf("token %q", token)
	}
	for _, v := range sec.values() {
		if strings.Contains(token, v) || strings.Contains(v, strings.SplitN(token, ".", 2)[1]) {
			t.Fatal("the Keychain holds the token, not its hash")
		}
	}
	if saved.n == 0 || len(saved.list) != 1 || saved.list[0].ID != dev.ID {
		t.Fatalf("saved %+v", saved.list)
	}
	got, ok := d.Authenticate(token)
	if !ok || got.ID != dev.ID {
		t.Fatalf("authenticate = %+v, %v", got, ok)
	}
	for _, bad := range []string{"", "x", dev.ID + ".", dev.ID + ".wrong", "nope." + strings.SplitN(token, ".", 2)[1], token + "x"} {
		if _, ok := d.Authenticate(bad); ok {
			t.Errorf("token %q accepted", bad)
		}
	}
}

func TestPairingSecretIsSingleUse(t *testing.T) {
	d, _, _, _ := newTestDevices(t)
	p, _ := d.BeginPairing()
	if _, _, err := d.Pair(p.Secret, "phone"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Pair(p.Secret, "second phone"); !errors.Is(err, ErrPairing) {
		t.Fatalf("second use: %v", err)
	}
}

func TestPairingSecretExpires(t *testing.T) {
	d, _, clk, _ := newTestDevices(t)
	p, _ := d.BeginPairing()
	if !p.Expires.Equal(clk.now().Add(PairingTTL)) {
		t.Fatalf("expires %v", p.Expires)
	}
	clk.add(PairingTTL + time.Second)
	if _, _, err := d.Pair(p.Secret, "phone"); !errors.Is(err, ErrPairing) {
		t.Fatalf("expired secret: %v", err)
	}
}

func TestWrongSecretsCancelThePairing(t *testing.T) {
	d, _, _, _ := newTestDevices(t)
	p, _ := d.BeginPairing()
	for range maxPairingAttempts {
		if _, _, err := d.Pair("AAAAAAAAAAAAAAAA", "phone"); !errors.Is(err, ErrPairing) {
			t.Fatalf("wrong secret: %v", err)
		}
	}
	if _, _, err := d.Pair(p.Secret, "phone"); !errors.Is(err, ErrPairing) {
		t.Fatalf("the right secret after %d wrong ones: %v", maxPairingAttempts, err)
	}
	if d.Pairing() {
		t.Fatal("pairing still open")
	}
}

func TestNewPairingReplacesTheOldSecret(t *testing.T) {
	d, _, _, _ := newTestDevices(t)
	old, _ := d.BeginPairing()
	if _, err := d.BeginPairing(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Pair(old.Secret, "phone"); !errors.Is(err, ErrPairing) {
		t.Fatalf("old secret: %v", err)
	}
	d.CancelPairing()
	if d.Pairing() {
		t.Fatal("cancelled pairing still open")
	}
}

func TestDeviceNameIsValidated(t *testing.T) {
	d, _, _, _ := newTestDevices(t)
	for _, name := range []string{"", "   ", "a\nb", strings.Repeat("x", maxDeviceName+1)} {
		p, _ := d.BeginPairing()
		if _, _, err := d.Pair(p.Secret, name); err == nil || errors.Is(err, ErrPairing) {
			t.Errorf("name %q: %v", name, err)
		}
	}
}

func TestRevokeAndRevokeAll(t *testing.T) {
	d, sec, _, saved := newTestDevices(t)
	pair := func(name string) (Device, string) {
		p, _ := d.BeginPairing()
		dev, tok, err := d.Pair(p.Secret, name)
		if err != nil {
			t.Fatal(err)
		}
		return dev, tok
	}
	a, ta := pair("a")
	b, tb := pair("b")
	if err := d.Revoke(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Authenticate(ta); ok {
		t.Fatal("revoked token still works")
	}
	if _, ok := d.Authenticate(tb); !ok {
		t.Fatal("other device lost access")
	}
	if len(d.List()) != 1 || len(saved.list) != 1 {
		t.Fatalf("list %+v saved %+v", d.List(), saved.list)
	}
	if err := d.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Authenticate(tb); ok || len(d.List()) != 0 || len(sec.values()) != 0 {
		t.Fatal("revoke all left access")
	}
	_ = b
}

func TestHashesAreReadFromTheKeychainOnceAfterRestart(t *testing.T) {
	d, sec, _, saved := newTestDevices(t)
	p, _ := d.BeginPairing()
	_, token, _ := d.Pair(p.Secret, "phone")
	// A new process: metadata from state.json, hash from the Keychain.
	again := NewDevices(saved.list, sec, saved.save)
	before := sec.reads
	for range 3 {
		if _, ok := again.Authenticate(token); !ok {
			t.Fatal("token not accepted after restart")
		}
	}
	if sec.reads-before != 1 {
		t.Fatalf("Keychain read %d times", sec.reads-before)
	}
	// A Keychain that fails denies access instead of letting anyone in.
	sec2 := newMemSecrets()
	sec2.fail = errors.New("locked")
	if _, ok := NewDevices(saved.list, sec2, saved.save).Authenticate(token); ok {
		t.Fatal("a failing Keychain let the token in")
	}
}

func TestLastSeenIsSavedAtMostOncePerInterval(t *testing.T) {
	d, _, clk, saved := newTestDevices(t)
	p, _ := d.BeginPairing()
	_, token, _ := d.Pair(p.Secret, "phone")
	n := saved.n
	d.Authenticate(token)
	d.Authenticate(token)
	if saved.n != n {
		t.Fatalf("saved %d times within the interval", saved.n-n)
	}
	clk.add(lastSeenSaveInterval + time.Second)
	d.Authenticate(token)
	if saved.n != n+1 || saved.list[0].LastSeen != clk.now().UnixMilli() {
		t.Fatalf("last seen not saved: %+v", saved.list)
	}
}

func TestDeviceLimit(t *testing.T) {
	d, _, _, _ := newTestDevices(t)
	for i := range maxDevices {
		p, _ := d.BeginPairing()
		if _, _, err := d.Pair(p.Secret, "d"+string(rune('a'+i%26))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.BeginPairing(); err == nil {
		t.Fatal("pairing past the device limit")
	}
}

func TestRevocationSurvivesKeychainAndDiskFailures(t *testing.T) {
	d, sec, _, saved := newTestDevices(t)
	p, _ := d.BeginPairing()
	dev, token, err := d.Pair(p.Secret, "phone")
	if err != nil {
		t.Fatal(err)
	}
	sec.failDelete = errors.New("keychain locked")
	saved.mu.Lock()
	saved.fail = errors.New("disk full")
	saved.mu.Unlock()
	if err := d.Revoke(dev.ID); err == nil {
		t.Fatal("a revocation that wasn't saved anywhere reported success")
	}
	if _, ok := d.Authenticate(token); ok || d.Has(dev.ID) {
		t.Fatal("revoked device still accepted")
	}
	if d.PendingRevocations() != 1 {
		t.Fatalf("pending = %d", d.PendingRevocations())
	}
	// Still failing: the revocation stays pending.
	if err := d.RetryRevocations(); err == nil || d.PendingRevocations() != 1 {
		t.Fatalf("retry while failing: %v, pending %d", err, d.PendingRevocations())
	}
	// The Keychain works again: done (without the hash the device can't come back).
	sec.mu.Lock()
	sec.failDelete = nil
	sec.mu.Unlock()
	if err := d.RetryRevocations(); err != nil || d.PendingRevocations() != 0 || len(sec.values()) != 0 {
		t.Fatalf("retry: %v, pending %d, keychain %v", err, d.PendingRevocations(), sec.values())
	}
}

func TestRevocationIsDoneWhenOneStepWorks(t *testing.T) {
	d, sec, _, _ := newTestDevices(t)
	p, _ := d.BeginPairing()
	dev, _, _ := d.Pair(p.Secret, "phone")
	sec.failDelete = errors.New("keychain locked")
	if err := d.Revoke(dev.ID); err != nil || d.PendingRevocations() != 0 {
		t.Fatalf("saved without the device: %v, pending %d", err, d.PendingRevocations())
	}
}

func TestKeychainIsReadOutsideTheLockAndRetriedSlowly(t *testing.T) {
	d, sec, clk, saved := newTestDevices(t)
	p, _ := d.BeginPairing()
	_, token, _ := d.Pair(p.Secret, "phone")
	again := NewDevices(saved.list, sec, saved.save)
	again.now = clk.now
	sec.mu.Lock()
	sec.block = make(chan struct{})
	sec.mu.Unlock()
	authDone := make(chan bool)
	go func() {
		_, ok := again.Authenticate(token)
		authDone <- ok
	}()
	// While the Keychain hangs, the device list still answers.
	listed := make(chan int)
	go func() { listed <- len(again.List()) }()
	select {
	case n := <-listed:
		if n != 1 {
			t.Fatalf("listed %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("List waited for the Keychain")
	}
	close(sec.block)
	if !<-authDone {
		t.Fatal("token refused after the Keychain answered")
	}
	sec.mu.Lock()
	sec.block = nil
	sec.mu.Unlock()

	// A device whose hash can't be read is retried at most every keychainRetry.
	broken := NewDevices(saved.list, sec, saved.save)
	broken.now = clk.now
	sec.mu.Lock()
	sec.fail = errors.New("locked")
	sec.mu.Unlock()
	before := sec.reads
	for range 5 {
		broken.Authenticate(token)
	}
	if sec.reads-before != 1 {
		t.Fatalf("Keychain read %d times for 5 requests", sec.reads-before)
	}
	clk.add(keychainRetry)
	sec.mu.Lock()
	sec.fail = nil
	sec.mu.Unlock()
	if _, ok := broken.Authenticate(token); !ok {
		t.Fatal("not retried after keychainRetry")
	}
}

func TestLoadHashesReadsEveryDeviceOnce(t *testing.T) {
	d, sec, _, saved := newTestDevices(t)
	var tokens []string
	for _, name := range []string{"a", "b", "c"} {
		p, _ := d.BeginPairing()
		_, tok, _ := d.Pair(p.Secret, name)
		tokens = append(tokens, tok)
	}
	again := NewDevices(saved.list, sec, saved.save)
	before := sec.reads
	again.LoadHashes()
	for _, tok := range tokens {
		if _, ok := again.Authenticate(tok); !ok {
			t.Fatal("token refused")
		}
	}
	if sec.reads-before != 3 {
		t.Fatalf("Keychain read %d times", sec.reads-before)
	}
}

func TestDeviceNamesRefuseFormatCharacters(t *testing.T) {
	for _, name := range []string{"phone\u202Etxt.exe", "ph\u200Bone", "\uFEFFphone", "a\u2066b"} {
		if CheckDeviceName(name) == nil {
			t.Errorf("%q accepted", name)
		}
	}
	if err := CheckDeviceName("Ola's iPhone 15 Pro – work 📱"); err != nil {
		t.Fatal(err)
	}
}
