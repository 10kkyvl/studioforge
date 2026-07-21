//go:build darwin

package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type macKeychainStore struct {
	service string
}

func openSystemSecretStore(service string) (SecretStore, error) {
	if _, err := exec.LookPath("security"); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	return &macKeychainStore{service: service}, nil
}

func (k *macKeychainStore) Set(ctx context.Context, key string, value []byte) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "add-generic-password", "-U", "-s", k.service, "-a", key, "-w", string(value))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("security add-generic-password: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (k *macKeychainStore) Get(ctx context.Context, key string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", k.service, "-a", key, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return nil, ErrSecretNotFound
		}
		return nil, errors.New("security find-generic-password failed")
	}
	value := strings.TrimSuffix(stdout.String(), "\n")
	value = strings.TrimSuffix(value, "\r")
	return []byte(value), nil
}

func (k *macKeychainStore) Delete(ctx context.Context, key string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-s", k.service, "-a", key)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return nil
		}
		return fmt.Errorf("security delete-generic-password: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
