package accounts

import (
	"context"
	"fmt"
	"sync"
)

// Provider is a service people sign in to. Each one registers itself (see
// accounts_registry.go); the UI lists what is registered.
type Provider struct {
	// ID names the provider in keys, state and bindings ("github").
	ID string
	// Name is what the UI calls it ("GitHub").
	Name string
	// Host is the host currently configured for the provider (accounts are
	// per host: github.com and an Enterprise server are separate accounts).
	Host func() string
	// Flow returns the device flow for signing in on Host, or an error that
	// tells the user what is missing (no OAuth client configured…).
	Flow func() (DeviceFlow, error)
	// Identify reads the account a token belongs to on host. It is also the
	// check that a token works before it is stored.
	Identify func(ctx context.Context, host string, tok Token) (Account, error)
	// EnvToken (optional) names the environment variable a token is taken
	// from when no account is signed in on host ("" when none is set).
	EnvToken func(host string) string
}

// Registry holds the providers, in registration order.
type Registry struct {
	mu   sync.Mutex
	list []Provider
}

// Register adds a provider; ids are unique.
func (r *Registry) Register(p Provider) error {
	if !idRE.MatchString(p.ID) || p.Name == "" || p.Host == nil || p.Flow == nil || p.Identify == nil {
		return fmt.Errorf("invalid account provider %q", p.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.list {
		if q.ID == p.ID {
			return fmt.Errorf("account provider %q registered twice", p.ID)
		}
	}
	r.list = append(r.list, p)
	return nil
}

// Get returns the provider with id.
func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.list {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// List returns the providers in registration order.
func (r *Registry) List() []Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Provider{}, r.list...)
}
