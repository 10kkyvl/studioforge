//go:build !windows && !darwin

package platform

func openSystemSecretStore(service string) (SecretStore, error) {
	return nil, ErrSecretStoreUnavailable
}
