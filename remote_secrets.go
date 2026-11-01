//go:build !server

package main

import "flint/internal/remote"

// remoteSecretStore keeps paired devices' token hashes in the Keychain.
func remoteSecretStore() remote.Secrets { return remoteKeychain{} }
