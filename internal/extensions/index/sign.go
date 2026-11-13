package index

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// The signature format is Flint's own and deliberately small (the minisign
// file format would need BLAKE2b, which is not in Go's standard library):
//
//   - a public key is the 32 bytes of an Ed25519 public key, in standard
//     base64 (44 characters);
//   - a secret key is the 64 bytes of an Ed25519 private key (seed, then
//     public key), in standard base64 (88 characters);
//   - a signature file ("index.json.sig") is one line: the 64-byte Ed25519
//     signature over the exact bytes of the index file, in standard base64.

// OfficialPublicKey verifies the index flint-ide/extensions publishes. It is
// empty until the maintainer generates the key pair (`flint-extindex
// keygen`), stores the secret key as that repository's signing secret and
// puts the public key here. While it is empty the official index is "not
// configured", and only an index with its own key in settings
// (extensions.indexPublicKey) can be used.
const OfficialPublicKey = ""

// ErrNoKey: there is no public key to verify an index with.
var ErrNoKey = errors.New("no public key is configured for the extension index")

// ErrBadSignature: the signature doesn't match the index and the key.
var ErrBadSignature = errors.New("the extension index's signature doesn't match: it was not signed with the configured key, or it was changed on the way")

// GenerateKey makes a key pair, both encoded as above.
func GenerateKey() (public, secret string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

// ParsePublicKey decodes a public key; "" is ErrNoKey.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrNoKey
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("the index public key must be an Ed25519 public key in base64 (44 characters), as `flint-extindex keygen` prints it")
	}
	return ed25519.PublicKey(b), nil
}

// ParseSecretKey decodes a secret key.
func ParseSecretKey(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("the signing key is empty")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PrivateKeySize {
		// The value is a secret: say nothing about it.
		return nil, errors.New("the signing key must be an Ed25519 secret key in base64 (88 characters), as `flint-extindex keygen` prints it")
	}
	key := ed25519.PrivateKey(b)
	// A private key carries its public half; a mismatch means it is damaged.
	if derived := ed25519.NewKeyFromSeed(key.Seed()); !derived.Equal(key) {
		return nil, errors.New("the signing key is damaged: its two halves don't belong together")
	}
	return key, nil
}

// PublicOf is the public key of a secret key, encoded.
func PublicOf(key ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
}

// Sign returns the signature file's content for data.
func Sign(key ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)) + "\n")
}

// Verify checks the signature file sig over the exact bytes data.
func Verify(key ed25519.PublicKey, data, sig []byte) error {
	if len(key) != ed25519.PublicKeySize {
		return ErrNoKey
	}
	if len(sig) > MaxSignatureBytes {
		return fmt.Errorf("%w (the signature file is too large)", ErrBadSignature)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("%w (the signature file is not a base64 Ed25519 signature)", ErrBadSignature)
	}
	if !ed25519.Verify(key, data, raw) {
		return ErrBadSignature
	}
	return nil
}
