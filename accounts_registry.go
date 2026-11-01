package main

import (
	"log"

	"flint/internal/accounts"
)

// wireAccounts sets up the token store and registers the account providers.
// A new provider (Google…) is one more Register call with its device flow
// and its Identify (see githubProvider).
func (a *App) wireAccounts() {
	var secrets accounts.Secrets = keyringSecrets{service: accounts.SecretService}
	if dev, ok := devSecrets(); ok {
		secrets = dev
	}
	a.accounts = accounts.NewStore(secrets)
	a.github = newGitHub(a)
	a.registry = &accounts.Registry{}
	for _, p := range []accounts.Provider{githubProvider(a)} {
		if err := a.registry.Register(p); err != nil {
			log.Printf("flint: %v", err)
		}
	}
}
