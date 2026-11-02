package host

import (
	"errors"
	"io/fs"

	"github.com/zalando/go-keyring"
)

// Keyring is Secrets in the OS credential store (macOS Keychain, the Secret
// Service on Linux).
type Keyring struct{}

func (Keyring) Get(service, account string) (string, error) {
	v, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fs.ErrNotExist
	}
	return v, err
}

func (Keyring) Set(service, account, value string) error {
	return keyring.Set(service, account, value)
}

func (Keyring) Delete(service, account string) error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return fs.ErrNotExist
	}
	return err
}
