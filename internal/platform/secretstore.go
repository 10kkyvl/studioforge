package platform

import (
	"context"
	"errors"
	"sync"
)

var ErrSecretStoreUnavailable = errors.New("operating-system secret store is unavailable")

// ErrSecretNotFound is returned by a SecretStore.Get when no value is stored
// for the given key.
var ErrSecretNotFound = errors.New("secret not found")

type SecretStore interface {
	Set(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

// OpenSystemSecretStore returns a SecretStore backed by the OS credential
// store (Windows Credential Manager, macOS Keychain). It returns
// ErrSecretStoreUnavailable on platforms without a supported backend, or when
// the backend cannot be reached. service scopes the stored entries (e.g. an
// application name).
func OpenSystemSecretStore(service string) (SecretStore, error) {
	return openSystemSecretStore(service)
}

type MemorySecretStore struct {
	mu     sync.RWMutex
	values map[string][]byte
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{values: map[string][]byte{}}
}
func (m *MemorySecretStore) Set(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = append([]byte(nil), value...)
	return nil
}
func (m *MemorySecretStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.values[key]
	if !ok {
		return nil, ErrSecretNotFound
	}
	return append([]byte(nil), v...), nil
}
func (m *MemorySecretStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}
