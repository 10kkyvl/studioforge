package platform

import (
	"context"
	"errors"
	"sync"
)

var ErrSecretStoreUnavailable = errors.New("operating-system secret store is unavailable")

type SecretStore interface {
	Set(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

// WindowsCredentialManagerStore and MacOSKeychainStore are explicit adapter
// boundaries. StudioForge v1 uses Claude Code's own authentication and does not
// persist Anthropic credentials.
type WindowsCredentialManagerStore struct{}

func (WindowsCredentialManagerStore) Set(context.Context, string, []byte) error {
	return ErrSecretStoreUnavailable
}
func (WindowsCredentialManagerStore) Get(context.Context, string) ([]byte, error) {
	return nil, ErrSecretStoreUnavailable
}
func (WindowsCredentialManagerStore) Delete(context.Context, string) error {
	return ErrSecretStoreUnavailable
}

type MacOSKeychainStore struct{}

func (MacOSKeychainStore) Set(context.Context, string, []byte) error {
	return ErrSecretStoreUnavailable
}
func (MacOSKeychainStore) Get(context.Context, string) ([]byte, error) {
	return nil, ErrSecretStoreUnavailable
}
func (MacOSKeychainStore) Delete(context.Context, string) error { return ErrSecretStoreUnavailable }

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
		return nil, errors.New("secret not found")
	}
	return append([]byte(nil), v...), nil
}
func (m *MemorySecretStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}
