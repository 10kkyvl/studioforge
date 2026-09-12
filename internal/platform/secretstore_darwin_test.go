//go:build darwin

package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMacKeychainStoreRoundTrip(t *testing.T) {
	if os.Getenv("STUDIOFORGE_KEYCHAIN_INTEGRATION") != "1" {
		t.Skip("set STUDIOFORGE_KEYCHAIN_INTEGRATION=1 to use a real macOS Keychain")
	}
	store, err := OpenSystemSecretStore("StudioForge-Test")
	if err != nil {
		t.Skipf("macOS Keychain unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := "roundtrip-throwaway"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = store.Delete(cleanupCtx, key)
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

func TestKeychainValueEncodingPreservesBinaryData(t *testing.T) {
	want := []byte{'\x00', '\n', '\r', 0xff, 'x'}
	got, ok := decodeKeychainValue(encodeKeychainValue(want))
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("decoded value=%#v ok=%v, want %#v", got, ok, want)
	}
}

func TestKeychainSetCommandDoesNotExposeSecretInArgv(t *testing.T) {
	secret := "key-without-argv-secret"
	encoded := encodeKeychainValue([]byte(secret))
	cmd, err := newKeychainSetCommand(context.Background(), "StudioForge", "openrouter", encoded)
	if err != nil {
		t.Fatalf("construct security command: %v", err)
	}
	args := strings.Join(cmd.Args, "\x00")
	if strings.Contains(args, secret) || strings.Contains(args, encoded) {
		t.Fatalf("secret leaked into security argv: %q", cmd.Args)
	}
	stdin, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatalf("read command stdin: %v", err)
	}
	if !strings.Contains(string(stdin), encoded) {
		t.Fatalf("encoded value missing from security stdin: %q", stdin)
	}
}

func TestKeychainSetRejectsInteractiveMetadataInjection(t *testing.T) {
	if _, err := newKeychainSetCommand(context.Background(), "StudioForge\nadd-generic-password", "account", "encoded"); err == nil {
		t.Fatal("expected newline in service name to be rejected")
	}
}

func TestKeychainSetRejectsOversizedInteractiveInput(t *testing.T) {
	service := strings.Repeat("s", securityMaxInputLine)
	cmd, err := newKeychainSetCommand(context.Background(), service, "account", "encoded")
	if err == nil {
		t.Fatal("expected oversized security interactive input to be rejected before execution")
	}
	if cmd != nil {
		t.Fatal("oversized input must not produce an executable command")
	}
}
