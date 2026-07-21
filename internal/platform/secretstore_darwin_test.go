//go:build darwin

package platform

import (
	"context"
	"errors"
	"testing"
)

func TestMacKeychainStoreRoundTrip(t *testing.T) {
	store, err := OpenSystemSecretStore("StudioForge-Test")
	if err != nil {
		t.Skipf("macOS Keychain unavailable: %v", err)
	}
	ctx := context.Background()
	key := "roundtrip-throwaway"
	t.Cleanup(func() {
		_ = store.Delete(ctx, key)
	})

	if err := store.Set(ctx, key, []byte("s3cr3t-value")); err != nil {
		t.Skipf("Keychain Set unavailable in this environment: %v", err)
	}
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "s3cr3t-value" {
		t.Fatalf("Get returned %q", got)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete error=%v want ErrSecretNotFound", err)
	}
}
