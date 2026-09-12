//go:build darwin

package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type macKeychainStore struct {
	service string
}

const keychainValuePrefix = "studioforge:v1:"

// security's -i mode uses the custom split_line parser in security.c and the
// fixed-size readline buffer in readline.c. readline stops before consuming
// the newline when the buffer fills, so reject a complete command at the
// boundary instead of allowing the remainder to be parsed as another command.
// Sources: https://github.com/apple-oss-distributions/SecurityTool/blob/main/security.c
// and https://github.com/apple-oss-distributions/SecurityTool/blob/main/readline.c.
const securityMaxInputLine = 4096

func encodeKeychainValue(value []byte) string {
	return keychainValuePrefix + base64.RawStdEncoding.EncodeToString(value)
}

func decodeKeychainValue(value string) ([]byte, bool) {
	if !strings.HasPrefix(value, keychainValuePrefix) {
		return nil, false
	}
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, keychainValuePrefix))
	if err != nil {
		return nil, false
	}
	return decoded, true
}

func securityInteractiveQuote(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for _, ch := range value {
		if ch == '\\' || ch == '\'' {
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	b.WriteByte('\'')
	return b.String()
}

func newKeychainSetCommand(ctx context.Context, service, key, value string) (*exec.Cmd, error) {
	// security -i reads commands from stdin with its documented readline
	// parser. This keeps the value out of argv and avoids the -w prompt, which
	// is intended for a human terminal and cannot be driven reliably by a pipe.
	if strings.ContainsAny(service, "\x00\r\n") || strings.ContainsAny(key, "\x00\r\n") {
		return nil, errors.New("keychain service and account cannot contain NUL or newline")
	}
	line := "add-generic-password -U -s " + securityInteractiveQuote(service) + " -a " + securityInteractiveQuote(key) + " -w " + securityInteractiveQuote(value) + "\n"
	if len(line) >= securityMaxInputLine {
		return nil, errors.New("keychain command exceeds security interactive input limit")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	cmd.Stdin = bytes.NewReader([]byte(line))
	return cmd, nil
}

func openSystemSecretStore(service string) (SecretStore, error) {
	if _, err := exec.LookPath("security"); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	return &macKeychainStore{service: service}, nil
}

func (k *macKeychainStore) Set(ctx context.Context, key string, value []byte) error {
	// Keep the password out of argv, where it is visible through ps and other
	// process inspection tools. security -i reads the command from stdin, so it
	// also cannot switch to a human-only /dev/tty password prompt.
	cmd, err := newKeychainSetCommand(ctx, k.service, key, encodeKeychainValue(value))
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Do not return security's diagnostic verbatim: interactive-mode
		// diagnostics may include the command line, which contains the encoded
		// secret in stdin even though it is absent from argv.
		return fmt.Errorf("security add-generic-password: %w", err)
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
	if decoded, ok := decodeKeychainValue(value); ok {
		return decoded, nil
	}
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
